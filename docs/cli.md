# CLI reference

`mcp-runtime` is the single binary for the entire platform — from cluster bootstrap
through server deployment, access policy, and runtime observability. Commands are
grouped into families; each family has a dedicated doc page linked below.

## Who can run what

Two access layers control what each command can do:

| Layer | How to authenticate | Commands |
|---|---|---|
| **Platform API** | `auth login` → token saved in `~/.mcpruntime/config.json` | `server`, `access`, `registry push`, `team`, `adapter` |
| **Kubernetes (admin)** | `KUBECONFIG` / kubeconfig file with cluster-admin RBAC | `bootstrap`, `setup`, `cluster`, `sentinel`, `server --use-kube`, `access --use-kube` |

> **Quick rule:** if you're a developer or team member deploying MCP servers, use Platform API
> commands. If you're an operator setting up the cluster or debugging the stack, use the
> Kubernetes commands.

### Role legend used in this page

| Badge | Meaning |
|---|---|
| 👤 **User** | Platform API; requires `auth login` or `MCP_PLATFORM_API_TOKEN` |
| 🔑 **Admin** | Platform API admin role *or* `--use-kube` (cluster-admin RBAC) |
| ⚙️ **Operator** | Kubernetes only; cluster-admin RBAC required |

## Credentials and profiles

Login credentials are stored locally at `~/.mcpruntime/config.json`. Each login is
saved as a **named profile** so you can switch between admin, team user, and
multi-tenant identities without re-typing credentials.

```bash
# Save credentials under the default profile
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# Save under a named profile
mcp-runtime auth login --api-url https://platform.mcpruntime.org \
  --email admin@example.com --password '...' --profile admin

# Switch the active profile
mcp-runtime auth use admin
mcp-runtime auth use acme-owner

# Use a profile for a single command without switching
MCP_PLATFORM_API_PROFILE=admin mcp-runtime server list
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime team list

# Check which profile is active and what token it holds
mcp-runtime auth status
```

**Environment overrides** (take precedence over saved profiles):

| Variable | Effect |
|---|---|
| `MCP_PLATFORM_API_TOKEN` | Use this token instead of the saved one |
| `MCP_PLATFORM_API_URL` | Override the saved API base URL |
| `MCP_PLATFORM_API_PROFILE` | Select a saved profile without switching the current one |

## Fast path

```bash
# Operator: cluster + platform install
mcp-runtime bootstrap
mcp-runtime setup --env-file config/deployments/mcpruntime-org.env

# Developer: log in, deploy a server, grant access
mcp-runtime auth login --api-url https://platform.mcpruntime.org
mcp-runtime server init payments --tool list_invoices
mcp-runtime server build image payments --tag v1
mcp-runtime registry push --image <image-ref> --scope tenant
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp

mcp-runtime access grant init payments-ops \
  --server payments --agent-id ops-agent --tool list_invoices --output grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Check everything
mcp-runtime status
```

Use built-in help for the exact flags and defaults of any command:

```bash
mcp-runtime --help
mcp-runtime <group> --help
mcp-runtime <group> <subcommand> --help
```

## Command map

