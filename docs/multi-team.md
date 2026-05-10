# Multi-Team Isolation

MCP Runtime's beta multi-team model uses both first-class team identity and
Kubernetes namespace boundaries. `MCPServer.spec.teamID` records the owning
platform team. `SubjectRef.teamID` constrains grants and sessions to callers
from that team. Namespaces and RBAC still isolate who can create resources.

The source-of-truth data plane is:

- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` are namespaced resources.
- `MCPServer.spec.teamID` is the stable platform team ID that owns the server.
- `SubjectRef` contains `humanID`, `agentID`, and `teamID`; the gateway matches
  every non-empty field by exact string equality.
- A subject with only `teamID` grants or binds any authenticated principal in
  that team.
- The gateway reads team identity from `spec.auth.teamIDHeader` in header mode
  or from OAuth `team_id`, `tenant_id`, or `tid` claims in OAuth mode.

## When To Use This

The default `mcp-servers` namespace is fine for a single team, local
development, and simple evaluation clusters. For a deployment that hosts
multiple teams or tenant boundaries, use one namespace per team.

Recommended namespace shape:

| Team | Namespace | Resources in that namespace |
|---|---|---|
| Acme | `mcp-team-acme` | Acme `MCPServer`, `MCPAccessGrant`, `MCPAgentSession`, secrets, configmaps |
| Globex | `mcp-team-globex` | Globex `MCPServer`, `MCPAccessGrant`, `MCPAgentSession`, secrets, configmaps |

Keep each server's grants and sessions in the same namespace as the server they
govern. When `serverRef.namespace` is omitted, clients and renderers should
treat the current resource namespace as the intended boundary.

## Provisioning A Team Namespace

There are two supported provisioning paths.

If the platform API is configured, admins can create a managed team namespace
through the platform-backed team command:

```bash
mcp-runtime auth login --api-url https://platform.example.com
mcp-runtime team create acme --name "Acme"
mcp-runtime team list
```

`team create` records the team in the platform store and asks the API to ensure
the managed namespace, quota, limit range, default deny network policy, and
default service account. Platform API writes into that namespace default
`spec.teamID` and `subject.teamID` from the authenticated principal's team.

For direct Kubernetes administration, use `team init`:

```bash
mcp-runtime team init acme --group acme-mcp-admins
```

`team init` applies the namespace, a restricted `mcp-workload` service account,
a default quota, default container limits, a default-deny NetworkPolicy that
allows same-namespace ingress, bundled Traefik ingress, DNS, and basic outbound
HTTP/S egress, a team-admin `Role`, a `RoleBinding`, and the `traefik-watch`
`Role`/`RoleBinding` for the bundled Traefik service account. It also patches
the bundled `traefik/traefik` Deployment so
`--providers.kubernetesingress.namespaces` includes the new team namespace. Use
`--dry-run` to print the generated manifest, and use `--skip-traefik-watch`
when your ingress controller is external or managed outside this repo.

The generated namespace/RBAC shape is:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: mcp-team-acme
  labels:
    mcpruntime.org/scope: team
    mcpruntime.org/team-slug: acme
    pod-security.kubernetes.io/enforce: restricted
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: mcp-runtime-team-admin
  namespace: mcp-team-acme
rules:
  - apiGroups: ["mcpruntime.org"]
    resources: ["mcpservers", "mcpaccessgrants", "mcpagentsessions"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods", "pods/log", "events", "services"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
```

`kubectl apply` uses create, update, and patch under the hood; Kubernetes has no
separate `apply` verb.

## Team Fields

Server ownership:

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: payments
  namespace: mcp-team-acme
spec:
  teamID: 7d0a0b8f-7c25-4761-a632-3cf0108e31d6
  image: registry.example.com/acme/payments:latest
```

Team-wide access:

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: payments-acme-read
  namespace: mcp-team-acme
spec:
  serverRef: {name: payments}
  subject: {teamID: 7d0a0b8f-7c25-4761-a632-3cf0108e31d6}
  maxTrust: low
  allowedSideEffects: [read]
```

Specific human or agent inside a team:

```yaml
subject:
  humanID: alice@example.com
  agentID: acme-cron-bot
  teamID: 7d0a0b8f-7c25-4761-a632-3cf0108e31d6
```

When more than one subject field is set, every field must match the request
identity. This means a moved user stops matching the old team's grants as soon
as their trusted `teamID` claim/header changes.

## Gateway Identity

Header mode defaults:

| Identity | Header |
|---|---|
| `humanID` | `X-MCP-Human-ID` |
| `agentID` | `X-MCP-Agent-ID` |
| `teamID` | `X-MCP-Team-ID` |
| `sessionID` | `X-MCP-Agent-Session` |

