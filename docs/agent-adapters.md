# Agent Adapters

MCP Runtime includes two agent-side adapters that attach governed identity to
MCP traffic without requiring the agent framework to know anything about
grants, sessions, or policy:

- `mcp-runtime adapter proxy` exposes a local Streamable HTTP MCP endpoint and
  forwards requests to an MCP Runtime route.
- `mcp-runtime adapter stdio` exposes a stdio MCP server process and forwards
  each JSON-RPC message to the same MCP Runtime HTTP route.

Both adapters only **present** issued identity values. They do not create
grants, create sessions, evaluate policy, or bypass the gateway. Platform
admins still author `MCPAccessGrant` resources; the platform API and the
adapter session endpoint together resolve which `MCPAgentSession` a caller
runs under, and the gateway is the enforcement point.

The adapter surface is intentionally limited to stdio and Streamable HTTP, the
two standard MCP transports. There is no separate legacy HTTP+SSE adapter.

## How the adapter gets its identity

There are three supported ways to give an adapter its `humanID`, `agentID`,
`teamID`, and `sessionID`:

1. **Platform-issued session (recommended).** The adapter calls
   `POST /api/runtime/adapter/sessions`. The platform derives the principal
   from your `mcp-runtime auth login` token, picks a matching enabled
   `MCPAccessGrant`, writes (or reuses) an `MCPAgentSession`, and returns the
   identity values. Optional `--auto-refresh` renews the session before
   expiry without restarting the adapter.
2. **Explicit flags / environment.** `--human-id`, `--agent-id`,
   `--session-id`, `--team-id` (or the matching `MCP_RUNTIME_*` env vars).
   Useful for testing and for inheriting an externally-managed session.
3. **Anonymous mode** (stdio only): `--anonymous` skips identity entirely so
   the adapter can target public/read-only runtime routes. Only the methods
   listed in `--anonymous-methods` are forwarded.

Mixed configurations are supported: identity flags always override values
returned by the platform-issued session, so a caller can pin a specific field
(e.g. a long-lived `--session-id` for a test) while letting the platform fill
in the rest. The override survives every auto-refresh tick.

## Platform-issued sessions — quickstart

```bash
mcp-runtime auth login --api-url https://platform.example.com

mcp-runtime adapter stdio \
  --runtime-url https://mcp.example.com/workspace-assistant-mcp/mcp \
  --server workspace-assistant-mcp \
  --agent ticket-triage-agent \
  --auto-refresh
```

What this does on each invocation:

1. The CLI calls `POST /api/runtime/adapter/sessions` with `{serverName,
   namespace?, agentID}`. `namespace` defaults to the principal's primary
   namespace.
2. The platform derives `humanID` from `Principal.Subject` (fallback to
   `Email`) and `teamID` from the principal's membership in the namespace's
   team.
3. The platform lists enabled `MCPAccessGrant` resources in that namespace,
   filters those whose `serverRef.name` matches and whose subject equals the
   caller or is empty (wildcard), and picks the grant with the highest
   `MaxTrust`. Ties are broken by oldest `creationTimestamp`.
4. The platform looks up an existing `MCPAgentSession` with the deterministic
   name `adapter-<sha256-prefix(humanID,agentID,teamID,serverName)>` in the
   namespace. If one exists and is not revoked, has more than 30 s until
   expiry, and its `policyVersion` matches the selected grant's, it is
   reused. Otherwise a fresh `MCPAgentSession` is applied with a 1 h TTL
   (capped at 24 h).
5. The response carries `name`, `humanID`, `agentID`, `teamID`,
   `consentedTrust`, `policyVersion`, and absolute `expiresAt`. The adapter
   uses `name` as `X-MCP-Agent-Session` on every outbound request.
6. With `--auto-refresh`, a background goroutine renews the session ~5 min
   before `expiresAt` and atomically rotates the identity. In-flight requests
   continue with the previous identity; subsequent requests pick up the new
   one without a restart. Transient platform errors are logged to stderr; the
   previous identity stays in place until a refresh succeeds.

### Required grant

A grant must exist before the platform will issue a session. Example:

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: triage-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: workspace-assistant-mcp
  subject:
    # Any of these may be empty to act as a wildcard for that field.
    humanID: support-lead
    agentID: ticket-triage-agent
    teamID: team-acme
  maxTrust: high
  allowedSideEffects:
    - read
  policyVersion: v1
  toolRules:
    - name: add
      decision: allow
      requiredTrust: low
    - name: upper
      decision: allow
      requiredTrust: low
```

If the principal does not match any enabled grant for the server, the
adapter-session endpoint returns 403 and the adapter refuses to start.

## Explicit-identity mode

When you already have an `MCPAgentSession` and don't want the platform to pick
the grant for you (for example in a fixed CI environment), set everything
explicitly:

```bash
export MCP_RUNTIME_URL=http://localhost:18080/workspace-assistant-mcp/mcp
export MCP_RUNTIME_HUMAN_ID=support-lead
export MCP_RUNTIME_AGENT_ID=ticket-triage-agent
export MCP_RUNTIME_SESSION_ID=sess-ticket-triage-agent

