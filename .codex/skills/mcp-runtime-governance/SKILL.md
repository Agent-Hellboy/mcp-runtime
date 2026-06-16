---
name: mcp-runtime-governance
description: Apply and debug MCP Runtime access grants, agent sessions, gateway policy, and MCP JSON-RPC traffic with governance headers. Use when working on MCPAccessGrant, MCPAgentSession, adapter proxy/stdio, access CLI, platform API grant/session endpoints, or allow/deny tool calls.
---

# MCP Runtime — governance and MCP traffic

## Flows

| Path | Notes |
|------|--------|
| **UI** | Create/apply grants and sessions; toggle enable/revoke |
| **CLI (default)** | `mcp-runtime auth login --api-url <url>` → `access grant init` / `access grant apply --file …` |
| **Adapter (recommended for agents)** | `adapter stdio\|proxy --server <name> --agent <id> [--auto-refresh]` → `POST /api/v1/runtime/adapter/sessions` |
| **Admin kube fallback** | `kubectl apply -f` or `access … --use-kube` (bypasses platform auth) |

Session apply via platform API is **admin-only**. Adapters usually skip manual session apply.

## Rules (short)

- One namespace per team; `MCPServer.spec.teamID` and `SubjectRef.teamID` must match gateway identity fields exactly when set.
- Platform API rejects cross-namespace `serverRef` and shared-catalog writes for non-admins.
- Each `MCPServer.spec.tools[]` needs `sideEffect: read|write|destructive`; grants need explicit `allowedSideEffects` (empty = deny all classes).
- `server policy inspect` shows rendered policy; gateway reloads on a short poll — wait before assuming `session_not_found`.

## Example manifests

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: workspace-assistant-grant
  namespace: mcp-servers
spec:
  subject: {humanID: user-123, agentID: ops-agent}
  serverRef: {name: workspace-assistant-mcp, namespace: mcp-servers}
  maxTrust: high
  allowedSideEffects: [read]
  toolRules:
    - {name: add, decision: allow, requiredTrust: low}
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: sess-ops-agent
  namespace: mcp-servers
spec:
  subject: {humanID: user-123, agentID: ops-agent}
  serverRef: {name: workspace-assistant-mcp, namespace: mcp-servers}
  consentedTrust: high
  policyVersion: v1
```

## HTTP API (admin `x-api-key`)

- `POST /api/v1/runtime/grants`, `POST /api/v1/runtime/sessions`
- `POST /api/v1/runtime/grants/{ns}/{name}/enable|disable`
- `POST /api/v1/runtime/sessions/{ns}/{name}/revoke|unrevoke`

## MCP JSON-RPC (local Kind, port-forward 18080)

```bash
PROTO=2025-06-18
BASE=http://localhost:18080/workspace-assistant-mcp/mcp
curl -sS -H "content-type: application/json" \
  -H "accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: $PROTO" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' -D - -o /dev/null "$BASE"
# Capture Mcp-Session-Id from response headers, then notifications/initialized and tools/call with -H "Mcp-Session-Id: <session>"
```

Kind e2e applies generated access YAML and exercises allow/deny over real MCP traffic — see `test/e2e/kind.sh` and `test/e2e/select_pr_scenarios.sh` (`governance`, `trust`, `adapter-proxy`).

## Code map

- CRDs: `api/v1alpha1/`, `config/crd/bases/`
- Shared policy: `pkg/access/`, `pkg/policy/`
- Adapters: `internal/cli/adapter/`, `internal/agentadapter/`, `services/runtime-control/internal/runtimeapi/adapter.go`