Override the team header per server with `spec.auth.teamIDHeader`. In OAuth
mode, the proxy validates the token and reads team identity from `team_id`,
`tenant_id`, or `tid`, in that order.

## Platform API Enforcement

The platform API fails closed for team-scoped writes:

- Non-admin callers cannot write servers, grants, or sessions into the shared
  `mcp-servers` catalog namespace.
- Non-admin callers can only read or write resources in namespaces listed on
  their authenticated principal.
- Server apply defaults `spec.teamID` from the principal's team namespace and
  rejects mismatches.
- Grant/session apply defaults `subject.teamID` from the referenced server or
  namespace team and rejects mismatches with the referenced server's
  `spec.teamID`.
- A grant or session must reference an `MCPServer` in the same namespace as the
  access resource. Cross-namespace `serverRef.namespace` values are rejected.
- Admin callers keep cluster-wide visibility, but same-namespace and team-ID
  consistency checks still apply to access resources.

Direct `kubectl apply` still depends on Kubernetes RBAC. Bind team admins only
inside their team namespace. As a defense-in-depth guard, the operator renders
only grants and sessions that match the target server team; missing
`subject.teamID` values are scoped to `MCPServer.spec.teamID`, and mismatched
team subjects are omitted from the gateway policy.

## Ingress Controller Watch Scope

The bundled Traefik manifests watch only `registry`, `mcp-sentinel`, and
`mcp-servers` by default so Traefik does not need broad namespace access. If MCP
servers live in team namespaces, update the ingress controller watch list, bind
the Traefik watch role in each team namespace, and allow ingress-controller
traffic through the namespace NetworkPolicy. `mcp-runtime team init` performs
those changes for the repo-managed `traefik/traefik` Deployment.

For the bundled Traefik overlay, extend the argument:

```text
--providers.kubernetesingress.namespaces=registry,mcp-sentinel,mcp-servers,mcp-team-acme,mcp-team-globex
```

External ingress controllers need equivalent namespace watch, RBAC, and
NetworkPolicy configuration.

## Identifier Conventions

Keep identifiers stable:

| Field | Recommendation |
|---|---|
| `teamID` | Use the platform store team UUID or another immutable identity-provider tenant/team ID. Do not use a mutable display name. |
| `humanID` | Use the identity provider's stable subject claim, or email when that is stable in your environment. |
| `agentID` | Use a readable owner-purpose string such as `acme-cron-bot`, `globex-data-loader`, or `claude-code`. |

`mcp-runtime access grant apply` and `mcp-runtime access session apply` run a
non-blocking advisory pass before applying manifests. The command warns about
obvious `humanID` shape problems, such as whitespace, malformed email-like
strings, case-sensitive uppercase email identifiers, or values that appear to
encode `mcp-team-*` namespace names. These warnings never block the apply.

## Audit And Reporting

Gateway analytics events carry target `team_id` from `MCPServer.spec.teamID`
and caller `subject_team_id` from the request identity, alongside server,
namespace, human ID, agent ID, session, tool, and decision. ClickHouse
materializes `team_id`, so team-scoped reporting can filter directly on the
server owner's team without joining through namespace names.

## Known Limits

- Team-wide grants require a trusted `teamID` header or OAuth team claim.
- The evaluator does exact string matching. It does not resolve live group
  membership from the platform database.
- Direct `kubectl apply` can bypass platform API defaulting; Kubernetes RBAC is
  the guardrail for direct writers.
- Cross-team server sharing is a privileged pattern. Prefer a shared namespace
  and explicit admin-owned grants when a server is intentionally shared.
- The default setup path still creates `mcp-servers`; per-team namespaces are an
  explicit operational step.

## Operational Checklist

1. Create one namespace per team or tenant boundary.
2. Bind team admins only to their namespace.
3. Put each team's `MCPServer`, `MCPAccessGrant`, `MCPAgentSession`, analytics
   secrets, and image pull secrets in that namespace.
4. Set `spec.teamID` on every team-owned `MCPServer`.
5. Set `subject.teamID` on grants and sessions, or use the platform API so it
   defaults the value.
6. Configure trusted header or OAuth team identity extraction.
7. Add team namespaces to the ingress controller watch list and RBAC.

## Related Docs

- [Getting Started](getting-started.md) for the single-namespace local flow.
- [CLI](cli.md) for `team`, `access`, and namespace-scoped commands.
- [Runtime](runtime.md) for CRD and reconciliation behavior.
- [API Reference](api.md) for access resource fields.
