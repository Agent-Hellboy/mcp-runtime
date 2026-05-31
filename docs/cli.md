# CLI reference

`mcp-runtime` is the single binary for the entire platform — cluster setup, server
deployment, access policy, and observability.

---

## Who can run what

| Badge | Role | How to authenticate |
|---|---|---|
| 👤 **User** | Team member deploying servers | `mcp-runtime auth login` → token in `~/.mcpruntime/` |
| 🔑 **Admin** | Platform admin or operator with kube access | Platform API admin role or `--use-kube` + cluster-admin RBAC |
| ⚙️ **Operator** | Cluster operator | `KUBECONFIG` with cluster-admin RBAC, no platform login needed |

---

## Profiles (saved credentials)

```bash
# Log in and save as default profile
mcp-runtime auth login --api-url https://platform.example.com

# Log in as a named profile
mcp-runtime auth login --api-url https://platform.example.com \
  --email alice@example.com --password '...' --profile alice

# Switch the active profile
mcp-runtime auth use alice
mcp-runtime auth use admin

# Use a different profile for one command only (without switching)
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Check which profile is active
mcp-runtime auth status
```

Credentials are saved in `~/.mcpruntime/config.json`. Each login creates a named
profile. `MCP_PLATFORM_API_TOKEN` and `MCP_PLATFORM_API_URL` override the saved profile.

---

## Command map

