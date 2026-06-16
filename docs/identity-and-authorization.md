# Identity and authorization

MCP Runtime uses different identities for platform administration, delegated
agent access, and Kubernetes workloads. Keeping these identities separate makes
it clear **who is acting, what they may do, and which component enforces the
decision**.

## The three identity planes

| Identity plane | Identity shape | Used for | Enforced by |
|---|---|---|---|
| Platform | User or service principal with role, subject, teams, and namespaces | UI, CLI, and platform API operations | Sentinel API |
| Agent governance | `humanID + agentID + teamID + sessionID` | MCP `tools/call` authorization | MCP gateway |
| Kubernetes workload | ServiceAccount plus RBAC bindings | Reading and changing cluster resources | Kubernetes API server |

These identities are related, but they are not interchangeable. A platform
administrator is not automatically an agent session, and an MCP agent identity
is not a Kubernetes ServiceAccount.

## Platform identity: who controls the platform

The Sentinel API authenticates a request using one of these credentials:

- A browser or CLI bearer token issued after local or OIDC login
- A user-owned API key sent as `x-api-key`
- A configured service or administrator API key sent as `x-api-key`

Authentication produces a platform principal containing fields such as:

```text
role
subject / user ID
email
primary namespace
allowed namespaces
team memberships and team roles
authentication type
```

The API uses this principal to authorize control-plane actions.

| Actor | Allowed actions | Why |
|---|---|---|
| Anonymous caller | Health, login, OIDC exchange, and explicitly public catalog reads | These routes are public by design |
| Authenticated user | Read visible teams, namespaces, servers, and deployments | Visibility is scoped by the principal's namespaces and teams |
| Server owner | Change their server and administer its grants and sessions | The server carries the owner's platform user label |
| Team owner | Manage team members and administer servers and access resources in the team's namespace | Team-owner membership grants administrative authority within that team |
| Platform administrator | Manage teams, users, namespaces, platform operations, and resources across tenants | The `admin` role is the platform-wide control-plane authority |
| Service API key | Only the API operations permitted by its assigned role | Service authentication does not automatically create a human or agent identity |

Creating or changing an access grant requires authority over the referenced
server. In normal platform flows, that means the caller is the server owner, a
team owner for the server namespace, or a platform administrator.

Direct creation of an `MCPAgentSession` through `/api/v1/runtime/sessions` is
administrator/internal-only. Normal users obtain a session through the adapter
session endpoint.

See the complete endpoint matrix in
[Sentinel API authn/authz matrix](security/authz-matrix.md).

## Agent identity: who is using an MCP tool

An agent call carries a delegated governance identity:

```text
humanID   the platform user responsible for the call
agentID   the agent or client acting for that user
teamID    the stable team whose server and policy are in use
sessionID the active MCPAgentSession resource
```

The adapter obtains this identity from the platform and writes it to the
configured governance headers on every request. It removes caller-supplied
identity headers before applying the issued values.

In the default header mode, the gateway reads these headers. Therefore, the
adapter, ingress path, and gateway form a trust boundary: untrusted clients
should not be able to bypass the adapter and inject governance headers directly.
OAuth-configured servers additionally authenticate the bearer token at the
gateway.

## Grant: administrator-approved authority

An `MCPAccessGrant` answers:

> Which human, agent, or team may use which MCP server, with which tools,
> side effects, and maximum trust?

Its important fields are:

```yaml
spec:
  serverRef:
    name: payments
    namespace: mcp-team-finance
  subject:
    humanID: user-123
    agentID: coding-agent
    teamID: team-finance-id
  maxTrust: medium
  allowedSideEffects: [read, write]
  toolRules:
    - name: create_invoice
      decision: allow
      requiredTrust: medium
```

Every populated subject field must match the request identity. A grant bound to
all three subject fields does not match a different human, agent, or team.

The grant controls:

- Whether a specific tool is allowed or denied
- Which side-effect classes are allowed
- The administrator-approved maximum trust
- Whether the grant is disabled

The grant does not prove that the agent has a current session.

## Session: active delegated consent

An `MCPAgentSession` answers:

> Is this human-agent-team identity currently active for this server, and how
> much trust is consented for this session?