| Group | Role | What it covers | Full reference |
|---|---|---|---|
| `auth` | 👤 User | Save and switch platform API credentials | [this page §auth](#auth) |
| `status` | 👤 User | Aggregated platform health summary | [this page §status](#status) |
| `server` | 👤 User / 🔑 Admin | Deploy and manage `MCPServer` resources | [Publish an MCP Server](publish-mcp-server.md) |
| `registry` | 👤 User / ⚙️ Operator | Push images; inspect and provision the registry | [this page §registry](#registry) |
| `access` | 👤 User / 🔑 Admin | Manage access grants and agent sessions | [API reference](api.md) |
| `adapter` | 👤 User | HTTP proxy and stdio shims for agent runtimes | [Agent adapters](agent-adapters.md) |
| `team` | 🔑 Admin | Manage platform teams and team users | [Multi-team isolation](multi-team.md) |
| `sentinel` | ⚙️ Operator | Inspect and operate the analytics/observability stack | [Sentinel](sentinel.md) |
| `bootstrap` | ⚙️ Operator | Preflight checks for cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| `setup` | ⚙️ Operator | Full platform install: operator, registry, TLS, sentinel | [this page §setup](#setup) |
| `cluster` | ⚙️ Operator | Initialize, configure, provision clusters; cert-manager | [Deployment targets](deployment-targets.md) |
| `completion` | 👤 User | Shell completion scripts | — |

---

## auth

👤 **User** — platform API credentials only; no Kubernetes required.

Save tokens locally and switch between profiles for multi-platform or multi-role
workflows. These credentials are used by `server`, `access`, `registry push`,
`team`, and `adapter` commands.

```bash
# Interactive login (prompts for token)
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# Non-interactive (CI / scripted)
mcp-runtime auth login --api-url https://platform.mcpruntime.org --token-stdin < token.txt

# Email + password login
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email admin@example.com --password '...' --profile admin

# Record the registry host alongside the token
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --token-stdin --registry-host registry.mcpruntime.org < token.txt

# Switch profiles
mcp-runtime auth use admin
mcp-runtime auth use acme-owner

# Check active credentials
mcp-runtime auth status

# Remove saved credentials
mcp-runtime auth logout
```

Notes:
- Credentials live in `~/.mcpruntime/config.json` (mode 0600)
- Each `auth login` saves a profile and makes it current
- `MCP_PLATFORM_API_PROFILE` selects a profile for a single command without switching

---

## status

👤 **User** — reads from the platform API and from Kubernetes if kubeconfig is available.

```bash
mcp-runtime status           # aggregated: cluster, registry, operator, sentinel
mcp-runtime cluster status   # cluster-level health
mcp-runtime registry status  # registry deployment + endpoint
mcp-runtime sentinel status  # sentinel stack (needs kubeconfig)
```

---

## server

👤 **User** by default (platform API) · 🔑 **Admin** with `--use-kube`.

Scaffold, build, push, and deploy `MCPServer` resources. Most day-to-day operations
go through the platform API — only manifest-level operations (`create`, `apply`,
`patch`, `logs`, `export`) require `--use-kube` and cluster-admin RBAC.

> **Full guide:** [Publish an MCP Server](publish-mcp-server.md)

### Platform API commands (👤 User)

```bash
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# List and inspect
mcp-runtime server list
mcp-runtime server get payments --namespace mcp-team-acme   # always pass --namespace for team servers
mcp-runtime server status
mcp-runtime server status --namespace mcp-team-acme
mcp-runtime server policy inspect payments --namespace mcp-team-acme

# Scaffold server metadata
# Option A: manually specify tools (names MUST match your server implementation)
mcp-runtime server init payments --tool list_invoices --tool refund_invoice
mcp-runtime server init payments --tool-spec list_invoices:low:read --tool-spec refund_invoice:high:destructive

# Option B: auto-discover tools from a locally running MCP server (recommended)
# Run your server locally first, then:
mcp-runtime server init payments --from-server http://localhost:8088

# Validate metadata and grant/session YAML before deploying (catches tool name mismatches)
mcp-runtime server validate --metadata-dir .mcp
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml --session-file session.yaml
mcp-runtime server validate --metadata-dir .mcp --from-server http://localhost:8088   # verify against live server

# Build the image
mcp-runtime server build image payments --tag v1 --platform linux/amd64

# Deploy (after pushing the image)
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp --update   # redeploy

# Delete
mcp-runtime server delete payments

# Generate GitOps manifests
mcp-runtime server generate --metadata-dir .mcp --output manifests/
```

### Admin / operator commands (🔑 --use-kube)

```bash
# Direct CRD operations — requires cluster-admin RBAC
mcp-runtime server create payments --image repo/payments --tag latest --use-kube
mcp-runtime server create payments --file server.yaml --use-kube
mcp-runtime server apply --file server.yaml --use-kube
mcp-runtime server export payments --file payments.yaml --use-kube
mcp-runtime server patch payments --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs payments --follow --use-kube
```

`--scope` values: `tenant` (team namespace), `org` (`mcp-servers-org`), `public` (`mcp-servers-public`).
`server deploy --update` is required for repeat deploys of the same server name.

---

## registry

👤 **User** for `push` · ⚙️ **Operator** for `provision` and `status`.

```bash
# Inspect (operator)
mcp-runtime registry status
mcp-runtime registry info

# Configure an external registry (operator)
mcp-runtime registry provision --url registry.example.com
mcp-runtime registry provision \
  --url registry.example.com \
  --operator-image registry.example.com/mcp-runtime-operator:latest

# Push images (user — requires auth login)
mcp-runtime registry push --image registry.mcpruntime.org/payments:v1
mcp-runtime registry push --image payments:v1 --name payments-api --scope tenant
mcp-runtime registry push --scope public --image payments:v1
```

`registry push` uploads via `POST /api/runtime/registry/push`; the platform API
pushes the image from inside the cluster. Always push the **exact ref** produced
by `server build image`.

---

## access

👤 **User** for grant read/write · 🔑 **Admin** for session `apply` and `--use-kube` flows.

Manage `MCPAccessGrant` and `MCPAgentSession` CRDs that feed the gateway policy layer.
Most developers scaffold a grant with `init`, review the YAML, then `apply` through
the platform API. Session creation for agents is usually handled automatically by
`adapter stdio/proxy --auto-refresh`.

> **Full reference:** [API reference — access fields](api.md)

> ⚠️ **Tool names must match exactly.** Grant `toolRules` reference tool names
> from your server's `.mcp/servers.yaml`. If a grant rule names a tool that is
> not in the metadata the gateway returns `tool_side_effect_unknown` and denies
> the call. Run `server validate --grant-file grant.yaml` before applying to
> catch mismatches early. Use `server init --from-server http://localhost:<port>`
> to populate metadata with the real tool names from your running server.

```bash
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# Grants (👤 User)
mcp-runtime access grant init payments-ops \
  --server payments --agent-id ops-agent \
  --tool list_invoices --tool-rule refund_invoice:allow:high \
  --output grant.yaml
mcp-runtime access grant apply --file grant.yaml
mcp-runtime access grant list
mcp-runtime access grant list --namespace mcp-team-acme
mcp-runtime access grant get payments-ops
mcp-runtime access grant disable payments-ops
mcp-runtime access grant enable payments-ops
mcp-runtime access grant delete payments-ops

# Sessions (🔑 Admin — session apply via platform API requires admin role)
mcp-runtime access session init cursor-session \
  --server payments --human-id <user-id> --agent-id cursor \
  --trust medium --expires-in 1h --output session.yaml
mcp-runtime access session apply --file session.yaml   # admin only
mcp-runtime access session list
mcp-runtime access session get cursor-session
mcp-runtime access session revoke cursor-session
mcp-runtime access session unrevoke cursor-session
```

`grant list` and `session list` default to `--all-namespaces`; pass `--namespace` to
narrow scope.

**Cross-team access** — a Team A owner can grant Team B users access to Team A's servers.
Set `--team-id <globex-team-uuid>` on `grant init` to scope the grant to Team B agents,
then apply from Team A's credentials. Team B agents get sessions via `adapter --auto-refresh`
or an admin applies a session using `session apply` with the same `teamID` in `spec.subject`.

```bash
# acme-owner grants globex team's cursor agent access to payments
MCP_PLATFORM_API_PROFILE=acme-owner \
  mcp-runtime access grant init payments-globex \
    --server payments --namespace mcp-team-acme \
    --team-id <globex-team-uuid> --agent-id cursor \
    --tool list_invoices --output grant.yaml
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime access grant apply --file grant.yaml

# Admin creates the cross-team session
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init globex-session \
    --server payments --namespace mcp-team-acme \
    --team-id <globex-team-uuid> --agent-id cursor \
    --trust low --expires-in 2h --output session.yaml
MCP_PLATFORM_API_PROFILE=admin mcp-runtime access session apply --file session.yaml
```

See [Multi-team isolation](multi-team.md) for namespace layout and RBAC details.

---

## adapter

👤 **User** — for agent runtimes that need platform-governed identity headers.

Run an HTTP reverse proxy or a stdio shim that injects `x-mcp-human-id`,
`x-mcp-agent-id`, `x-mcp-session-id`, and other governance headers before
requests reach the MCP server. Adapters create agent sessions automatically
when `--auto-refresh` is set.

> **Full guide:** [Agent adapters](agent-adapters.md)

```bash
# HTTP reverse proxy
mcp-runtime adapter proxy \
  --runtime-url https://platform.mcpruntime.org/payments/mcp \
  --server payments --agent my-agent \
  --auto-refresh

# stdio shim (for Claude Desktop / local agent processes)
mcp-runtime adapter stdio \
  --runtime-url https://platform.mcpruntime.org/payments/mcp \
  --server payments --agent my-agent \
  --auto-refresh
```

Both adapters use the active platform credentials (`auth login` profile or
`MCP_PLATFORM_API_TOKEN`) to authenticate session creation with the platform API.

---

## team

🔑 **Admin** — all `team` commands require platform API admin role.

Create and manage platform teams. Each team gets a dedicated Kubernetes namespace
with quota, NetworkPolicy, service account, and ingress wiring.

> **Full guide:** [Multi-team isolation](multi-team.md)

```bash
mcp-runtime auth login --api-url https://platform.mcpruntime.org --profile admin
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Create a team
mcp-runtime team create acme --name "Acme Corp"

# Add a password-login user to the team
mcp-runtime team user create acme \
  --username acme-dev@example.com --password '...' --role member
mcp-runtime team user list acme
```

`team user create` creates or updates a password user and adds them to the team as
`member` or `owner`. Users then log in with `auth login --email ... --password ...`.

> ⚠️ `team init` is **deprecated** and rejects at runtime. Use `team create`.

---

## sentinel

⚙️ **Operator** — requires `KUBECONFIG` pointing at a cluster with admin RBAC.
Normal users should use the platform dashboard or top-level `mcp-runtime status`.

Inspect and operate the bundled analytics, gateway, and observability stack.

> **Full guide:** [Sentinel](sentinel.md)

```bash
# Health + events
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

# Logs (--follow / --tail / --since / --previous)
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs grafana --tail 500

# Restart a component
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

# Port-forward shortcuts
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward ui
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward api --port 18080
```

**Component keys** for `logs` and `restart`:
`clickhouse`, `zookeeper`, `kafka`, `ingest`, `api`, `processor`, `ui`, `gateway`,
`prometheus`, `grafana`, `otel-collector`, `tempo`, `loki`, `promtail`.

**Built-in port-forward shortcuts:** `api`, `ui`, `prometheus`, `grafana`.

---

## bootstrap

⚙️ **Operator** — run on a fresh cluster before `setup`.

Validate kubectl connectivity, CoreDNS, default `StorageClass`, Traefik
`IngressClass`, and MetalLB availability. Missing pieces are reported as warnings
so you can decide what to install.

> **Full guide:** [Cluster readiness](cluster-readiness.md)

```bash
mcp-runtime bootstrap
mcp-runtime bootstrap --provider k3s
mcp-runtime bootstrap --apply --provider k3s   # automated fix — k3s only
```

---

## setup

⚙️ **Operator** — installs the full platform stack.

Install the runtime namespace, internal registry, operator, ingress wiring, and
the bundled sentinel analytics stack. Setup is reconcile-oriented and runs
pre-flight checks before applying state.

```bash
# Minimal (all flags from env file)
mcp-runtime setup --env-file config/deployments/mcpruntime-org.env

# Common flags
mcp-runtime setup --with-tls --tls-cluster-issuer letsencrypt-prod \
  --registry-mode bundled-https --platform-mode tenant --ingress none

mcp-runtime setup --with-tls --acme-email ops@example.com  # Let's Encrypt
mcp-runtime setup --platform-mode public                   # anonymous catalog
mcp-runtime setup --without-sentinel                       # skip analytics stack
mcp-runtime setup --test-mode                              # local Kind dev path
mcp-runtime setup --registry-mode external \
  --external-registry-url registry.example.com
```

**Key env vars** (used with `--env-file`; see `config/deployments/mcpruntime-org.env.example`):

| Variable | Flag equivalent |
|---|---|
| `MCP_SETUP_WITH_TLS=1` | `--with-tls` |
| `MCP_SETUP_TLS_CLUSTER_ISSUER=letsencrypt-prod` | `--tls-cluster-issuer` |
| `MCP_SETUP_REGISTRY_MODE=bundled-https` | `--registry-mode` |
| `MCP_SETUP_PLATFORM_MODE=tenant` | `--platform-mode` |
| `MCP_SETUP_INGRESS=none` | `--ingress` |
| `MCP_SETUP_SKIP_CERT_MANAGER_INSTALL=1` | `--skip-cert-manager-install` |
| `MCP_PLATFORM_DOMAIN=mcpruntime.org` | derives all three ingress hostnames |

`--platform-mode` accepts `tenant` (default), `org`, or `public`.
`--registry-mode` accepts `auto`, `bundled-http`, `bundled-https`, or `external`.

---

## cluster

⚙️ **Operator** — Kubernetes cluster operations.

Initialize CRDs and namespaces, configure ingress, provision new clusters, and
manage cert-manager. Most subcommands require a kubeconfig with cluster-admin RBAC.

> **Full guide:** [Deployment targets](deployment-targets.md)

```bash
# Initialize / re-target
mcp-runtime cluster init
mcp-runtime cluster init --kubeconfig ~/.kube/config --context dev

# Configure ingress
mcp-runtime cluster config --ingress traefik

# Provision a new cluster (kind / eks)
mcp-runtime cluster provision --provider kind --nodes 3
mcp-runtime cluster provision --provider eks --name prod-mcp --region us-west-1

# cert-manager helpers
mcp-runtime cluster cert status
mcp-runtime cluster cert apply
mcp-runtime cluster cert wait --timeout 10m

# Post-install diagnostics (runs 37 checks)
KUBECONFIG=~/.kube/config mcp-runtime cluster doctor
```

**Active providers:** `kind`, `eks`. (`gke` / `aks` flags exist but provisioning
helpers are not yet implemented.)

---

## Platform API vs `--use-kube`

Most `server` and `access` commands use the **platform API** by default.
Set up credentials with `auth login` first.

`--use-kube` is **admin/dev/test only** — it bypasses platform auth and talks
directly to the Kubernetes API through your kubeconfig.

| Default (platform API) 👤 | Requires `--use-kube` 🔑 |
|---|---|
| `server list`, `get`, `delete`, `status`, `policy inspect`, `deploy` | `server create`, `apply`, `export`, `patch`, `logs` |
| `access grant/session` list, get, apply, delete, enable/disable, revoke | Same commands with `--use-kube` for raw CRD operations |
| `registry push` | Direct `kubectl apply` for manifests |

- `server get` without `--use-kube` returns a platform API summary; full MCPServer YAML needs `--use-kube`.
- `server logs` always requires `--use-kube`; use `sentinel logs gateway` when you only have platform credentials.
- `access session apply` via platform API is **admin-only**; agents normally get sessions automatically via `adapter --auto-refresh`.
- `--team` is a platform API resolver and is rejected with `--use-kube`.

---

## Common flows

```bash
# ─── Operator: first-time cluster setup ───────────────────────────────────────
KUBECONFIG=~/.kube/config mcp-runtime cluster provision --provider kind --nodes 3
KUBECONFIG=~/.kube/config mcp-runtime bootstrap
KUBECONFIG=~/.kube/config mcp-runtime setup --env-file config/deployments/mcpruntime-org.env

# ─── Admin: create teams and users ────────────────────────────────────────────
mcp-runtime auth login --api-url https://platform.mcpruntime.org \
  --email admin@example.com --password '...' --profile admin

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username acme-owner@example.com --password '...' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create globex --name "Globex Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create globex \
  --username globex-dev@example.com --password '...' --role member

# ─── Developer (Team A): log in, deploy a server ──────────────────────────────
mcp-runtime auth login --api-url https://platform.mcpruntime.org \
  --email acme-owner@example.com --password '...' --profile acme-owner

MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server init payments \
  --tool list_invoices --tool refund_invoice
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server build image payments --tag v1
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime registry push \
  --image <exact-image-ref-from-build> --scope tenant
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server deploy payments \
  --scope tenant --metadata-dir .mcp

MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server list
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server get payments --namespace mcp-team-acme
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime server policy inspect payments \
  --namespace mcp-team-acme

# ─── Access control: grant Team B access to Team A's server ───────────────────
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime access grant init payments-globex \
  --server payments --namespace mcp-team-acme \
  --team-id <globex-team-uuid> --agent-id cursor \
  --tool list_invoices --output grant.yaml
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime access grant apply --file grant.yaml
MCP_PLATFORM_API_PROFILE=acme-owner mcp-runtime access grant list

# ─── Admin applies a cross-team session ───────────────────────────────────────
MCP_PLATFORM_API_PROFILE=admin mcp-runtime access session init globex-session \
  --server payments --namespace mcp-team-acme \
  --team-id <globex-team-uuid> --agent-id cursor \
  --trust low --expires-in 2h --output session.yaml
MCP_PLATFORM_API_PROFILE=admin mcp-runtime access session apply --file session.yaml
MCP_PLATFORM_API_PROFILE=admin mcp-runtime access session list

# ─── Agent connects via adapter (uses auto-refresh for session) ───────────────
mcp-runtime adapter stdio \
  --runtime-url https://platform.mcpruntime.org/payments/mcp \
  --server payments --agent cursor --auto-refresh

# ─── Operator: inspect the stack ──────────────────────────────────────────────
mcp-runtime status
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 10m
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward ui

# ─── Admin: patch a running server (Kubernetes) ───────────────────────────────
KUBECONFIG=~/.kube/config mcp-runtime server patch payments \
  --namespace mcp-team-acme --patch '{"spec":{"imageTag":"v2"}}' --use-kube
KUBECONFIG=~/.kube/config mcp-runtime server logs payments \
  --namespace mcp-team-acme --tail 50 --use-kube
```

---

## Further reading

| Topic | Link |
|---|---|
| Build, push, deploy, verify a server end-to-end (includes `server validate`) | [Publish an MCP Server](publish-mcp-server.md) |
| Resource fields: MCPServer, MCPAccessGrant, MCPAgentSession | [API reference](api.md) |
| HTTP proxy and stdio adapter configuration | [Agent adapters](agent-adapters.md) |
| Multi-team namespaces, RBAC, isolation | [Multi-team isolation](multi-team.md) |
| Sentinel logs, events, restart, port-forward | [Sentinel](sentinel.md) |
| Distro-specific cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| Kind, EKS, GKE, AKS, k3s deployment | [Deployment targets](deployment-targets.md) |
