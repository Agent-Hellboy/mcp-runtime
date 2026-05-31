# CLI reference

This guide walks through every `mcp-runtime` command using the **real example servers
in the repository** so you can follow along, test the platform, and build a
presentation from a single working script.

**Example servers used throughout this doc:**

| Server | Language | Run command | Tools |
|---|---|---|---|
| `workspace-assistant-mcp` | Go | `go run .` | `echo`, `add`, `upper`, `lower`, `create_task`, `draft_release_note`, `slugify` |
| `data-utility-mcp` | Python | `python app.py` | `echo`, `add`, `multiply`, `upper`, `lower`, `ping`, `reverse` |
| `text-analysis-mcp` | Rust | `cargo run` | `repeat`, `word_count`, `extract_keywords` |

All three listen on `http://localhost:8088/mcp` by default.
`--from-server http://localhost:8088` appends `/mcp` automatically.

---

## Who can run what

| Badge | Role | How to authenticate |
|---|---|---|
| 👤 **User** | Team member deploying servers | `mcp-runtime auth login` → token in `~/.mcpruntime/` |
| 🔑 **Admin** | Platform admin or kube operator | Platform API admin role or `--use-kube` + cluster-admin RBAC |
| ⚙️ **Operator** | Cluster operator | `KUBECONFIG` with cluster-admin RBAC, no platform login needed |

---

## Profiles (saved credentials)

Credentials are saved in `~/.mcpruntime/config.json`. Each `auth login` creates
a named profile; switch with `auth use` or override per-command with
`MCP_PLATFORM_API_PROFILE`.

```bash
# Log in and save as the default profile
mcp-runtime auth login --api-url https://platform.example.com

# Log in as a named profile
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password '...' \
  --profile alice

# Switch the active profile (affects all following commands)
mcp-runtime auth use alice

# Use a different profile for one command only — no switch
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Inspect the active profile
mcp-runtime auth status

# Remove saved credentials
mcp-runtime auth logout
```

---

## Command map

