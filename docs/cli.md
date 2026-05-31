# CLI reference

`mcp-runtime` is the single binary for the entire platform — cluster setup, server
deployment, access policy, and observability. This page covers every command group
with role markers, key flags, and links to the full guide for each family.

---

## Who can run what

| Badge | Role | How to authenticate |
|---|---|---|
| 👤 **User** | Team member deploying servers | `mcp-runtime auth login` (token in `~/.mcpruntime/`) |
| 🔑 **Admin** | Platform admin or operator with kube access | Platform API admin role **or** `--use-kube` + cluster-admin RBAC |
| ⚙️ **Operator** | Cluster operator | `KUBECONFIG` with cluster-admin RBAC, no platform login needed |

> **Quick rule:** developers and team members use 👤 commands. Cluster operators use ⚙️ commands. Platform admins use 🔑 commands for team/user management.

---

## Profiles (saved credentials)

Credentials are saved locally in `~/.mcpruntime/config.json`. Each login is a
named **profile** so you can switch between identities without re-typing passwords.

```bash
# Log in (saved as the default profile)
mcp-runtime auth login --api-url https://platform.example.com

# Log in under a named profile
mcp-runtime auth login --api-url https://platform.example.com \
  --email admin@example.com --password '...' --profile admin

# Switch the active profile for all subsequent commands
mcp-runtime auth use admin
mcp-runtime auth use acme-owner

# Use a different profile for one command only
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# See which profile is active
mcp-runtime auth status
```

**Environment overrides** (take precedence over any saved profile):

| Variable | Effect |
|---|---|
| `MCP_PLATFORM_API_TOKEN` | Use this token instead of the saved one |
| `MCP_PLATFORM_API_URL` | Override the API base URL |
| `MCP_PLATFORM_API_PROFILE` | Pick a profile for one command without switching |

---

## Command map

