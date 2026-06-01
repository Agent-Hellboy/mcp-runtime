# Next Product Picks Implementation Plan

This plan tracks product ideas beyond the minimal tool-catalog MVP. Issue 273
implements only the lightweight catalog, copy-config, and informational risk
badge slice.

## Current Baseline

- Server inventory is declared on `MCPServer.spec.tools`, `prompts`,
  `mcpResources`, and `tasks`.
- Live inventory probing already feeds the server catalog through the Runtime
  API.
- Grants and sessions already model tool-level access and session consent.
- The UI already has server cards, inventory drift badges, and connect config
  rendering.
- OIDC login exists, but team membership is manual for now.

## Product Picks

1. Tool-level search/catalog across all MCP servers.
2. Risk badges and policy modes.
3. Human approval workflow for destructive tools.
4. Copy-config onboarding for Claude, Cursor, VS Code, and raw JSON.
5. Virtual governed endpoints composed from selected tools.
6. IdP/OIDC group mapping to grants.

## Recommended Sequence

### 1. Catalog And Copy Config

Build a scoped `/api/runtime/tools` endpoint from existing server inventory and
live-inventory cache. Add a dense UI table, `mcp-runtime catalog tools`, and a
visible server `Copy config` action.

### 2. Informational Risk

Add optional `tools[].riskLevel` and compute defaults from trust and side
effect. Keep this informational only: no policy enum changes and no new
enforcement decisions.

### 3. Identity Mapping

If manual team membership becomes painful, parse verified OIDC group claims and
map groups to platform teams in the platform store.

### 4. Approvals

For destructive tools, add approval request storage, UI queue, CLI approval
commands, and gateway retry flow. Do not store raw tool arguments by default.

### 5. Virtual Endpoints

Add a virtual endpoint model that composes selected tools from multiple servers,
requires explicit aliases for collisions, and preserves source server/tool in
audit events.

## Validation

Each shipped slice should include API scoping tests, CLI help/golden coverage
where command output changes, UI static/render tests, and gateway/policy tests
when audit or request-path behavior changes.
