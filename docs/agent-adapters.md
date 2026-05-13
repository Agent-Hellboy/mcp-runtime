# Agent Adapters

MCP Runtime includes two optional agent-side adapters for frameworks and IDEs
that need help attaching governed identity to MCP traffic:

- `mcp-runtime adapter proxy` exposes a local Streamable HTTP MCP endpoint and
  forwards requests to an MCP Runtime route.
- `mcp-runtime adapter stdio` exposes a stdio MCP server process and forwards
  each JSON-RPC message to the same MCP Runtime HTTP route.

Both adapters only present issued identity values. They do not create grants,
create sessions, evaluate policy, or bypass the gateway. Platform admins still
grant access through `MCPAccessGrant` and `MCPAgentSession`; the gateway remains
the enforcement point.

The adapter surface is intentionally limited to stdio and Streamable HTTP, the
two standard MCP transports. There is no separate legacy HTTP+SSE adapter. When
the docs or code mention `text/event-stream`, that is only because Streamable
HTTP allows a server to return a JSON response or an event-stream response for
the same request, and clients are expected to handle both response shapes.

## Configuration

Set these values for both adapters:

| Environment variable | Required | Purpose |
|---|---:|---|
| `MCP_RUNTIME_URL` | yes | Absolute Streamable HTTP MCP route, such as `http://localhost:18080/go-example-mcp/mcp`. |
| `MCP_RUNTIME_HUMAN_ID` | yes | Human identity issued by the platform/admin flow. |
| `MCP_RUNTIME_AGENT_ID` | yes | Agent identity issued by the platform/admin flow. |
| `MCP_RUNTIME_SESSION_ID` | yes | `MCPAgentSession` name/value to present in `X-MCP-Agent-Session`. |
| `MCP_RUNTIME_HOST_HEADER` | no | Optional upstream `Host` header for host-based ingress. |
| `MCP_RUNTIME_LISTEN_ADDR` | proxy only | Local proxy listen address. Defaults to `127.0.0.1:8099`. |
| `MCP_RUNTIME_PROTOCOL_VERSION` | shim only | MCP protocol header for stdio-to-HTTP calls. Defaults to `2025-06-18`; an `initialize.params.protocolVersion` value overrides it for that shim process. |
| `MCP_RUNTIME_SET_XFF` | proxy only | Set to `false`, `0`, `no`, or `off` to suppress proxy-generated `X-Forwarded-*` headers. Defaults to enabled. |
| `MCP_RUNTIME_REQUEST_TIMEOUT` | shim only | Optional Go duration such as `300s` for stdio-to-HTTP requests. Defaults to unbounded so long-running tools are not cut off. |
| `MCP_RUNTIME_LOG_LEVEL` | no | Set to `info` to log runtime 4xx denials to stderr with status, reason, method, and tool name. Defaults to silent. |

The adapters inject these governance headers on every forwarded request:

```text
X-MCP-Human-ID: <MCP_RUNTIME_HUMAN_ID>
X-MCP-Agent-ID: <MCP_RUNTIME_AGENT_ID>
X-MCP-Agent-Session: <MCP_RUNTIME_SESSION_ID>
```

Incoming spoofed values for those three headers are overwritten. MCP protocol
headers such as `Mcp-Protocol-Version`, `Mcp-Session-Id`, `content-type`, and
`accept` are preserved for HTTP proxy traffic. The stdio shim stores the
runtime `Mcp-Session-Id` returned by `initialize` and sends it on later HTTP
requests.

The HTTP proxy streams `text/event-stream` responses through as they arrive and
returns JSON-RPC error envelopes for upstream connection failures. That keeps
agent clients on the MCP response shape instead of receiving a generic HTML
`502 Bad Gateway` body.

## Admin Flow

Apply grants and sessions before giving an adapter config to an agent builder:

```bash
./bin/mcp-runtime access grant apply --file grant.yaml
./bin/mcp-runtime access session apply --file session.yaml
./bin/mcp-runtime server policy inspect go-example-mcp --namespace mcp-servers
```

Minimal example:

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ticket-triage-agent
  namespace: mcp-servers
spec:
  serverRef:
    name: go-example-mcp
  subject:
    humanID: support-lead
    agentID: ticket-triage-agent
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
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: sess-ticket-triage-agent
  namespace: mcp-servers
