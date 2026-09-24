<!-- cockpit:🔍 34 searches already logged on turn 1. For c -->
## Component discovery: use graphify query first

When exploring component structure, imports, hook usage, or file relationships, query the knowledge graph first: `graphify query "<question>"`. This is faster than grep/find and reduces redundant searches. Examples:
- `graphify query "ServersWorkspace component usage"`
- `graphify query "hook dependencies in App.tsx"`
- `graphify query "components that import API client"`

If the graph lacks a node for a symbol that clearly exists, run `graphify update .` to refresh, then retry. Only fall back to grep after confirming the graph misses it.