| Command | Role | What it does | Guide |
|---|---|---|---|
| `auth` | 👤 | Save and switch platform credentials | [§ auth](#auth) |
| `status` | 👤 | Platform health at a glance | [§ status](#status) |
| `server` | 👤 / 🔑 | Scaffold, validate, build, push, deploy, manage | [Publish a server](publish-mcp-server.md) |
| `registry` | 👤 / ⚙️ | Push images; inspect the registry | [§ registry](#registry) |
| `access` | 👤 / 🔑 | Grants and sessions for gateway policy | [API reference](api.md) |
| `adapter` | 👤 | HTTP proxy and stdio shim for agents | [Agent adapters](agent-adapters.md) |
| `team` | 🔑 | Create teams and add password users | [Multi-team](multi-team.md) |
| `sentinel` | ⚙️ | Inspect and operate the analytics stack | [Sentinel](sentinel.md) |
| `bootstrap` | ⚙️ | Pre-install cluster checks | [Cluster readiness](cluster-readiness.md) |
| `setup` | ⚙️ | Install the full platform stack | [§ setup](#setup) |
| `cluster` | ⚙️ | Initialize clusters, manage cert-manager | [Deployment targets](deployment-targets.md) |

---

## auth

👤 **User**

```bash
# Interactive — prompts for a token
mcp-runtime auth login --api-url https://platform.example.com

# Non-interactive (CI / scripted)
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --token-stdin < token.txt

# Email + password login
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password '...' \
  --profile alice

# Switch / inspect / remove
mcp-runtime auth use alice
mcp-runtime auth status
mcp-runtime auth logout
```

---

## status

👤 **User** (kubeconfig optional for full sentinel detail)

```bash
mcp-runtime status                                                   # registry, operator, platform API
mcp-runtime registry status                                          # registry pod + endpoint
KUBECONFIG=~/.kube/config mcp-runtime sentinel status               # sentinel stack
```

---

## server

👤 **User** by default · 🔑 **Admin** with `--use-kube`

> **Full guide:** [Publish an MCP Server](publish-mcp-server.md)

The developer flow is five steps: **init → validate → build → push → deploy**.

---

### Step 1 — scaffold metadata with `server init`

`server init` creates `.mcp/servers.yaml` which describes your server's tools,
trust levels, side effects, and policy. **Tool names must exactly match** what
your server implements — use `--from-server` to discover them automatically:

```bash
# workspace-assistant-mcp (Go)
cd examples/workspace-assistant-mcp
go run . &                                  # starts on http://localhost:8088/mcp
SERVER_PID=$!

mcp-runtime server init workspace-demo \
  --from-server http://localhost:8088       # /mcp appended automatically
# Discovered: aaa-ping, add, create_task, draft_release_note,
#             echo, lower, slugify, upper

kill $SERVER_PID
```

```bash
# data-utility-mcp (Python)
cd examples/data-utility-mcp
pip install "mcp[cli]"
python app.py &                             # starts on http://localhost:8088/mcp
SERVER_PID=$!

mcp-runtime server init data-util \
  --from-server http://localhost:8088
# Discovered: add, echo, lower, multiply, ping, reverse, upper

kill $SERVER_PID
```

```bash
# text-analysis-mcp (Rust)
cd examples/text-analysis-mcp
cargo run &                                 # starts on http://localhost:8088/mcp
SERVER_PID=$!

mcp-runtime server init text-analysis \
  --from-server http://localhost:8088
# Discovered: extract_keywords, repeat, word_count

kill $SERVER_PID
```

**Manual alternative** — when you already know the tool names:

```bash
# --tool name                         → allow, read side-effect, low trust
# --tool-spec name:trust:side-effect  → full control (trust: low|medium|high,
#                                       side-effect: read|write|destructive)
mcp-runtime server init workspace-demo \
  --tool echo \
  --tool add \
  --tool upper \
  --tool-spec create_task:medium:write \
  --tool-spec draft_release_note:medium:write
```

---

### Step 2 — validate before building

Catches tool name mismatches that would cause `tool_side_effect_unknown` errors
at the gateway before you spend time on a build:

```bash
mcp-runtime server validate --metadata-dir .mcp

# Also validate a grant you scaffolded
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

# Cross-check against the locally running server
mcp-runtime server validate --metadata-dir .mcp --from-server http://localhost:8088
```

---

### Step 3 — build the image

Run from the same directory where the Dockerfile lives:

```bash
# workspace-assistant-mcp
cd examples/workspace-assistant-mcp
mcp-runtime server build image workspace-demo --tag v1

# data-utility-mcp
cd examples/data-utility-mcp
mcp-runtime server build image data-util --tag v1

# text-analysis-mcp
cd examples/text-analysis-mcp
mcp-runtime server build image text-analysis --tag v1
```

The command prints the **exact image ref** you must use in the push step, e.g.:
```
registry.example.com/acme/workspace-demo:v1
```

Use `--platform linux/amd64` when building on Apple Silicon targeting k3s / EKS nodes.

---

### Step 4 — push the image

Use the **exact ref** printed by `server build image`:

```bash
mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant

# Other scopes
mcp-runtime registry push --image ... --scope org     # org-wide catalog
mcp-runtime registry push --image ... --scope public  # anonymous catalog
```

---

### Step 5 — deploy

```bash
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp

# Re-deploy after a code / image change
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp \
  --update
```

---

### Full example — workspace-assistant-mcp end-to-end

```bash
cd examples/workspace-assistant-mcp

# 1. Discover tools from the running server
go run . &
SERVER_PID=$!
mcp-runtime server init workspace-demo --from-server http://localhost:8088
kill $SERVER_PID

# 2. Validate
mcp-runtime server validate --metadata-dir .mcp

# 3. Build (as alice, an Acme team owner)
mcp-runtime auth use alice
mcp-runtime server build image workspace-demo --tag v1
# → prints: registry.example.com/acme/workspace-demo:v1

# 4. Push — use the exact ref printed above
mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant

# 5. Deploy
mcp-runtime server deploy workspace-demo --scope tenant --metadata-dir .mcp

# 6. Confirm
mcp-runtime server list
mcp-runtime server get workspace-demo --namespace mcp-team-acme
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme
```

---

### Inspect and manage

```bash
mcp-runtime server list
mcp-runtime server get workspace-demo --namespace mcp-team-acme
mcp-runtime server status --namespace mcp-team-acme
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme
mcp-runtime server delete workspace-demo
mcp-runtime server generate --metadata-dir .mcp --output manifests/   # GitOps YAML
```

### Admin / operator (🔑 --use-kube)

```bash
mcp-runtime server create workspace-demo --image repo/workspace-demo --tag v1 \
  --namespace mcp-team-acme --use-kube
mcp-runtime server apply  --file server.yaml --use-kube
mcp-runtime server export workspace-demo --namespace mcp-team-acme --use-kube
mcp-runtime server patch  workspace-demo --namespace mcp-team-acme \
  --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs   workspace-demo --namespace mcp-team-acme \
  --follow --use-kube
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

# Push (user — requires auth login)
# Use the exact image ref printed by `server build image`
mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant
```

---

## access

👤 **User** for grants · 🔑 **Admin** for session `apply`

> **Full reference:** [API reference](api.md)

> ⚠️ **Tool names in grants must exactly match `.mcp/servers.yaml`.**
> Run `server validate --grant-file grant.yaml` before applying to catch
> mismatches that cause `tool_side_effect_unknown` at the gateway.

### Grants (👤 User)

```bash
# Simple: allow tools with read side-effect and low trust
# --tool name  →  allow, read, low trust
mcp-runtime access grant init workspace-ops \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --tool upper \
  --output grant.yaml

# Advanced: mixed allow/deny with custom trust levels
# --tool-rule name:allow|deny:low|medium|high
mcp-runtime access grant init workspace-ops \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool-rule echo:allow:low \
  --tool-rule add:allow:low \
  --tool-rule create_task:deny:medium \
  --output grant.yaml

# Always validate before applying
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Manage
mcp-runtime access grant list
mcp-runtime access grant list    --namespace mcp-team-acme
mcp-runtime access grant get     workspace-ops --namespace mcp-team-acme
mcp-runtime access grant disable workspace-ops --namespace mcp-team-acme
mcp-runtime access grant enable  workspace-ops --namespace mcp-team-acme
mcp-runtime access grant delete  workspace-ops --namespace mcp-team-acme
```

### Sessions (🔑 Admin for `apply`)

Agents normally get sessions automatically via `adapter --auto-refresh`.
Use `session init` + `session apply` only for explicit manual sessions.

```bash
mcp-runtime access session init cursor-session \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --trust low \
  --expires-in 4h \
  --output session.yaml

MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml

mcp-runtime access session list
mcp-runtime access session get      cursor-session --namespace mcp-team-acme
mcp-runtime access session revoke   cursor-session --namespace mcp-team-acme
mcp-runtime access session unrevoke cursor-session --namespace mcp-team-acme
```

### Cross-team access

Team A can grant Team B's agents access to Team A's servers:

```bash
# Team A owner creates a cross-team grant
mcp-runtime access grant init workspace-to-globex \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --team-id <globex-team-uuid> \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant-cross.yaml
mcp-runtime access grant apply --file grant-cross.yaml

# Admin creates the session for Team B's agent
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init globex-session \
    --server workspace-demo \
    --namespace mcp-team-acme \
    --team-id <globex-team-uuid> \
    --agent-id cursor \
    --trust low \
    --expires-in 4h \
    --output session-cross.yaml
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session-cross.yaml
```

See [Multi-team isolation](multi-team.md).

---

## adapter

👤 **User**

> **Full guide:** [Agent adapters](agent-adapters.md)

The adapter injects platform session and governance headers before every request
reaches the MCP server. When `--server` is set, the adapter creates the session
automatically — `--agent` (session name) is **required** in that case.
`--agent-id` sets the identity header forwarded to the server.

```bash
# HTTP proxy — MCP clients connect to http://127.0.0.1:8099
mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099

# stdio shim — for Claude Desktop or local agent processes
mcp-runtime adapter stdio \
  --runtime-url https://mcp.example.com/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh
```

Once the adapter is running, point any MCP client (Claude Desktop, Cursor, or your
agent SDK) at `http://127.0.0.1:8099` — session creation and governance headers are
handled transparently.

---

## team

🔑 **Admin** — all `team` commands require the platform API admin role.

> **Full guide:** [Multi-team isolation](multi-team.md)

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Create a team — also provisions the Kubernetes namespace with RBAC
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"

# Add a password-login user to the team
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@acme.com --password '...' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user list acme
```

Team users log in with:
```bash
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password '...' \
  --profile alice
```

> ⚠️ `team init` is deprecated — use `team create`.

---

## sentinel

⚙️ **Operator** — requires `KUBECONFIG` with cluster-admin RBAC.

> **Full guide:** [Sentinel](sentinel.md)

```bash
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

# Logs — --follow, --tail, --since, --previous
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs ingest --tail 200

# Restart a component
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

# Open a component locally
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
mcp-runtime bootstrap --apply --provider k3s    # automated fix on k3s
```

---

## setup

⚙️ **Operator** — runs pre-flight checks automatically before installing anything.

```bash
# Recommended: all flags from an env file
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

**Key env vars** (`--env-file` equivalents — see `config/deployments/mcpruntime-org.env.example`):

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
mcp-runtime cluster init
mcp-runtime cluster config --ingress traefik
mcp-runtime cluster provision --provider kind --nodes 3
mcp-runtime cluster provision --provider eks --name prod-mcp

mcp-runtime cluster cert status
mcp-runtime cluster cert apply
mcp-runtime cluster cert wait --timeout 10m

KUBECONFIG=~/.kube/config mcp-runtime cluster doctor    # 37-point diagnostic
```

---

## Presentation walkthrough

Follow this script top to bottom to demo the full platform using
`workspace-assistant-mcp`. Requires a running platform at `https://platform.example.com`
and admin credentials.

```bash
# ── 1. Create two teams (admin) ───────────────────────────────────────────────
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email admin@example.com --password '...' \
  --profile admin

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@acme.com --password 'alice123' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create globex --name "Globex Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create globex \
  --username bob@globex.com --password 'bob456' --role member

# ── 2. Log in as alice (Acme team owner) ─────────────────────────────────────
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password 'alice123' \
  --profile alice
mcp-runtime auth use alice

# ── 3. Init metadata from the running example server ─────────────────────────
cd examples/workspace-assistant-mcp
go run . &
SERVER_PID=$!

mcp-runtime server init workspace-demo --from-server http://localhost:8088
# Discovered: aaa-ping, add, create_task, draft_release_note,
#             echo, lower, slugify, upper

kill $SERVER_PID

# ── 4. Validate ───────────────────────────────────────────────────────────────
mcp-runtime server validate --metadata-dir .mcp

# ── 5. Build → push → deploy ──────────────────────────────────────────────────
mcp-runtime server build image workspace-demo --tag v1
# Prints the exact image ref, e.g.: registry.example.com/acme/workspace-demo:v1

mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant

mcp-runtime server deploy workspace-demo --scope tenant --metadata-dir .mcp

# ── 6. Confirm the server is up ───────────────────────────────────────────────
mcp-runtime server list
mcp-runtime server get workspace-demo --namespace mcp-team-acme
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme

# ── 7. Grant access ───────────────────────────────────────────────────────────
mcp-runtime access grant init workspace-cursor \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --tool upper \
  --output grant.yaml

mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml
mcp-runtime access grant list

# ── 8. Create a session (admin) ───────────────────────────────────────────────
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init demo-session \
    --server workspace-demo \
    --namespace mcp-team-acme \
    --agent-id cursor \
    --trust low \
    --expires-in 4h \
    --output session.yaml
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml
mcp-runtime access session list

# ── 9. Connect via adapter and call tools with your MCP client ────────────────
mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099 &

# → Connect Claude Desktop, Cursor, or your MCP client to http://127.0.0.1:8099
# → Call echo, add, upper — governance headers injected automatically

# ── 10. Inspect analytics ─────────────────────────────────────────────────────
mcp-runtime status
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 10m
# → Open https://platform.example.com → Analytics → Tools tab
#   to see tool calls by user, team, and agent
```

---

## Further reading

| Topic | Link |
|---|---|
| Build → push → deploy with `server validate` | [Publish an MCP Server](publish-mcp-server.md) |
| MCPServer, MCPAccessGrant, MCPAgentSession fields | [API reference](api.md) |
| HTTP proxy and stdio adapter | [Agent adapters](agent-adapters.md) |
| Multi-team namespaces and RBAC | [Multi-team isolation](multi-team.md) |
| Sentinel logs, events, restart | [Sentinel](sentinel.md) |
| Distro-specific cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| Kind, EKS, k3s deployment | [Deployment targets](deployment-targets.md) |
