# Upstream tracking

External things the runtime depends on or is converging toward. One line
per item, with a link and a date. New entries follow `TEMPLATE.md`. Trim
or remove items once they are no longer load-bearing.

## How to add

```markdown
### <upstream item>

<One sentence on what it is and why we care. Stance: tracking / planned /
implemented / not applicable.>

References:
- <upstream URL>
- <runtime file or doc that mirrors it, if any>

Added: 2026-05-12
```

## Why this is here, not in the `mcp-spec-compliance` skill output

The skill **queries upstream live** at audit time (open SEP PRs, draft
changelog, schema). This file is the lower-frequency, human-curated
view of "what we're actively positioned on" — the things we want a
contributor to know about even when they didn't run the skill. Keep
the two consistent: when the skill repeatedly surfaces a SEP, record
the stance here so the next session does not re-derive it.
