# Claude and Codex Skills

The `.claude/` directory contains repo-local configuration for Claude-based development tools. `.claude/settings.json` enables the plugins expected for this repository, and `.claude/skills` exposes the same skills that Codex uses.

`skills` should be a symlink:

```text
.claude/skills -> ../.codex/skills
```

Keep `.codex/skills` as the canonical skills directory. The symlink lets Claude Desktop, Claude Code, and the Codex CLI discover the same local skill definitions, which keeps documentation and agent workflows consistent across tools.

Create or refresh the symlink from the repo root if it is missing or points somewhere else:

```bash
ln -sfn ../.codex/skills .claude/skills
```

Do not replace the symlink with a copied directory. CI and contributors should treat `.claude/skills` as a pointer to the canonical skills tree; update files under `.codex/skills` when changing shared skills.
