#!/usr/bin/env python3
"""Generate a pkg.go.dev-style internals package reference from `go doc`."""

from __future__ import annotations

import pathlib
import re
import subprocess


ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "docs" / "internals" / "go-package-reference.md"

PACKAGES = [
    ("API types", ["go", "doc", "-all", "./api/v1alpha1"]),
    ("Metadata helpers", ["go", "doc", "-all", "./pkg/metadata"]),
    ("Agent adapters", ["go", "doc", "-all", "./internal/agentadapter"]),
    ("Operator internals", ["go", "doc", "-all", "./internal/operator"]),
    ("CLI command routing", ["go", "doc", "-all", "./internal/cli/root"]),
    ("CLI core", ["go", "doc", "-all", "./internal/cli/core"]),
    ("CLI Kubernetes helpers", ["go", "doc", "-all", "./internal/cli/kube"]),
    ("CLI Kubernetes errors", ["go", "doc", "-all", "./internal/cli/kubeerr"]),
    ("CLI cluster", ["go", "doc", "-all", "./internal/cli/cluster"]),
    ("CLI cert-manager", ["go", "doc", "-all", "./internal/cli/certmanager"]),
    ("CLI platform API", ["go", "doc", "-all", "./internal/cli/platformapi"]),
    ("CLI platform status", ["go", "doc", "-all", "./internal/cli/platformstatus"]),
    ("CLI registry", ["go", "doc", "-all", "./internal/cli/registry"]),
    ("CLI registry config", ["go", "doc", "-all", "./internal/cli/registry/config"]),
    ("CLI registry references", ["go", "doc", "-all", "./internal/cli/registry/ref"]),
    ("CLI registry resolution", ["go", "doc", "-all", "./internal/cli/registry/resolve"]),
    ("CLI server", ["go", "doc", "-all", "./internal/cli/server"]),
    ("CLI setup asset paths", ["go", "doc", "-all", "./internal/cli/setup/assetpath"]),
    ("CLI setup ingress manifests", ["go", "doc", "-all", "./internal/cli/setup/ingressmanifest"]),
    ("CLI setup plan", ["go", "doc", "-all", "./internal/cli/setup/plan"]),
    ("CLI binary", ["go", "doc", "-cmd", "./cmd/mcp-runtime"]),
    ("Operator binary", ["go", "doc", "-cmd", "./cmd/operator"]),
]


HEADER_RE = re.compile(r"^[A-Z][A-Z0-9 ]+$")
PACKAGE_RE = re.compile(r'^package\s+(\S+)(?:\s+// import "([^"]+)")?')


class PackageDoc:
    def __init__(self, title: str, command: list[str], output: str) -> None:
        self.title = title
        self.command = command
        self.output = output.rstrip()
        self.package_name = ""
        self.import_path = ""
        self.overview: list[str] = []
        self.sections: dict[str, list[str]] = {}
        self._parse()

    def _parse(self) -> None:
        lines = self.output.splitlines()
        if not lines:
            return

        match = PACKAGE_RE.match(lines[0])
        if match:
            self.package_name = match.group(1)
            self.import_path = match.group(2) or ""

        current = "OVERVIEW"
        buckets: dict[str, list[str]] = {current: []}
        for line in lines[1:]:
            stripped = line.strip()
            if stripped and HEADER_RE.match(stripped):
                current = stripped.title()
                buckets.setdefault(current, [])
                continue
            buckets.setdefault(current, []).append(line)

        self.overview = trim_blank_edges(buckets.pop("OVERVIEW", []))
        self.sections = {name: trim_blank_edges(body) for name, body in buckets.items() if trim_blank_edges(body)}

    def index_entries(self) -> list[tuple[str, str]]:
        entries: list[tuple[str, str]] = []
        for section, lines in self.sections.items():
            if section in {"Constants", "Variables"}:
                entries.append((section, package_anchor(self.title, section)))
                continue
            for line in lines:
                stripped = line.strip()
                if stripped.startswith(("func ", "type ")):
                    entries.append((declaration_label(stripped), package_anchor(self.title, stripped)))
        return entries