mcp-runtime adapter proxy
```

| Environment variable | Required | Purpose |
|---|---:|---|
| `MCP_RUNTIME_URL` | yes | Absolute Streamable HTTP MCP route. |
| `MCP_RUNTIME_HUMAN_ID` | yes¹ | Human identity (`X-MCP-Human-ID`). |
| `MCP_RUNTIME_AGENT_ID` | yes¹ | Agent identity (`X-MCP-Agent-ID`). |
| `MCP_RUNTIME_TEAM_ID` | no | Team identity (`X-MCP-Team-ID`) for team-scoped grants. |
| `MCP_RUNTIME_SESSION_ID` | yes¹ | `MCPAgentSession` name (`X-MCP-Agent-Session`). |
| `MCP_RUNTIME_HOST_HEADER` | no | Override the `Host` header for host-based ingress. |
| `MCP_RUNTIME_LISTEN_ADDR` | proxy | Local listener; defaults to `127.0.0.1:8099`. |
| `MCP_RUNTIME_PROTOCOL_VERSION` | no | MCP protocol header. Defaults to `2025-06-18`; the negotiated `result.protocolVersion` from the runtime's `initialize` response overrides it for the rest of the process. |
| `MCP_RUNTIME_SET_XFF` | proxy | `false`/`0`/`no`/`off` suppresses `X-Forwarded-*` headers. Defaults to enabled. |
| `MCP_RUNTIME_REQUEST_TIMEOUT` | no | Go duration for adapter→runtime calls. Defaults to unbounded. |
| `MCP_RUNTIME_MAX_INBOUND_BYTES` | proxy | Caps inbound JSON-RPC bodies; over-cap responds 413. Defaults to 16 MiB. |
| `MCP_RUNTIME_AUTH_HEADER` | no | Static `Authorization` header injected on every runtime request (e.g. `Bearer …`). |
| `MCP_RUNTIME_TLS_CLIENT_CERT` / `_KEY` | no | PEM client cert / key for mTLS to the runtime. |
| `MCP_RUNTIME_TLS_CA_BUNDLE` | no | PEM CA bundle replacing the system trust store. |
| `MCP_RUNTIME_ANONYMOUS` | stdio | `true` enables anonymous mode. |
| `MCP_RUNTIME_ANONYMOUS_METHODS` | stdio | CSV allowlist of methods in anonymous mode. |
| `MCP_RUNTIME_TOOLS_CACHE_TTL` | stdio | Caches `tools/list` responses for this duration (e.g. `30s`). Anonymous mode bypasses the cache. |
| `MCP_RUNTIME_LOG_LEVEL` | no | `info` logs runtime 4xx denials to stderr. |

¹ Required unless `--server` (platform-issued session) or `--anonymous` is in
use. With `--server`, missing fields are populated from the issued response.

The adapters inject these headers on every forwarded request:

```text
X-MCP-Human-ID:      <humanID>
X-MCP-Agent-ID:      <agentID>
X-MCP-Team-ID:       <teamID>            (omitted when empty)
X-MCP-Agent-Session: <sessionID>
Authorization:       <MCP_RUNTIME_AUTH_HEADER>   (when set)
```

Incoming spoofed values for the four governance headers are stripped before
the upstream call. MCP protocol headers (`Mcp-Protocol-Version`,
`Mcp-Session-Id`, `content-type`, `accept`) are preserved.

## Anonymous mode (stdio)

For public/read-only routes — for example a catalog discovery endpoint —
identity is unnecessary and the stdio shim can run anonymous:

```bash
mcp-runtime adapter stdio \
  --runtime-url https://mcp.example.com/public-catalog/mcp \
  --anonymous \
  --anonymous-methods initialize,notifications/initialized,ping,tools/list,resources/list,prompts/list
```

Anonymous methods default to the protocol handshake plus the three read-only
discovery calls. Any method outside the allowlist is rejected with a JSON-RPC
`-32601` error before the request leaves the adapter, so an agent SDK cannot
accidentally call `tools/call` against a public route.

The `tools/list` cache is **bypassed** in anonymous mode: different anonymous
callers can see different responses depending on what the runtime exposes
publicly, and there is no safe shared cache key.

## Direct HTTP clients

When the agent framework supports Streamable HTTP MCP and custom headers,
you can call the runtime directly without the adapter. Mint a session with
the platform API once, then attach the returned identity on every request.
Use a platform login token or user API key for this call; service-only setup
keys do not carry a human subject and cannot mint adapter sessions.

```python
import asyncio
import os
import httpx

from agents import Agent, Runner
from agents.mcp import MCPServerStreamableHttp