| Command | Role | What it does | Guide |
|---|---|---|---|
| `auth` | 👤 | Save and switch platform credentials | [§ auth](#auth) |
| `status` | 👤 | Platform health at a glance | [§ status](#status) |
| `server` | 👤 / 🔑 | Scaffold, build, deploy, and manage servers | [Publish a server](publish-mcp-server.md) |
| `registry` | 👤 / ⚙️ | Push images; inspect and configure the registry | [§ registry](#registry) |
| `access` | 👤 / 🔑 | Grants and sessions for the gateway policy | [API reference](api.md) |
| `adapter` | 👤 | HTTP proxy and stdio shim for agents | [Agent adapters](agent-adapters.md) |
| `team` | 🔑 | Create teams and add users | [Multi-team](multi-team.md) |
| `sentinel` | ⚙️ | Inspect and operate the analytics stack | [Sentinel](sentinel.md) |
| `bootstrap` | ⚙️ | Pre-install cluster checks | [Cluster readiness](cluster-readiness.md) |
| `setup` | ⚙️ | Install the full platform stack | [§ setup](#setup) |
| `cluster` | ⚙️ | Initialize clusters and manage cert-manager | [Deployment targets](deployment-targets.md) |

---

## Typical end-to-end flow

```
Operator          → bootstrap → setup
Admin             → team create → team user create
Developer         → auth login → server init → server validate
                  → server build image → registry push → server deploy
Admin             → access grant init → access grant apply
                  → access session init → access session apply
Agent / Developer → adapter proxy  (or adapter stdio)
```

---

## auth

👤 **User**

```bash
# Interactive
mcp-runtime auth login --api-url https://platform.example.com

# Non-interactive (CI)
mcp-runtime auth login --api-url https://platform.example.com --token-stdin < token.txt

# Email + password
mcp-runtime auth login --api-url https://platform.example.com \
  --email alice@example.com --password '...' --profile alice

# Switch / inspect / remove
mcp-runtime auth use alice
mcp-runtime auth status
mcp-runtime auth logout
```

---

## status

👤 **User** (kubeconfig optional for cluster detail)

```bash
mcp-runtime status                          # overall: registry, operator, API
mcp-runtime registry status                 # registry pod + endpoint
KUBECONFIG=~/.kube/config mcp-runtime sentinel status   # sentinel stack
```

---

## server

👤 **User** by default · 🔑 **Admin** with `--use-kube`

> **Full guide:** [Publish an MCP Server](publish-mcp-server.md)

Most day-to-day work uses the platform API. Direct CRD operations (`create`,
`apply`, `patch`, `logs`, `export`) require `--use-kube` and cluster-admin RBAC.

### Step 1 — scaffold metadata

Tool names in `.mcp/servers.yaml` **must match** your server's actual tool
implementations. Use `--from-server` to discover them automatically:

```bash
# Recommended: run your server locally, then discover tool names automatically
go run . &                                          # start your MCP server
mcp-runtime server init payments \
  --from-server http://localhost:8088               # discovers real tool names
                                                    # /mcp appended automatically

# Manual: if you already know your tool names
mcp-runtime server init payments \
  --tool list_invoices \
  --tool-spec refund_invoice:high:destructive       # name:trust:side-effect
```

### Step 2 — validate before building

Catches mismatches (missing tools, wrong side effects) that would cause
`tool_side_effect_unknown` errors at the gateway:

```bash
mcp-runtime server validate --metadata-dir .mcp
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime server validate --metadata-dir .mcp --from-server http://localhost:8088
```

### Step 3 — build, push, deploy

```bash
mcp-runtime server build image payments --tag v1 --platform linux/amd64
mcp-runtime registry push --image <exact-ref-from-build> --scope tenant
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp --update   # re-deploy
```

`--scope` values: `tenant` (your team namespace), `org`, `public`.

### Inspect

```bash
mcp-runtime server list
mcp-runtime server get payments --namespace mcp-team-acme   # --namespace required for team servers
mcp-runtime server status --namespace mcp-team-acme
mcp-runtime server policy inspect payments --namespace mcp-team-acme
mcp-runtime server delete payments
mcp-runtime server generate --metadata-dir .mcp --output manifests/   # GitOps YAML
```

### Admin / operator (🔑 --use-kube)

```bash
mcp-runtime server create payments --image repo/payments --tag v1 --use-kube
mcp-runtime server apply  --file server.yaml --use-kube
mcp-runtime server export payments --use-kube
mcp-runtime server patch  payments --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs   payments --follow --use-kube
```

---

## registry

👤 **User** for `push` · ⚙️ **Operator** for `status`, `info`, `provision`

```bash
# Inspect (operator)
mcp-runtime registry status
mcp-runtime registry info

# Configure an external registry (operator)
mcp-runtime registry provision --url registry.example.com

# Push an image (user — requires auth login)
mcp-runtime registry push --image <exact-ref-from-server-build> --scope tenant
mcp-runtime registry push --image payments:v1 --scope public
```

Always push the **exact image ref** that `server build image` printed.

---

## access

👤 **User** for grants · 🔑 **Admin** for session `apply`

> **Full reference:** [API reference](api.md)

> ⚠️ **Tool names in grants must match `.mcp/servers.yaml` exactly.** If a
> `toolRule` names a tool absent from the metadata, the gateway returns
> `tool_side_effect_unknown`. Run `server validate --grant-file grant.yaml`
> before applying.

### Grants (👤 User)

```bash
# Scaffold a grant YAML
mcp-runtime access grant init payments-ops \
  --server payments --namespace mcp-team-acme \
  --agent-id cursor \
  --tool list_invoices \
  --tool-rule refund_invoice:allow:high \   # name:allow|deny:trust
  --output grant.yaml

# Validate grant before applying
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

# Apply / manage
mcp-runtime access grant apply  --file grant.yaml
mcp-runtime access grant list
mcp-runtime access grant list   --namespace mcp-team-acme
mcp-runtime access grant get    payments-ops --namespace mcp-team-acme
mcp-runtime access grant disable payments-ops --namespace mcp-team-acme
mcp-runtime access grant enable  payments-ops --namespace mcp-team-acme
mcp-runtime access grant delete  payments-ops --namespace mcp-team-acme
```

### Sessions (🔑 Admin for `apply`)

Agents normally get sessions automatically via `adapter --auto-refresh`. Use
`session init` + `session apply` only when you need an explicit session.

```bash
mcp-runtime access session init cursor-session \
  --server payments --namespace mcp-team-acme \
  --agent-id cursor --trust low --expires-in 2h \
  --output session.yaml

mcp-runtime access session apply    --file session.yaml   # admin only
mcp-runtime access session list
mcp-runtime access session get      cursor-session --namespace mcp-team-acme
mcp-runtime access session revoke   cursor-session --namespace mcp-team-acme
mcp-runtime access session unrevoke cursor-session --namespace mcp-team-acme
```

### Cross-team access

Team A can grant Team B's agents access to Team A's servers:

```bash
# Team A owner grants Team B's cursor agent access to payments
mcp-runtime access grant init payments-to-teamB \
  --server payments --namespace mcp-team-acme \
  --team-id <team-b-uuid> --agent-id cursor \
  --tool list_invoices --output grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Admin creates a session for Team B's agent
mcp-runtime access session init teamB-session \
  --server payments --namespace mcp-team-acme \
  --team-id <team-b-uuid> --agent-id cursor \
  --trust low --expires-in 2h --output session.yaml
MCP_PLATFORM_API_PROFILE=admin mcp-runtime access session apply --file session.yaml
```

See [Multi-team isolation](multi-team.md) for namespace layout and RBAC.

---

## adapter

👤 **User** — injects governance headers before requests reach the MCP server.

> **Full guide:** [Agent adapters](agent-adapters.md)

When `--server` is set the adapter creates a platform session automatically
(`--auto-refresh` keeps it alive). Use `--agent-id` to set the agent identity
header sent to the server.

```bash
# HTTP reverse proxy (agents call http://127.0.0.1:8099)
mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/payments/mcp \
  --server payments \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099

# stdio shim (for Claude Desktop / local agent processes)
mcp-runtime adapter stdio \
  --runtime-url https://mcp.example.com/payments/mcp \
  --server payments \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh
```

`--agent` is the session name (used to create/refresh the platform session).
`--agent-id` is the identity injected as `X-MCP-Agent-ID` on each request.

---

## team

🔑 **Admin** — all `team` commands require the platform API admin role.

> **Full guide:** [Multi-team isolation](multi-team.md)

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Create a team (also creates the Kubernetes namespace with RBAC)
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"

# Add a password-login user
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@example.com --password '...' --role owner
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user list acme
```

Users log in with `auth login --email alice@example.com --password '...'`.

> ⚠️ `team init` is **deprecated**. Use `team create`.

---

## sentinel

⚙️ **Operator** — requires `KUBECONFIG` with cluster-admin RBAC.

> **Full guide:** [Sentinel](sentinel.md)

```bash
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

# Logs
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs ingest --tail 200

# Restart
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

# Open a local port to a component
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward ui      # :8082
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward grafana # :3000
```

Component names for `logs` / `restart`:
`clickhouse`, `zookeeper`, `kafka`, `ingest`, `processor`, `api`, `ui`,
`gateway`, `prometheus`, `grafana`, `otel-collector`, `tempo`, `loki`, `promtail`.

---

## bootstrap

⚙️ **Operator** — run before `setup` on a fresh cluster.

> **Full guide:** [Cluster readiness](cluster-readiness.md)

```bash
mcp-runtime bootstrap                        # check DNS, StorageClass, IngressClass
mcp-runtime bootstrap --provider k3s        # k3s-aware checks
mcp-runtime bootstrap --apply --provider k3s  # auto-fix on k3s (installs CoreDNS etc.)
```

---

## setup

⚙️ **Operator** — installs the full platform stack.

Runs pre-flight checks before applying anything. Use `--env-file` to drive all
flags from a file (see `config/deployments/mcpruntime-org.env.example`):

```bash
# Recommended: drive everything from an env file
mcp-runtime setup --env-file config/deployments/mcpruntime-org.env

# Common flags (same as env vars below)
mcp-runtime setup \
  --with-tls --tls-cluster-issuer letsencrypt-prod \
  --registry-mode bundled-https \
  --platform-mode tenant \
  --ingress none

mcp-runtime setup --with-tls --acme-email ops@example.com   # Let's Encrypt
mcp-runtime setup --without-sentinel                         # skip analytics
mcp-runtime setup --test-mode                                # local Kind dev
```

**Key env vars** (`--env-file` equivalents):

| Env var | Flag |
|---|---|
| `MCP_PLATFORM_DOMAIN=example.com` | derives `registry.`, `mcp.`, `platform.` hostnames |
| `MCP_SETUP_WITH_TLS=1` | `--with-tls` |
| `MCP_SETUP_TLS_CLUSTER_ISSUER=letsencrypt-prod` | `--tls-cluster-issuer` |
| `MCP_SETUP_REGISTRY_MODE=bundled-https` | `--registry-mode` |
| `MCP_SETUP_PLATFORM_MODE=tenant` | `--platform-mode` |
| `MCP_SETUP_INGRESS=none` | `--ingress` |
| `MCP_SETUP_SKIP_CERT_MANAGER_INSTALL=1` | `--skip-cert-manager-install` |

`--registry-mode`: `auto` · `bundled-http` · `bundled-https` · `external`
`--platform-mode`: `tenant` (default) · `org` · `public`

---

## cluster

⚙️ **Operator**

> **Full guide:** [Deployment targets](deployment-targets.md)

```bash
mcp-runtime cluster init                                         # install CRDs + namespaces
mcp-runtime cluster config --ingress traefik                     # configure ingress
mcp-runtime cluster provision --provider kind --nodes 3          # create a Kind cluster
mcp-runtime cluster provision --provider eks --name prod-mcp

mcp-runtime cluster cert status                                  # cert-manager TLS state
mcp-runtime cluster cert apply
mcp-runtime cluster cert wait --timeout 10m

KUBECONFIG=~/.kube/config mcp-runtime cluster doctor            # 37-point post-install check
```

Active providers: `kind`, `eks`. (`gke` / `aks` exist but provisioning is not yet implemented.)

---

## Full example: two teams, cross-team access

```bash
# ── 1. Operator: install ─────────────────────────────────────────────────────
KUBECONFIG=~/.kube/config mcp-runtime bootstrap
KUBECONFIG=~/.kube/config mcp-runtime setup \
  --env-file config/deployments/mcpruntime-org.env

# ── 2. Admin: create two teams ───────────────────────────────────────────────
mcp-runtime auth login --api-url https://platform.example.com \
  --email admin@example.com --password '...' --profile admin

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@example.com --password '...' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create globex --name "Globex Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create globex \
  --username bob@example.com --password '...' --role member

# ── 3. Developer (Acme): deploy a server ─────────────────────────────────────
mcp-runtime auth login --api-url https://platform.example.com \
  --email alice@example.com --password '...' --profile alice

mcp-runtime auth use alice

# Run server locally, discover real tool names, validate, build, push, deploy
go run ./payments &
mcp-runtime server init payments --from-server http://localhost:8088
mcp-runtime server validate --metadata-dir .mcp
mcp-runtime server build image payments --tag v1
mcp-runtime registry push --image registry.example.com/acme/payments:v1 --scope tenant
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp

# ── 4. Acme grants Globex access ─────────────────────────────────────────────
GLOBEX_TEAM_ID="<uuid from team list>"

mcp-runtime access grant init payments-to-globex \
  --server payments --namespace mcp-team-acme \
  --team-id $GLOBEX_TEAM_ID --agent-id cursor \
  --tool list_invoices --output grant.yaml

mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml

# ── 5. Admin creates a session for Globex's cursor agent ─────────────────────
mcp-runtime auth use admin

mcp-runtime access session init globex-session \
  --server payments --namespace mcp-team-acme \
  --team-id $GLOBEX_TEAM_ID --agent-id cursor \
  --trust low --expires-in 4h --output session.yaml
mcp-runtime access session apply --file session.yaml

# ── 6. Globex agent connects via adapter ─────────────────────────────────────
mcp-runtime auth use bob   # (after bob logs in)

mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/payments/mcp \
  --server payments --agent cursor --agent-id cursor \
  --auto-refresh --listen 127.0.0.1:8099
# → agent calls http://127.0.0.1:8099 with standard MCP

# ── 7. Inspect ───────────────────────────────────────────────────────────────
mcp-runtime status
mcp-runtime server list
mcp-runtime access grant list
mcp-runtime access session list
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 10m
```

---

## Further reading

| Topic | Link |
|---|---|
| Build → push → deploy flow with `server validate` | [Publish an MCP Server](publish-mcp-server.md) |
| MCPServer, MCPAccessGrant, MCPAgentSession field reference | [API reference](api.md) |
| HTTP proxy and stdio adapter config | [Agent adapters](agent-adapters.md) |
| Multi-team namespaces and RBAC | [Multi-team isolation](multi-team.md) |
| Sentinel logs, events, restart, port-forward | [Sentinel](sentinel.md) |
| Distro-specific cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| Kind, EKS, k3s deployment | [Deployment targets](deployment-targets.md) |