Its important fields are:

```yaml
spec:
  serverRef:
    name: payments
    namespace: mcp-team-finance
  subject:
    humanID: user-123
    agentID: coding-agent
    teamID: team-finance-id
  consentedTrust: low
  expiresAt: 2026-06-12T12:00:00Z
  revoked: false
```

For the normal adapter flow:

1. An authenticated platform user asks for a session for a server and agent.
2. The platform verifies the user's team and namespace access.
3. The platform selects a matching enabled grant.
4. Requested trust is capped by the grant's `maxTrust`.
5. The platform creates or reuses an `MCPAgentSession`.
6. The adapter injects the returned identity and session ID into MCP requests.

The session controls expiry, revocation, and consented trust. It does **not**
contain tool rules or `allowedSideEffects`; those remain grant policy.

## Tool metadata: what the server declares

When the MCP server is defined, its owner or deployer classifies each governed
tool:

```yaml
tools:
  - name: list_invoices
    requiredTrust: low
    sideEffect: read
  - name: delete_invoice
    requiredTrust: high
    sideEffect: destructive
```

The gateway does not infer risk by inspecting tool implementation. The declared
metadata is the policy input. Missing or unknown side-effect metadata causes a
denial rather than silently treating the tool as safe.

## Gateway decision for every `tools/call`

For a tool call, the gateway authorizes the intersection of identity, policy,
consent, and tool metadata:

```text
valid identity
AND valid matching session
AND matching enabled grant
AND tool allowed by the grant
AND tool side effect allowed by the grant
AND effective trust >= required trust
```

The evaluation order is:

1. Inspect the JSON-RPC request and extract the tool name.
2. Load the rendered policy for the target MCP server.
3. Read the human, agent, team, and session identity.
4. Find a non-revoked, non-expired session whose subject matches that identity.
5. Find grants whose populated subject fields match the identity.
6. Apply explicit per-tool deny or allow rules.
7. Compare the tool's declared side effect with the grant's
   `allowedSideEffects`.
8. Calculate effective trust:

   ```text
   effectiveTrust = min(grant.maxTrust, session.consentedTrust)
   ```

9. Require `effectiveTrust` to meet both the tool's and matching rule's required
   trust.
10. Forward an allowed request or return a denial without contacting the MCP
    server.
11. Emit an audit event containing the identity, tool, decision, reason, trust
    values, server, namespace, and policy version.

Example:

```text
Tool:               delete_invoice
Declared effect:    destructive
Declared trust:     high
Grant tool rule:    allow
Grant side effects: [read, write]
Grant max trust:    high
Session consent:    high
Result:             deny, side_effect_not_allowed
```

The allow rule is insufficient because authorization is an intersection. The
grant must also permit `destructive`.

## Kubernetes workload identity

The operator and Sentinel API use Kubernetes ServiceAccounts and RBAC to perform
cluster operations. This identity plane controls actions such as:

- Reading and reconciling `MCPServer`, `MCPAccessGrant`, and
  `MCPAgentSession` resources
- Creating workloads, Services, Ingresses, Secrets, and namespace-scoped access
  resources
- Reading platform configuration required by the service

Most workloads that do not call the Kubernetes API disable automatic
ServiceAccount token mounting. Kubernetes RBAC is a separate enforcement layer
from platform roles and gateway policy.

## Responsibility summary

| Question | Source of truth |
|---|---|
| Who is operating the platform? | Authenticated platform principal |
| Which tenant resources can they manage? | Role, team membership, namespace scope, and ownership |
| Which human and agent are making the tool call? | Grant/session subject and governance identity headers |
| Is that delegated access active now? | Session expiry and revocation |
| Which tools can be called? | Grant tool rules |
| Which classes of effects are permitted? | Grant `allowedSideEffects` |
| How risky is the tool? | MCPServer tool metadata |
| How much authority is effective? | Minimum of grant maximum and session consent |
| Who enforces the tool call? | MCP gateway |
| Who can change cluster resources? | Kubernetes ServiceAccount and RBAC |

**Next:** [Concepts](concepts.md) for the individual resource model, or
[Multi-Team Isolation](multi-team.md) for namespace and team boundaries.
