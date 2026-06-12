# Concepts

This page explains the core abstractions in MCP Runtime before you deploy anything.
Most concepts have direct CLI counterparts — links are included throughout.

For the complete answer to who may perform an action, under which identity, and
where that decision is enforced, see
[Identity and authorization](identity-and-authorization.md).

---

## MCPServer

An `MCPServer` is a Kubernetes CRD that describes a running MCP server: what image
to run, which port it listens on, how it routes traffic, and whether the governance
gateway is enabled.

When you run `mcp-runtime server deploy`, the operator reconciles an `MCPServer` into
a Kubernetes `Deployment`, `Service`, and `Ingress` — you never write those yourself.

```yaml
# What the operator creates from one MCPServer
Deployment  → runs your server image
Service     → exposes it inside the cluster
Ingress     → routes /<server-name>/mcp to the Service
```

The `MCPServer` also carries policy settings: which tools are governed, what trust
levels they require, and whether a gateway sidecar enforces policy on every call.

---

## MCPAccessGrant

A grant is a policy document that says: **"Agent X, acting for Team Y, is allowed
to call these tools on server Z, up to this trust level, with these side effects."**

Grants are per-agent and per-server. An agent that has no grant for a server cannot
call any tools — the gateway denies it by default.

```
MCPAccessGrant
  serverRef: payments          ← which server
  subject:
    agentID: cursor            ← which agent
    teamID: <globex-uuid>      ← which team (optional)
  maxTrust: low
  allowedSideEffects: [read]
  toolRules:
    - name: list_invoices      ← allowed
      decision: allow
      requiredTrust: low
    - name: delete_invoice     ← blocked
      decision: deny
```

Grants are created with `mcp-runtime access grant init` and applied with
`mcp-runtime access grant apply`. See [CLI reference — access](cli.md#access).

---

## MCPAgentSession

A session ties an agent identity to a grant for a fixed period of time. It carries:

- **consentedTrust** — the trust ceiling the human user has explicitly consented to
- **expiresAt** — when the session ends (agent must renew)
- **revoked** — can be set to instantly block the agent

The gateway checks the session on every tool call. If there is no valid session, or
the session is revoked, the call is denied regardless of the grant.

```
MCPAgentSession
  serverRef: payments
  subject:
    agentID: cursor
    teamID: <globex-uuid>
  consentedTrust: low          ← human approved this level
  expiresAt: 2026-06-02T12:00Z
  revoked: false
```

In normal use, sessions are created automatically by the adapter when you run
`adapter proxy --server ... --auto-refresh`. You only create them manually when you
need explicit control over expiry, trust ceiling, or revocation.

---

## Grant vs Session — when you need each

| Scenario | Grant needed? | Session needed? |
|---|---|---|
| Agent calling tools on a server | Yes | Yes |
| Block an agent from a specific tool | Yes (deny rule) | — |
| Limit trust to `low` regardless of what the agent requests | Yes (`maxTrust`) | — |
| Time-limit an agent's access | — | Yes (`expiresAt`) |
| Instantly revoke an agent mid-flight | — | Yes (`revoked: true`) |
| Share one server between two teams | Yes (with `teamID`) | Yes (with `teamID`) |

---

## Trust levels

Trust is a ceiling — the gateway only allows a tool call if the session's
`consentedTrust` meets or exceeds the tool's `requiredTrust`.

| Level | Meaning |
|---|---|
| `low` | Read-only, reversible, low-risk operations |
| `medium` | Writes or operations with noticeable side effects |
| `high` | Destructive, irreversible, or high-impact operations |

Setting `maxTrust: low` on a grant means even if the agent claims `high` trust in
its session, the gateway caps it at `low`.

---

## Side effects

Side effects classify what a tool actually does to data. The grant must explicitly
allow the side-effect class before a call reaches the server.

| Value | When to use |
|---|---|
| `read` | Fetches or queries only — no state change |
| `write` | Creates, updates, or modifies records |
| `destructive` | Deletes, wipes, or makes irreversible changes |

A grant with `allowedSideEffects: [read]` blocks any tool whose metadata declares
`sideEffect: write` or `sideEffect: destructive`, even if that tool is in the
allow list.

---

## The gateway

The gateway is a sidecar container that sits in front of every MCP server
(when `gateway.enabled: true` on the MCPServer). Every request goes through it
before reaching your server.

On each tool call the gateway:

1. Reads the `X-MCP-Agent-ID`, `X-MCP-Team-ID`, and `X-MCP-Agent-Session` headers
2. Looks up the active `MCPAgentSession` and `MCPAccessGrant` for that agent+server pair
3. Checks trust level, side-effect class, and per-tool allow/deny rules
4. Either forwards the call to your server or returns a denial with a reason code
5. Emits an analytics event with the decision

The gateway runs as a sidecar — your server code never changes to support it.

---

## Adapter

The adapter is a local proxy that runs on the developer's machine (or inside an
agent process). It:

- Calls the platform API to create a session for the agent
- Injects the correct governance headers (`X-MCP-Agent-ID`, `X-MCP-Agent-Session`, etc.)
  on every outbound request to the MCP server
- Refreshes the session automatically before it expires (`--auto-refresh`)

Without the adapter, an agent would have to manage platform sessions and inject
headers itself. The adapter makes this invisible.

```
Your MCP client → adapter proxy (localhost:8099) → gateway → MCP server
```

---

## Platform mode

Platform mode controls which namespace servers are published into and who can
browse the catalog without logging in.

| Mode | Who sees servers | Catalog namespace |
|---|---|---|
| `tenant` (default) | Only team members | Per-team namespaces |
| `org` | All signed-in users | `mcp-servers-org` |
| `public` | Anyone, no login | `mcp-servers-public` |

Set with `--platform-mode` on `setup` or `MCP_SETUP_PLATFORM_MODE` in your env file.

---

## Scopes

When publishing a server image (`registry push`) or deploying a server
(`server deploy`), `--scope` controls which catalog namespace the server lands in:

| Scope | Namespace | Who can use it |
|---|---|---|
| `tenant` | `mcp-team-<slug>` | Members of that team |
| `org` | `mcp-servers-org` | All signed-in users in the org |
| `public` | `mcp-servers-public` | Anyone |

---

## What's in a `.mcp/servers.yaml`

The `.mcp/servers.yaml` file is the metadata file that `server init` creates.
It is the single source of truth for your server's tool policy. The gateway
enforces exactly what is declared here — if a tool is not listed, calls to it
are denied.

```yaml
servers:
  - name: payments
    image: registry.example.com/acme/payments
    imageTag: v1
    scope: tenant
    tools:
      - name: list_invoices      # must match the real tool name in your server
        requiredTrust: low
        sideEffect: read
      - name: create_invoice
        requiredTrust: medium
        sideEffect: write
    policy:
      mode: allow-list
      defaultDecision: deny      # deny everything not in the list
    session:
      required: true
    gateway:
      enabled: true
```

Use `server init --from-server http://localhost:8088` to generate this file
automatically from a running server's `tools/list` response rather than writing
it by hand. Then run `server validate` before deploying to catch mismatches.


---

**Next:** [Publish an MCP Server](publish-mcp-server.md) — build, push, and deploy your first governed server.
