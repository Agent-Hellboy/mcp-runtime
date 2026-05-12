# `ai-assist/` — durable agent-facing learnings

This directory is the **team-shared, in-repo memory** for AI coding agents
(Claude Code, Codex CLI, Cursor, Copilot, …) and the humans who work with
them. It is checked into git on purpose: every contributor and every agent
sees the same context.

## Charter

Keep entries that **a future contributor or agent would benefit from the
first time they touch this repo**. Three buckets:

| File | What lives here |
|---|---|
| `gotchas.md` | Non-obvious behavior that has already cost a session — silent reloads, polling intervals, symlinked files, distroless containers, etc. |
| `cross-cutting.md` | "When you touch X, also check Y" — invariants that span multiple files or components and are easy to miss. |
| `tracking.md` | External things to watch — upstream specs, SEPs, dependency releases, deprecated APIs in flight. One line per item, dated, with a link. |

Use `TEMPLATE.md` for new entries.

## What NOT to put here

These belong somewhere else, not in `ai-assist/`:

- **Ephemeral session state** (current TODO list, "what I'm working on") → tasks,
  the user's local plan.md, or memory.
- **Code patterns / architecture / file paths** → derivable by reading the
  repo; that's what `AGENTS.md` and the `docs/` tree are for.
- **Build/test/lint commands** → already in `AGENTS.md`.
- **Git history, commit-by-commit recaps** → `git log` is authoritative.
- **One-off debugging transcripts** → useful in a PR description, not here.
  Only the *generalized learning* extracted from a debugging session belongs
  here.
- **Anything that would rot in a week** → if it depends on a specific PR or
  feature flag that's about to land, don't write it; if you already did,
  remove it once the change lands.

A good rule of thumb: would this entry still be useful in 3 months, or
would a contributor have to re-derive it? If "still useful," it belongs;
if "would re-derive," skip.

## How to add an entry

1. Open the file matching the bucket above (`gotchas.md`, `cross-cutting.md`,
   or `tracking.md`).
2. Append a new entry following the shape in `TEMPLATE.md` — short title,
   one-paragraph body, file or doc references where applicable, an
   `Added:` date in `YYYY-MM-DD`.
3. Keep entries **short** (≤ 8 lines of body). If you need a long
   explanation, the right home is probably `docs/`, not here.
4. Have the user **review the diff manually before commit**. This file is
   meant to capture human-validated learnings, not stream-of-consciousness
   notes from an agent.

## Maintenance

- Update or remove entries that become wrong, stale, or were superseded
  by code/docs changes. Stale entries are worse than no entries.
- If an entry has been promoted to `AGENTS.md` or the `docs/` tree,
  remove it here and leave a one-line pointer if the new location is
  non-obvious.
- Treat this directory like any other reviewed code: changes go through
  PR, get a sanity check, and land with a `doc:` commit prefix per
  `AGENTS.md` commit conventions.

See `AGENTS.md` → **AI session hygiene** for when agents are expected to
propose updates here.
