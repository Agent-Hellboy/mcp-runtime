# Entry template

Copy this shape when adding to `gotchas.md`, `cross-cutting.md`, or
`tracking.md`. Keep it tight; the goal is one screen, not a chapter.

```markdown
### <short, specific title — imperative or noun phrase>

<One paragraph, ≤ 8 lines. What is the surprise / invariant / item.
Why does it matter (the consequence of not knowing it). When does it
apply (the trigger). If there's a workaround or "do this instead,"
state it concretely.>

References:
- `path/to/file.go:LINE`
- `docs/something.md#anchor`
- upstream URL if external

Added: 2026-05-12
```

## Style notes

- **Lead with the rule or symptom**, not the backstory. Future readers
  want the answer first.
- **Name the trigger.** "When you do X" or "On Y change" — not "sometimes."
- **Cite real lines.** If you can't point at code or docs, the entry is
  probably about ephemeral state and doesn't belong here.
- **Don't restate `AGENTS.md`.** If it's already there, link to it; if
  it could go there, propose moving it.
- **Date every entry.** `Added: YYYY-MM-DD`. Update or delete entries
  whose date is older than the underlying behavior.
