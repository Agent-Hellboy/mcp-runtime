# Module 1 — Core concepts

Before you deploy anything, this module explains the five key abstractions in
MCP Runtime and how they connect. Every term used in the CLI and docs traces back
to one of these.

---

## What problem is MCP Runtime solving?

When you run MCP servers in production you need answers to three questions:

1. **Who deployed this server, and can they update it?** — Kubernetes namespaces
   and RBAC provide isolation.
2. **Which agents are allowed to call which tools?** — Grants define the policy.
3. **Did this agent have consent for this call, and has it expired?** — Sessions
   carry time-bounded, revocable consent.

Without MCP Runtime you wire all three manually. With it, one CLI command handles
all three and the gateway enforces them on every call.

---

## The five abstractions

### 1. MCPServer

A Kubernetes CRD that describes a running MCP server. You create it with
`mcp-runtime server deploy`. The operator reconciles it into a `Deployment`,
`Service`, and `Ingress` — you never write those yourself.

```
MCPServer
  name: payments
  image: registry.example.com/acme/payments:v1
  port: 8088
  gateway.enabled: true   ← enables the policy sidecar
  tools:
    - name: list_invoices
      requiredTrust: low
      sideEffect: read
```

The `tools` list is critical — it is the source of truth the gateway uses
for every policy decision. If a tool is not listed here, calls to it are denied.

---

### 2. MCPAccessGrant

A policy document that says: *"This agent, acting for this team, may call
these tools on this server, up to this trust level."*

```
MCPAccessGrant
  serverRef: payments
  subject:
    agentID: cursor
    teamID: <team-uuid>       ← which team this grant covers
  maxTrust: low               ← ceiling, even if session claims higher
  allowedSideEffects: [read]
  toolRules:
    - name: list_invoices
      decision: allow
      requiredTrust: low
    - name: delete_invoice
      decision: deny          ← explicitly blocked
```

No grant = no access. The gateway denies by default.

---

### 3. MCPAgentSession

A time-bounded, revocable token that ties an agent identity to a grant.
The session carries the trust the human user has *consented to* for this
specific interaction.

```
MCPAgentSession
  serverRef: payments
  subject:
    agentID: cursor
    teamID: <team-uuid>
  consentedTrust: low    ← human approved this level
  expiresAt: ...         ← gateway rejects after this
  revoked: false         ← set true to block immediately
```

The gateway checks the session on every tool call. No valid session = denied.

In normal use the adapter creates sessions automatically (`--auto-refresh`).
You only create them manually when you need explicit control over expiry or
revocation.

---

### 4. The gateway

A sidecar container injected next to your MCP server when `gateway.enabled: true`.
Every request goes through it. On each tool call it:

1. Reads `X-MCP-Agent-ID`, `X-MCP-Team-ID`, `X-MCP-Agent-Session` headers
2. Looks up the active grant and session
3. Checks trust level, side-effect class, and per-tool rules
4. Forwards or denies
5. Emits an analytics event

Your server code never changes to support it.

```
MCP client → gateway sidecar (port 8091) → your server (port 8088)
```

---

### 5. The adapter

A local proxy you run on your machine (or inside an agent process). It:

- Calls the platform API to create a session
- Injects the right governance headers on every outbound request
- Refreshes the session before it expires (`--auto-refresh`)

```
MCP client → adapter proxy (localhost:8099) → gateway → server
```

Without the adapter your MCP client would have to manage platform sessions
and inject headers itself. The adapter makes this invisible.

---

## The decision table

| Scenario | Grant needed? | Session needed? |
|---|---|---|
| Agent calling any tools on a server | Yes | Yes |
| Block agent from a specific tool | Yes (deny rule) | — |
| Cap trust regardless of what agent claims | Yes (`maxTrust`) | — |
| Time-limit access | — | Yes (`expiresAt`) |
| Instantly revoke mid-flight | — | Yes (`revoked: true`) |
| Share one server between two teams | Yes (with `teamID`) | Yes (with `teamID`) |

---

## Trust levels

| Level | When to use |
|---|---|
| `low` | Read-only, reversible, low-risk |
| `medium` | Writes or operations with noticeable side effects |
| `high` | Destructive, irreversible, or high-impact |

`maxTrust: low` on a grant means even if the agent's session claims `high`,
the gateway caps the effective trust at `low`.

---

## Side effects

| Value | What it means |
|---|---|
| `read` | Fetches or queries — no state change |
| `write` | Creates or modifies records |
| `destructive` | Deletes, wipes, or makes irreversible changes |

A grant with `allowedSideEffects: [read]` blocks any tool whose `.mcp/servers.yaml`
metadata declares `sideEffect: write`, even if that tool is in the allow list.
This is why tool names and side effects in your metadata must match reality —
a mismatch causes `tool_side_effect_unknown` at the gateway.

---

## Check your understanding

Before moving to Module 2, you should be able to answer:

1. What does the gateway check on every tool call?
2. What is the difference between a Grant and a Session?
3. Why do tool names in `.mcp/servers.yaml` have to match the server's actual implementation?
4. What does `maxTrust: low` on a Grant mean when the Session has `consentedTrust: high`?

---

**Next:** [Module 2 — Your first governed server](module-2-first-server.md)