async def main() -> None:
    platform_token = os.environ["MCP_PLATFORM_API_TOKEN"]
    async with httpx.AsyncClient() as http:
        resp = await http.post(
            os.environ["MCP_PLATFORM_API_URL"].rstrip("/")
            + "/api/runtime/adapter/sessions",
            json={
                "serverName": "workspace-assistant-mcp",
                "agentID": "ticket-triage-agent",
            },
            headers={
                "Authorization": f"Bearer {platform_token}",
            },
        )
        resp.raise_for_status()
        session = resp.json()

    async with MCPServerStreamableHttp(
        name="workspace-assistant-mcp",
        params={
            "url": os.environ["MCP_RUNTIME_URL"],
            "headers": {
                "X-MCP-Human-ID": session["humanID"],
                "X-MCP-Agent-ID": session["agentID"],
                "X-MCP-Team-ID": session.get("teamID", ""),
                "X-MCP-Agent-Session": session["name"],
            },
        },
    ) as server:
        agent = Agent(
            name="Governed Agent",
            instructions="Use MCP tools when they help.",
            mcp_servers=[server],
        )
        print((await Runner.run(agent, "Add 2 and 3.")).final_output)

asyncio.run(main())
```

This is the only path that requires the consumer to know about the platform
API. For framework code that cannot attach headers, prefer the proxy or stdio
adapter described below.

## HTTP proxy adapter

Use the proxy when a framework can speak Streamable HTTP MCP but cannot
attach the governance headers itself.

```bash
mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/workspace-assistant-mcp/mcp \
  --server workspace-assistant-mcp \
  --agent ticket-triage-agent \
  --auto-refresh
```

Then point the framework's MCP URL at the local proxy:

```text
http://127.0.0.1:8099/mcp
```

The proxy forwards to the exact `--runtime-url` route. The local request path
is accepted for client compatibility; it is not appended upstream. Query
strings from the configured URL and client request are merged. By default the
proxy adds `X-Forwarded-*` headers; set `--no-xforwarded` to suppress them on
loopback paths where they only add audit noise.

The proxy also exposes:

- `GET /healthz`, `GET /livez`, `GET /readyz` — 204 No Content when running.
- `GET /metrics` — delegates to `ProxyConfig.MetricsHandler` when wired up
  (typically a Prometheus exporter backed by `RuntimeTransport.Meter`).
  Returns 404 when no metrics handler is configured.

Inbound JSON-RPC bodies over `--max-inbound-bytes` (default 16 MiB) get HTTP
413 with a JSON-RPC parse-error body so the agent SDK can recover.

This shape works for LangChain, LlamaIndex, CrewAI, custom Python/Go/Node
services, or any other MCP-aware runtime that can talk to a Streamable HTTP
URL.

## Stdio shim

Use the shim when an IDE or client only launches stdio MCP commands (Cursor,
Claude Desktop, similar):

```json
{
  "mcpServers": {
    "workspace-assistant-mcp": {
      "command": "/absolute/path/to/bin/mcp-runtime",
      "args": [
        "adapter", "stdio",
        "--runtime-url", "https://mcp.example.com/workspace-assistant-mcp/mcp",
        "--server", "workspace-assistant-mcp",
        "--agent", "ticket-triage-agent",
        "--auto-refresh"
      ],
      "env": {
        "MCP_PLATFORM_API_URL": "https://platform.example.com",
        "MCP_PLATFORM_API_TOKEN": "..."
      }
    }
  }
}
```

The shim reads newline-delimited JSON-RPC messages from stdin, posts them to
the runtime, and writes responses back to stdout.

Behaviour worth knowing:

- `initialize` is forwarded synchronously so the runtime `Mcp-Session-Id` is
  captured before later requests. The negotiated `protocolVersion` from the
  `result` body is also captured and used on subsequent calls.
- Streamable HTTP `text/event-stream` responses are streamed through frame
  by frame so server-to-client requests and progress messages flow without
  waiting for the runtime to close the response. The shim watches each SSE
  frame for `notifications/tools/list_changed` and invalidates its
  `tools/list` cache when it sees one.
- The shim leaves `MCP_RUNTIME_REQUEST_TIMEOUT` unset by default so
  long-running tool calls are not cut off. Set it when fail-fast behaviour
  is preferable.
- Platform 4xx denials (e.g. `trust_too_low`) are returned to stdio clients
  as JSON-RPC errors. Runtime denials matching `session_expired` /
  `session_not_found` are repackaged with `error.data.runtime_status =
  "session_expired"` so the SDK can choose to re-initialize.
- Idempotent reads (`tools/list`, `resources/list`, `prompts/list`, `ping`)
  retry on `502`/`504`/connection-reset with exponential backoff (100 ms →
  200 ms → 1 s cap). `tools/call` never retries automatically.

## Expected outcomes

- A low-trust allowed tool call succeeds when the grant, session, and tool
  rule permit it.
- If the active session consents to less trust than a tool requires, the
  runtime returns `trust_too_low` and the adapter surfaces it as a JSON-RPC
  error to the client.
- Disabling or revoking the platform-side grant/session blocks calls
  without changing adapter configuration. With `--auto-refresh`, the next
  refresh tick may detect the new state (no matching grant → 403, surfaced
  in the adapter's stderr; the previous identity remains in use until
  expiry).
- Restarting the adapter against a revoked or expired session yields a
  fresh `MCPAgentSession` automatically, since the reuse predicate excludes
  revoked/near-expiry sessions.