spec:
  serverRef:
    name: go-example-mcp
  subject:
    humanID: support-lead
    agentID: ticket-triage-agent
  consentedTrust: high
  policyVersion: v1
```

## Direct HTTP Clients

Use direct HTTP when the framework supports Streamable HTTP MCP and custom
request headers. For example, the OpenAI Agents SDK exposes
`MCPServerStreamableHttp` with `params.url` and `params.headers` for local or
remote Streamable HTTP MCP servers.

```python
import asyncio
import os

from agents import Agent, Runner
from agents.mcp import MCPServerStreamableHttp

async def main() -> None:
    async with MCPServerStreamableHttp(
        name="go-example-mcp",
        params={
            "url": os.environ["MCP_RUNTIME_URL"],
            "headers": {
                "X-MCP-Human-ID": os.environ["MCP_RUNTIME_HUMAN_ID"],
                "X-MCP-Agent-ID": os.environ["MCP_RUNTIME_AGENT_ID"],
                "X-MCP-Agent-Session": os.environ["MCP_RUNTIME_SESSION_ID"],
            },
        },
    ) as server:
        agent = Agent(
            name="Governed Agent",
            instructions="Use MCP tools when they help.",
            mcp_servers=[server],
        )
        result = await Runner.run(agent, "Add 2 and 3.")
        print(result.final_output)

asyncio.run(main())
```

## HTTP Proxy Adapter

Use the proxy when a framework can speak Streamable HTTP MCP but cannot attach
the governance headers itself.

```bash
export MCP_RUNTIME_URL=http://localhost:18080/go-example-mcp/mcp
export MCP_RUNTIME_HUMAN_ID=support-lead
export MCP_RUNTIME_AGENT_ID=ticket-triage-agent
export MCP_RUNTIME_SESSION_ID=sess-ticket-triage-agent

./bin/mcp-runtime adapter proxy
```

Then point the framework's MCP HTTP URL at the local proxy:

```text
http://127.0.0.1:8099/mcp
```

The proxy forwards to the exact `MCP_RUNTIME_URL` route. The local request path
is accepted for client compatibility; it is not appended to the upstream route.
Query strings from the configured URL and client request are merged.
By default the proxy adds `X-Forwarded-For`, `X-Forwarded-Host`, and
`X-Forwarded-Proto`; set `MCP_RUNTIME_SET_XFF=false` when the local loopback
address only adds audit noise.

This shape works for LangChain, LlamaIndex, CrewAI, custom Python/Go/Node
services, or any other MCP-aware runtime that can connect to a Streamable HTTP
MCP URL.

## Stdio Shim

Use the shim when an IDE or client only launches stdio MCP commands.

```json
{
  "mcpServers": {
    "go-example-mcp": {
      "command": "/absolute/path/to/bin/mcp-runtime",
      "args": ["adapter", "stdio"],
      "env": {
        "MCP_RUNTIME_URL": "http://localhost:18080/go-example-mcp/mcp",
        "MCP_RUNTIME_HUMAN_ID": "support-lead",
        "MCP_RUNTIME_AGENT_ID": "ticket-triage-agent",
        "MCP_RUNTIME_SESSION_ID": "sess-ticket-triage-agent"
      }
    }
  }
}
```

The shim reads newline-delimited JSON-RPC messages from stdin, posts them to
`MCP_RUNTIME_URL`, and writes JSON-RPC responses to stdout. HTTP denials from
the platform, such as `trust_too_low`, are returned to stdio clients as JSON-RPC
errors so the client sees the governed failure instead of a silent transport
drop.

For Streamable HTTP event-stream responses, the shim writes each valid JSON-RPC
`data:` frame to stdout as it arrives and keeps reading stdin while the upstream
stream remains open. That lets server-to-client requests and progress messages
flow through without waiting for the runtime to close the HTTP response.

The shim exits on process context cancellation even when stdin is idle.
`initialize` is forwarded synchronously so the runtime session ID is captured
before later requests. Leave `MCP_RUNTIME_REQUEST_TIMEOUT` unset for unbounded
tool calls, or set it to a duration when a demo or local integration should
fail fast if the runtime stops responding.

## Expected Outcomes

- A low-trust allowed tool call succeeds when the grant, session, and tool rule
  permit it.
- If the active session consents to less trust than the tool requires, the
  runtime returns a denial such as `trust_too_low`.
- Removing, disabling, or revoking the platform-side grant/session blocks calls
  without changing adapter configuration.
