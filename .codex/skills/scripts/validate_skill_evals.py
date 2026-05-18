#!/usr/bin/env python3
"""Validate repo-local Agent Skill eval manifests."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def require(condition: bool, errors: list[str], message: str) -> None:
    if not condition:
        errors.append(message)


def load_json(path: Path, errors: list[str]) -> dict[str, Any] | None:
    try:
        data = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        errors.append(f"{path}: failed to load or parse JSON: {exc}")
        return None
    if not isinstance(data, dict):
        errors.append(f"{path}: top-level value must be an object")
        return None
    return data


def validate_output_evals(skill: str, path: Path, errors: list[str]) -> None:
    data = load_json(path, errors)
    if data is None:
        return

    require(data.get("skill_name") == skill, errors, f"{path}: skill_name must be {skill!r}")
    evals = data.get("evals")
    require(isinstance(evals, list), errors, f"{path}: evals must be a list")
    if not isinstance(evals, list):
        return
    require(len(evals) >= 2, errors, f"{path}: expected at least 2 evals")

    seen_ids: set[str] = set()
    for index, item in enumerate(evals):
        prefix = f"{path}: eval {index}"
        if not isinstance(item, dict):
            errors.append(f"{prefix}: must be an object")
            continue
        eval_id = item.get("id")
        require(isinstance(eval_id, str) and bool(eval_id.strip()), errors, f"{prefix}: id must be a non-empty string")
        if isinstance(eval_id, str):
            require(eval_id not in seen_ids, errors, f"{prefix}: duplicate id {eval_id!r}")
            seen_ids.add(eval_id)
        for field in ("prompt", "expected_output"):
            require(
                isinstance(item.get(field), str) and bool(item[field].strip()),
                errors,
                f"{prefix}: {field} must be a non-empty string",
            )
        assertions = item.get("assertions")
        require(isinstance(assertions, list) and bool(assertions), errors, f"{prefix}: assertions must be a non-empty list")
        if isinstance(assertions, list):
            for assertion_index, assertion in enumerate(assertions):
                require(
                    isinstance(assertion, str) and bool(assertion.strip()),
                    errors,
                    f"{prefix}: assertion {assertion_index} must be a non-empty string",
                )


def validate_trigger_queries(skill: str, path: Path, errors: list[str]) -> None:
    data = load_json(path, errors)
    if data is None:
        return

    require(data.get("skill_name") == skill, errors, f"{path}: skill_name must be {skill!r}")
    queries = data.get("queries")
    require(isinstance(queries, list), errors, f"{path}: queries must be a list")
    if not isinstance(queries, list):
        return
    require(len(queries) >= 3, errors, f"{path}: expected at least 3 trigger queries")

    positives = 0
    negatives = 0
    for index, item in enumerate(queries):
        prefix = f"{path}: query {index}"
        if not isinstance(item, dict):
            errors.append(f"{prefix}: must be an object")
            continue
        require(
            isinstance(item.get("query"), str) and bool(item["query"].strip()),
            errors,
            f"{prefix}: query must be a non-empty string",
        )
        should_trigger = item.get("should_trigger")
        require(isinstance(should_trigger, bool), errors, f"{prefix}: should_trigger must be boolean")
        if should_trigger is True:
            positives += 1
        elif should_trigger is False:
            negatives += 1

    require(positives >= 2, errors, f"{path}: expected at least 2 should_trigger=true queries")
    require(negatives >= 1, errors, f"{path}: expected at least 1 should_trigger=false query")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "skills_root",
        nargs="?",
        type=Path,
        default=Path(__file__).resolve().parents[1],
        help="Path to the .codex/skills directory",
    )
    args = parser.parse_args()

    skills_root = args.skills_root.resolve()
    if not skills_root.is_dir():
        print(f"{skills_root}: not a directory")
        return 1

    errors: list[str] = []
    try:
        skill_dirs = sorted(path for path in skills_root.iterdir() if (path / "SKILL.md").is_file())
    except OSError as exc:
        print(f"{skills_root}: failed to list skill directories: {exc}")
        return 1
    require(bool(skill_dirs), errors, f"{skills_root}: no skill directories found")

    for skill_dir in skill_dirs:
        skill = skill_dir.name
        evals_dir = skill_dir / "evals"
        output_evals = evals_dir / "evals.json"
        trigger_queries = evals_dir / "trigger_queries.json"
        require(output_evals.is_file(), errors, f"{output_evals}: missing")
        require(trigger_queries.is_file(), errors, f"{trigger_queries}: missing")
        if output_evals.is_file():
            validate_output_evals(skill, output_evals, errors)
        if trigger_queries.is_file():
            validate_trigger_queries(skill, trigger_queries, errors)

    if errors:
        for error in errors:
            print(error)
        return 1

    print(f"validated {len(skill_dirs)} skill eval suites under {skills_root}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
