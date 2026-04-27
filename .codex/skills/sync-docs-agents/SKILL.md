---
name: sync-docs-agents
description: Keep repository documentation and AGENTS.md aligned with code, design, and product changes. Use when Codex changes or reviews behavior that affects user-facing docs, developer runbooks, architecture notes, CLI/API references, setup instructions, design-system guidance, operational commands, or agent instructions; when a user asks to update docs after implementation; or when stale docs/AGENTS.md could mislead future contributors.
---

# Sync Docs and AGENTS

## Overview

Use this skill to decide what documentation must change after a code, design, or product update, then make those edits in the right files with verifiable links back to source behavior.

## Workflow

1. Identify the change surface.
   - Inspect the diff, touched packages, new commands, config flags, UI behavior, API shapes, CRDs, generated schemas, deployment manifests, and tests.
   - Search for existing docs before adding new files: `rg -n "<command|field|feature|concept>" README.md AGENTS.md docs website config api internal services`.
   - Prefer updating the nearest existing guide, reference, or runbook over creating a new document.

2. Classify required doc updates.
   - User-facing product change: update README, docs guides, CLI examples, API/reference pages, screenshots or UI copy references if present.
   - Developer workflow change: update AGENTS.md, build/test instructions, local setup notes, failure modes, environment variables, or repo map entries.
   - Design change: update design-system rules, component guidance, UX behavior notes, or docs that describe screens/workflows.
   - Operational change: update install, deployment, TLS, registry, Kubernetes, observability, and rollback/debug instructions.
   - Schema/contract change: update CRD/API docs, examples, generated docs if the repo owns them, and any golden snapshots tied to CLI help.

3. Verify source truth before writing.
   - Treat code, tests, CRD types, OpenAPI specs, CLI definitions, workflow files, and manifests as source of truth.
   - For commands and examples, confirm exact flags, defaults, resource names, paths, and environment variables from implementation.
   - Avoid inventing roadmap promises or behavior not present in code unless the user explicitly asks for planned-product wording.

4. Edit with the right scope.
   - Keep docs concise and task-oriented; remove stale claims instead of layering caveats.
   - Preserve the repo's voice, terminology, command style, and existing document structure.
   - Update AGENTS.md only for durable contributor or agent guidance, not one-off implementation details.
   - Keep examples copy-pasteable: include required tags, namespaces, paths, auth headers, and prerequisite environment variables.
   - Update links and table-of-contents entries when adding or renaming pages.

5. Validate.
   - Run the narrowest relevant checks: markdown/docs build if available, golden tests for CLI help changes, generated-doc drift checks when docs are generated, and targeted code tests if examples exercise behavior.
   - At minimum, run searches for old field names, commands, or stale wording that the change replaces.
   - If validation cannot be run, state the blocker and what was manually checked.

## AGENTS.md Rules

Update AGENTS.md when a change affects how future agents or contributors should work in the repo. Good reasons include new required checks, changed setup commands, new failure modes, changed repository layout, new service ownership, changed CLI workflows, or new operational safety rules.

Do not put product marketing, exhaustive API reference, release notes, or transient PR context in AGENTS.md. Link or summarize stable docs instead.

## Output Standard

When finished, summarize:
- docs and AGENTS.md files changed
- source behavior each update reflects
- validation run, including searches for stale terms
- known doc gaps left intentionally open
