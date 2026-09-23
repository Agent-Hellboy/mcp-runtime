# Graphify upstream maintenance

Graphify is a third-party CLI and skill. Keep the MCP Runtime integration
aligned with upstream while preserving the repo's own safety rules and
workflow-specific guidance.

## Last reviewed

- Review date: 2026-09-23
- Upstream release and PyPI `graphifyy` version: `0.9.66`
- Local CLI version observed during review: `0.8.35`
- Upstream source: [Graphify-Labs/graphify](https://github.com/Graphify-Labs/graphify)
- Upstream Codex entrypoint: [skill-codex.md](https://github.com/Graphify-Labs/graphify/blob/v8/graphify/skill-codex.md)
- Upstream release list: [GitHub releases](https://github.com/Graphify-Labs/graphify/releases)
- Package release history: [PyPI graphifyy](https://pypi.org/project/graphifyy/)

Recheck these values whenever the local integration is reviewed; this record
is a timestamped snapshot, not a promise that the versions remain current.

## Review procedure

1. Check the installed CLI with `graphify --version` and, when installed by uv,
   `uv tool list`.
2. Check the latest published release using the upstream GitHub releases page
   or `gh release list --repo Graphify-Labs/graphify --limit 1`. Check the
   package's current version at
   `https://pypi.org/pypi/graphifyy/json` (the distribution is `graphifyy`,
   while the executable is named `graphify`).
3. Review the release notes, versioned Codex skill entrypoint, and any
   referenced upstream docs that overlap with local references. Compare
   behavioral changes such as commands, flags, interpreter requirements,
   generated artifacts, integrations, and update semantics.
4. Update only the affected local references. Keep repo-specific rules intact:
   don't install or upgrade the package without an explicit request, preserve
   the interpreter/safety guard, and avoid copying generic content that
   upstream already maintains well.
5. Update trigger/eval cases if the skill's scope or routing changes. Check
   local markdown links and the skill eval manifest validator, then refresh
   this review record with the inspected upstream release, local CLI version,
   and date.

## CLI upgrades

Do not upgrade the user's installed CLI as part of checking upstream or
updating these docs. If an upgrade is explicitly requested, use the installed
package manager (for a uv tool install, `uv tool upgrade graphifyy`), then
confirm with `graphify --version`. Review any changed CLI behavior against
the local skill before relying on it.