def run(command: list[str]) -> str:
    return subprocess.check_output(command, cwd=ROOT, text=True)


def trim_blank_edges(lines: list[str]) -> list[str]:
    start = 0
    end = len(lines)
    while start < end and lines[start].strip() == "":
        start += 1
    while end > start and lines[end - 1].strip() == "":
        end -= 1
    return lines[start:end]


def slug(value: str) -> str:
    value = value.lower()
    value = re.sub(r"`([^`]+)`", r"\1", value)
    value = re.sub(r"[^a-z0-9]+", "-", value)
    return value.strip("-")


def package_anchor(title: str, name: str) -> str:
    return f"{slug(title)}-{slug(name)}"


def declaration_label(line: str) -> str:
    if line.startswith("func "):
        return line.split("{", 1)[0].strip()
    if line.startswith("type "):
        return line.split("{", 1)[0].strip()
    return line


def declaration_anchor_line(title: str, section: str, line: str) -> str | None:
    stripped = line.strip()
    if section not in {"Functions", "Types"}:
        return None
    if not stripped.startswith(("func ", "type ")):
        return None
    return f'<a id="{package_anchor(title, stripped)}"></a>'


def fenced_block(lines: list[str], language: str = "text") -> list[str]:
    return [f"```{language}", *lines, "```"]


def render_package(pkg: PackageDoc) -> list[str]:
    out: list[str] = [f'<a id="{slug(pkg.title)}"></a>', f"## {pkg.title}", ""]

    meta: list[str] = []
    if pkg.package_name:
        meta.append(f"Package: `{pkg.package_name}`")
    if pkg.import_path:
        meta.append(f"Import path: `{pkg.import_path}`")
    if meta:
        out.extend(meta)
        out.append("")

    out.extend(
        [
            "Source command:",
            "",
            "```bash",
            " ".join(pkg.command),
            "```",
            "",
        ]
    )

    out.extend([f'<a id="{package_anchor(pkg.title, "Overview")}"></a>', "### Overview", ""])
    if pkg.overview:
        out.extend(pkg.overview)
    else:
        out.append("_No package overview is documented._")
    out.append("")

    jump_targets = ["Overview", "Index", *pkg.sections.keys()]
    out.extend(["### Jump To", ""])
    for target in jump_targets:
        out.append(f"- [{target}](#{package_anchor(pkg.title, target)})")
    out.append("")

    out.extend([f'<a id="{package_anchor(pkg.title, "Index")}"></a>', "### Index", ""])
    entries = pkg.index_entries()
    if entries:
        for label, anchor in entries:
            out.append(f"- [`{label}`](#{anchor})")
    else:
        out.append("_No exported declarations._")
    out.append("")

    for section, lines in pkg.sections.items():
        out.extend([f'<a id="{package_anchor(pkg.title, section)}"></a>', f"### {section}", ""])
        block: list[str] = []
        for line in lines:
            anchor = declaration_anchor_line(pkg.title, section, line)
            if anchor:
                if block:
                    out.extend(fenced_block(block))
                    out.append("")
                    block = []
                out.append(anchor)
            block.append(line)
        if block:
            out.extend(fenced_block(block))
            out.append("")

    return out


def main() -> None:
    packages = [PackageDoc(title, command, run(command)) for title, command in PACKAGES]
    chunks = [
        "# Go Package Reference",
        "",
        "<!-- Generated by docs/scripts/generate_go_package_reference.py; do not edit by hand. -->",
        "",
        "This page renders `go doc` output for the main contributor-facing Go packages in a pkg.go.dev-style shape.",
        "Regenerate it from the repository root with:",
        "",
        "```bash",
        "python3 docs/scripts/generate_go_package_reference.py",
        "```",
        "",
        "## Packages",
        "",
    ]

    for pkg in packages:
        label = pkg.import_path or pkg.package_name or pkg.title
        chunks.append(f"- [{pkg.title}](#{slug(pkg.title)}) `{label}`")
    chunks.append("")

    for pkg in packages:
        chunks.extend(render_package(pkg))

    OUT.write_text("\n".join(chunks), encoding="utf-8")


if __name__ == "__main__":
    main()