| Command | Role | What it does | Guide |
|---|---|---|---|
| `auth` | 👤 | Save and switch platform credentials | [§ auth](#auth) |
| `status` | 👤 | Platform health at a glance | [§ status](#status) |
| `server` | 👤 / 🔑 | Scaffold, build, deploy, and manage servers | [Publish a server](publish-mcp-server.md) |
| `registry` | 👤 / ⚙️ | Push images; inspect and configure the registry | [§ registry](#registry) |
| `access` | 👤 / 🔑 | Grants and sessions for gateway policy | [API reference](api.md) |
| `adapter` | 👤 | HTTP proxy and stdio shim for agents | [Agent adapters](agent-adapters.md) |
| `team` | 🔑 | Create teams and add users | [Multi-team](multi-team.md) |
| `sentinel` | ⚙️ | Inspect and operate the analytics stack | [Sentinel](sentinel.md) |
| `bootstrap` | ⚙️ | Pre-install cluster checks | [Cluster readiness](cluster-readiness.md) |
| `setup` | ⚙️ | Install the full platform stack | [§ setup](#setup) |
| `cluster` | ⚙️ | Initialize clusters and manage cert-manager | [Deployment targets](deployment-targets.md) |

---

## auth

👤 **User**

```bash
mcp-runtime auth login --api-url https://platform.example.com
mcp-runtime auth login --api-url https://platform.example.com --token-stdin < token.txt
mcp-runtime auth login --api-url https://platform.example.com \
  --email alice@example.com --password '...' --profile alice

mcp-runtime auth use alice
mcp-runtime auth status
mcp-runtime auth logout
```

---

## status

👤 **User**

```bash
mcp-runtime status                    # overall: API, registry, operator
mcp-runtime registry status           # registry pod + endpoint
KUBECONFIG=~/.kube/config mcp-runtime sentinel status   # sentinel stack
```

---

## server

👤 **User** by default · 🔑 **Admin** with `--use-kube`

> **Full guide:** [Publish an MCP Server](publish-mcp-server.md)

### Step 1 — run your server locally and scaffold metadata

Tool names in `.mcp/servers.yaml` **must exactly match** your server's tool names.
Use `--from-server` to discover them automatically from a running local instance:

```bash
# In your project directory — run the server, then discover tools
go run .                                              # or: docker run -p 8088:8088 myimage
mcp-runtime server init myserver \
  --from-server http://localhost:8088               # appends /mcp automatically

# OR: specify tools manually if you already know the names
mcp-runtime server init myserver \
  --tool echo \
  --tool add \
  --tool-spec create_task:medium:write               # name:trust:side-effect
```

### Step 2 — validate before building

Catches tool name mismatches that cause `tool_side_effect_unknown` at the gateway:

```bash
mcp-runtime server validate --metadata-dir .mcp
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
```

### Step 3 — build, push, deploy

Run these from your project directory (where the Dockerfile lives):

```bash
# Build — uses Dockerfile in current directory
mcp-runtime server build image myserver --tag v1

# Push — use the exact image ref printed by build
mcp-runtime registry push \
  --image registry.example.com/myteam/myserver:v1 \
  --scope tenant

# Deploy
mcp-runtime server deploy myserver --scope tenant --metadata-dir .mcp
mcp-runtime server deploy myserver --scope tenant --metadata-dir .mcp --update   # re-deploy
```

`--scope` values: `tenant` (your team namespace) · `org` · `public`

### Inspect

```bash
mcp-runtime server list
mcp-runtime server get myserver --namespace mcp-team-acme    # --namespace required for team servers
mcp-runtime server status --namespace mcp-team-acme
mcp-runtime server policy inspect myserver --namespace mcp-team-acme
mcp-runtime server delete myserver
mcp-runtime server generate --metadata-dir .mcp --output manifests/   # GitOps YAML
```

### Admin / operator (🔑 --use-kube)

```bash
mcp-runtime server create myserver --image repo/myserver --tag v1 --use-kube
mcp-runtime server apply  --file server.yaml --use-kube
mcp-runtime server export myserver --use-kube
mcp-runtime server patch  myserver --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs   myserver --follow --use-kube
```

---

## registry

👤 **User** for `push` · ⚙️ **Operator** for `status`, `info`, `provision`

```bash
# Inspect (operator)
mcp-runtime registry status
mcp-runtime registry info

# Configure external registry (operator)
mcp-runtime registry provision --url registry.example.com

# Push image (user — requires auth login)
# Always use the exact image ref printed by `server build image`
mcp-runtime registry push --image registry.example.com/myteam/myserver:v1 --scope tenant
mcp-runtime registry push --image myserver:v1 --scope public
```

---

## access

👤 **User** for grants · 🔑 **Admin** for session `apply`

> **Full reference:** [API reference](api.md)

> ⚠️ **Tool names in grants must exactly match `.mcp/servers.yaml`.**
> If a `toolRule` names a tool not in the metadata, the gateway returns
> `tool_side_effect_unknown`. Run `server validate --grant-file grant.yaml`
> before applying.

### Grants (👤 User)

```bash
# Scaffold a grant YAML (--tool = allow with read side-effect, low trust)
mcp-runtime access grant init myserver-ops \
  --server myserver \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant.yaml

# For deny rules or non-default trust: --tool-rule name:allow|deny:low|medium|high
mcp-runtime access grant init myserver-ops \
  --server myserver --namespace mcp-team-acme \
  --agent-id cursor \
  --tool-rule echo:allow:low \
  --tool-rule create_task:deny:high \
  --output grant.yaml

# Validate grant before applying
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

# Apply / manage
mcp-runtime access grant apply   --file grant.yaml
mcp-runtime access grant list
mcp-runtime access grant list    --namespace mcp-team-acme
mcp-runtime access grant get     myserver-ops --namespace mcp-team-acme
mcp-runtime access grant disable myserver-ops --namespace mcp-team-acme
mcp-runtime access grant enable  myserver-ops --namespace mcp-team-acme
mcp-runtime access grant delete  myserver-ops --namespace mcp-team-acme
```

### Sessions (🔑 Admin for `apply`)

Agents normally get sessions automatically via `adapter --auto-refresh`. Use
`session init` + `session apply` only when you need an explicit manual session.

```bash
mcp-runtime access session init my-session \
  --server myserver \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --trust low \
  --expires-in 4h \
  --output session.yaml

MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml   # admin only

mcp-runtime access session list
mcp-runtime access session get      my-session --namespace mcp-team-acme
mcp-runtime access session revoke   my-session --namespace mcp-team-acme
mcp-runtime access session unrevoke my-session --namespace mcp-team-acme
```

### Cross-team access

Team A can grant Team B's agents access to Team A's servers:

```bash
# Get Team B's UUID
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list   # shows slug, not UUID
# Get UUID via API or from an existing session/grant

# Team A owner creates the cross-team grant
mcp-runtime access grant init myserver-to-teamB \
  --server myserver \
  --namespace mcp-team-a \
  --team-id <team-b-uuid> \
  --agent-id cursor \
  --tool echo \
  --output grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Admin creates the session for Team B's agent
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init teamB-session \
    --server myserver --namespace mcp-team-a \
    --team-id <team-b-uuid> --agent-id cursor \
    --trust low --expires-in 4h --output session.yaml
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml
```

See [Multi-team isolation](multi-team.md).

---

## adapter

👤 **User**

> **Full guide:** [Agent adapters](agent-adapters.md)

`--server` triggers automatic platform session creation (`--agent` is then required).
`--agent-id` sets the identity header forwarded to the MCP server.

```bash
# HTTP proxy — agents call http://127.0.0.1:8099
mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/myserver/mcp \
  --server myserver \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099

# stdio shim (for Claude Desktop / local processes)
mcp-runtime adapter stdio \
  --runtime-url https://mcp.example.com/myserver/mcp \
  --server myserver \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh
```

---

## team

🔑 **Admin**

> **Full guide:** [Multi-team isolation](multi-team.md)

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Create a team (also provisions the Kubernetes namespace)
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"

# Add a password-login user
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@example.com --password '...' --role owner
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user list acme
```

Users log in with: `mcp-runtime auth login --email alice@example.com --password '...'`

> ⚠️ `team init` is deprecated — use `team create`.

---

## sentinel

⚙️ **Operator**

> **Full guide:** [Sentinel](sentinel.md)

```bash
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs ingest --tail 200

KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward ui
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward grafana
```

Component names for `logs` / `restart`:
`clickhouse` `zookeeper` `kafka` `ingest` `processor` `api` `ui`
`gateway` `prometheus` `grafana` `otel-collector` `tempo` `loki` `promtail`

---

## bootstrap

⚙️ **Operator** — run before `setup` on a fresh cluster.

> **Full guide:** [Cluster readiness](cluster-readiness.md)

```bash
mcp-runtime bootstrap
mcp-runtime bootstrap --provider k3s
mcp-runtime bootstrap --apply --provider k3s   # auto-fix on k3s
```

---

## setup

⚙️ **Operator**

Runs pre-flight checks before installing anything. Use `--env-file` to drive all
flags from a file (see `config/deployments/mcpruntime-org.env.example`):

```bash
# Recommended: all configuration from env file
mcp-runtime setup --env-file config/deployments/mcpruntime-org.env

# Common explicit flags
mcp-runtime setup \
  --with-tls \
  --tls-cluster-issuer letsencrypt-prod \
  --registry-mode bundled-https \
  --platform-mode tenant \
  --ingress none

mcp-runtime setup --with-tls --acme-email ops@example.com   # Let's Encrypt
mcp-runtime setup --without-sentinel                         # skip analytics
mcp-runtime setup --test-mode                                # local Kind dev
```

**Env vars for `--env-file`** (see `.env.example` for the full list):

| Env var | Flag |
|---|---|
| `MCP_PLATFORM_DOMAIN=example.com` | derives all three ingress hostnames |
| `MCP_SETUP_WITH_TLS=1` | `--with-tls` |
| `MCP_SETUP_TLS_CLUSTER_ISSUER=letsencrypt-prod` | `--tls-cluster-issuer` |
| `MCP_SETUP_REGISTRY_MODE=bundled-https` | `--registry-mode` |
| `MCP_SETUP_PLATFORM_MODE=tenant` | `--platform-mode` |
| `MCP_SETUP_INGRESS=none` | `--ingress` |
| `MCP_SETUP_SKIP_CERT_MANAGER_INSTALL=1` | `--skip-cert-manager-install` |

---

## cluster

⚙️ **Operator**

> **Full guide:** [Deployment targets](deployment-targets.md)

```bash
mcp-runtime cluster init                              # install CRDs + namespaces
mcp-runtime cluster config --ingress traefik          # configure ingress
mcp-runtime cluster provision --provider kind --nodes 3
mcp-runtime cluster provision --provider eks --name prod-mcp

mcp-runtime cluster cert status
mcp-runtime cluster cert apply
mcp-runtime cluster cert wait --timeout 10m

KUBECONFIG=~/.kube/config mcp-runtime cluster doctor  # 37-point diagnostic
```

---

## End-to-end: two teams, deploy and access

```bash
# ── 1. Cluster install (operator) ────────────────────────────────────────────
KUBECONFIG=~/.kube/config mcp-runtime bootstrap
KUBECONFIG=~/.kube/config mcp-runtime setup \
  --env-file config/deployments/mcpruntime-org.env

# ── 2. Create teams and users (admin) ────────────────────────────────────────
mcp-runtime auth login --api-url https://platform.example.com \
  --email admin@example.com --password '...' --profile admin

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@example.com --password '...' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create globex --name "Globex Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create globex \
  --username bob@example.com --password '...' --role member

# ── 3. Developer (Acme) deploys a server ─────────────────────────────────────
mcp-runtime auth login --api-url https://platform.example.com \
  --email alice@example.com --password '...' --profile alice
mcp-runtime auth use alice

# Run server locally, discover real tool names
go run .                                                   # starts on :8088
mcp-runtime server init payments --from-server http://localhost:8088

# Validate metadata before building
mcp-runtime server validate --metadata-dir .mcp

# Build → push → deploy
mcp-runtime server build image payments --tag v1
mcp-runtime registry push \
  --image registry.example.com/acme/payments:v1 \
  --scope tenant
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp

# Confirm it's up
mcp-runtime server list
mcp-runtime server get payments --namespace mcp-team-acme
mcp-runtime server policy inspect payments --namespace mcp-team-acme

# ── 4. Grant access ──────────────────────────────────────────────────────────
mcp-runtime access grant init payments-bob \
  --server payments \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant.yaml

mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml
mcp-runtime access grant list

# ── 5. Create session (admin) ─────────────────────────────────────────────────
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init bob-session \
    --server payments --namespace mcp-team-acme \
    --agent-id cursor --trust low --expires-in 4h \
    --output session.yaml
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml

# ── 6. Connect via adapter ────────────────────────────────────────────────────
mcp-runtime auth use alice   # or bob logs in separately

mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/payments/mcp \
  --server payments \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099
# → MCP client connects to http://127.0.0.1:8099

# ── 7. Inspect ────────────────────────────────────────────────────────────────
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
| Build → push → deploy with `server validate` | [Publish an MCP Server](publish-mcp-server.md) |
| MCPServer, MCPAccessGrant, MCPAgentSession fields | [API reference](api.md) |
| HTTP proxy and stdio adapter | [Agent adapters](agent-adapters.md) |
| Multi-team namespaces, RBAC | [Multi-team isolation](multi-team.md) |
| Sentinel logs, events, restart | [Sentinel](sentinel.md) |
| Distro-specific cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| Kind, EKS, k3s deployment | [Deployment targets](deployment-targets.md) |
