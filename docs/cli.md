# CLI reference

This guide walks through every `mcp-runtime` command using the **real example servers
in the repository** so you can follow along, test the platform, and build a
presentation from a single working script.

**Example servers used in this doc:**

| Server | Language | Dockerfile | Key tools |
|---|---|---|---|
| `workspace-assistant-mcp` | Go | `examples/workspace-assistant-mcp/Dockerfile` | `echo`, `add`, `upper`, `lower`, `create_task` |
| `text-analysis-mcp` | Rust | `examples/text-analysis-mcp/Dockerfile` | `repeat`, `word_count`, `extract_keywords` |
| `data-utility-mcp` | Python | `examples/data-utility-mcp/Dockerfile` | `echo`, `add`, `multiply`, `upper`, `lower`, `ping` |

All three listen on port `8088` by default and serve the MCP endpoint at `/mcp`.

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
a named profile; switch profiles with `auth use` or `MCP_PLATFORM_API_PROFILE`
per-command.

```bash
# Log in and save as the default profile
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# Log in as a named profile
mcp-runtime auth login --api-url https://platform.mcpruntime.org \
  --email alice@acme.com --password '...' --profile alice

# Switch the active profile (affects all following commands)
mcp-runtime auth use alice

# Use a different profile for one command only
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
mcp-runtime auth login --api-url https://platform.mcpruntime.org

# Non-interactive (CI/scripted)
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --token-stdin < token.txt

# Email + password (when the platform supports password login)
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email alice@acme.com --password 'secret' \
  --profile alice

# Switch / inspect / remove
mcp-runtime auth use alice
mcp-runtime auth use admin
mcp-runtime auth status
mcp-runtime auth logout
```

---

## status

👤 **User** (kubeconfig optional for full detail)

```bash
mcp-runtime status                          # registry, operator, platform API
mcp-runtime registry status                 # registry pod + endpoint
KUBECONFIG=~/.kube/config mcp-runtime sentinel status   # sentinel stack
```

---

## server

👤 **User** by default · 🔑 **Admin** with `--use-kube`

> **Full guide:** [Publish an MCP Server](publish-mcp-server.md)

The typical developer flow is four steps: **init → validate → build → push → deploy**.

---

### Step 1 — scaffold metadata with `server init`

`server init` creates `.mcp/servers.yaml` which holds tool names, trust levels,
side effects, and policy settings. **Tool names must exactly match** what your
server implements.

**Recommended: discover tool names automatically** by running the server locally
first, then using `--from-server`:

```bash
# Example: workspace-assistant-mcp (Go)
cd examples/workspace-assistant-mcp
go run .                           # starts on http://localhost:8088/mcp

# In another terminal — init discovers all 8 tools automatically
mcp-runtime server init workspace-demo \
  --from-server http://localhost:8088
# → Discovered: aaa-ping, add, create_task, draft_release_note,
#               echo, lower, slugify, upper
```

```bash
# Example: data-utility-mcp (Python)
cd examples/data-utility-mcp
pip install -r requirements.txt
python app.py                      # starts on http://localhost:8088/mcp

mcp-runtime server init data-util \
  --from-server http://localhost:8088
# → Discovered: add, echo, lower, multiply, ping, reverse, upper
```

```bash
# Example: text-analysis-mcp (Rust)
cd examples/text-analysis-mcp
cargo run                          # starts on http://localhost:8088/mcp

mcp-runtime server init text-analysis \
  --from-server http://localhost:8088
# → Discovered: extract_keywords, repeat, word_count
```

**Manual alternative** (when you already know tool names):

```bash
# --tool name                    → allow rule, read side-effect, low trust
# --tool-spec name:trust:effect  → full control over trust and side-effect

mcp-runtime server init workspace-demo \
  --tool echo \
  --tool add \
  --tool upper \
  --tool-spec create_task:medium:write \
  --tool-spec draft_release_note:medium:write
```

`--tool-spec` format: `name:low|medium|high:read|write|destructive`

---

### Step 2 — validate before building

Catches mismatches between metadata and grants that would cause
`tool_side_effect_unknown` errors at runtime:

```bash
mcp-runtime server validate --metadata-dir .mcp

# Also validate a grant YAML you scaffolded
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

# Cross-check against the locally running server
mcp-runtime server validate \
  --metadata-dir .mcp \
  --from-server http://localhost:8088
```

---

### Step 3 — build the image

Run from the project directory (where the Dockerfile is):

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

The command prints the exact image ref to push, e.g.:
```
registry.mcpruntime.org/acme/workspace-demo:v1
```

Use `--platform linux/amd64` when building on Apple Silicon for k3s/EKS nodes.

---

### Step 4 — push the image

```bash
# Use the exact image ref printed by server build image
mcp-runtime registry push \
  --image registry.mcpruntime.org/acme/workspace-demo:v1 \
  --scope tenant

# Other scope values
mcp-runtime registry push --image ... --scope org     # org-wide catalog
mcp-runtime registry push --image ... --scope public  # anonymous catalog
```

---

### Step 5 — deploy

```bash
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp

# Re-deploy after a code/image change
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp \
  --update
```

---

### Full push example — workspace-assistant-mcp

```bash
cd examples/workspace-assistant-mcp

# 1. Run locally and init
go run . &
mcp-runtime server init workspace-demo --from-server http://localhost:8088

# 2. Validate
mcp-runtime server validate --metadata-dir .mcp

# 3. Build (run as acme team member)
mcp-runtime auth use alice
mcp-runtime server build image workspace-demo --tag v1

# 4. Push
mcp-runtime registry push \
  --image registry.mcpruntime.org/acme/workspace-demo:v1 \
  --scope tenant

# 5. Deploy
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp

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
mcp-runtime server generate --metadata-dir .mcp --output manifests/
```

### Admin / operator commands (🔑 --use-kube)

```bash
mcp-runtime server create workspace-demo --image repo/workspace-demo --tag v1 --use-kube
mcp-runtime server apply  --file server.yaml --use-kube
mcp-runtime server export workspace-demo --use-kube
mcp-runtime server patch  workspace-demo \
  --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs   workspace-demo --follow --use-kube
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
# Always use the exact image ref from `server build image`
mcp-runtime registry push \
  --image registry.mcpruntime.org/acme/workspace-demo:v1 \
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
# Scaffold a grant — allow specific tools
# --tool name        → allow, read side-effect, low trust (simple case)
# --tool-rule name:allow|deny:low|medium|high  → full control
mcp-runtime access grant init workspace-ops \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --tool upper \
  --output grant.yaml

# With mixed rules (allow echo, deny create_task)
mcp-runtime access grant init workspace-ops \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool-rule echo:allow:low \
  --tool-rule add:allow:low \
  --tool-rule create_task:deny:medium \
  --output grant.yaml

# Validate then apply
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Inspect / manage
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

```bash
# Team A grants Team B's cursor agent access to their server
mcp-runtime access grant init workspace-to-globex \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --team-id <globex-team-uuid> \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant-cross.yaml
mcp-runtime access grant apply --file grant-cross.yaml

# Admin creates the cross-team session
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

The adapter injects governance headers before requests reach the MCP server.
When `--server` is set, a platform session is created automatically —
`--agent` is **required** in that case (session name).
`--agent-id` sets the `X-MCP-Agent-ID` identity header forwarded to the server.

```bash
# HTTP proxy — MCP clients call http://127.0.0.1:8099
mcp-runtime adapter proxy \
  --runtime-url https://mcp.mcpruntime.org/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099

# stdio shim (Claude Desktop / local agent processes)
mcp-runtime adapter stdio \
  --runtime-url https://mcp.mcpruntime.org/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh
```

Test the adapter with curl (Streamable HTTP — MCP 2025-06-18):

```bash
# Initialize MCP session
INIT=$(curl -si http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
        "protocolVersion":"2025-06-18",
        "capabilities":{},
        "clientInfo":{"name":"test","version":"1"}}}')

SID=$(echo "$INIT" | grep -i mcp-session-id | awk '{print $2}' | tr -d '\r')

# Mark session ready
curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' > /dev/null

# List tools
curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

# Call a tool
curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
        "name":"echo","arguments":{"message":"hello from mcp-runtime!"}}}'
```

---

## team

🔑 **Admin**

> **Full guide:** [Multi-team isolation](multi-team.md)

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Create a team — also provisions the Kubernetes namespace
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"

# Add a password-login user
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@acme.com --password 'secret' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user list acme
```

Users log in with:
```bash
mcp-runtime auth login --api-url https://platform.mcpruntime.org \
  --email alice@acme.com --password 'secret' --profile alice
```

> ⚠️ `team init` is deprecated — use `team create`.

---

## sentinel

⚙️ **Operator**

> **Full guide:** [Sentinel](sentinel.md)

```bash
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

# Logs (--follow / --tail / --since / --previous)
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs ingest --tail 200

# Restart
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

# Port-forward to open a component locally
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
mcp-runtime bootstrap --apply --provider k3s   # automated fix on k3s
```

---

## setup

⚙️ **Operator**

Runs pre-flight checks automatically before installing anything.

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

**Key env vars** (`--env-file` equivalents):

| Env var | Flag |
|---|---|
| `MCP_PLATFORM_DOMAIN=mcpruntime.org` | derives all three ingress hostnames |
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

KUBECONFIG=~/.kube/config mcp-runtime cluster doctor   # 37-point diagnostic
```

---

## Complete presentation walkthrough

Run this end-to-end to demo the full platform using `workspace-assistant-mcp`:

```bash
# ── 0. Prerequisites ──────────────────────────────────────────────────────────
# Platform is already set up at https://platform.mcpruntime.org
# You have admin credentials

# ── 1. Create two teams (admin) ───────────────────────────────────────────────
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email admin@mcpruntime.org --password '...' \
  --profile admin

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create acme \
  --username alice@acme.com --password 'alice123' --role owner

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create globex --name "Globex Corp"
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team user create globex \
  --username bob@globex.com --password 'bob456' --role member

# ── 2. Deploy workspace-assistant-mcp as Acme (alice) ────────────────────────
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email alice@acme.com --password 'alice123' \
  --profile alice
mcp-runtime auth use alice

cd examples/workspace-assistant-mcp

# Run locally to discover real tool names
go run . &
mcp-runtime server init workspace \
  --from-server http://localhost:8088
kill %1   # stop local server

# Validate metadata
mcp-runtime server validate --metadata-dir .mcp

# Build → push → deploy
mcp-runtime server build image workspace --tag v1
mcp-runtime registry push \
  --image registry.mcpruntime.org/acme/workspace:v1 \
  --scope tenant
mcp-runtime server deploy workspace --scope tenant --metadata-dir .mcp

# Confirm
mcp-runtime server list
mcp-runtime server get workspace --namespace mcp-team-acme

# ── 3. Grant access and set up a session ─────────────────────────────────────
# Alice grants cursor agent access to echo + add tools
mcp-runtime access grant init workspace-cursor \
  --server workspace \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --tool upper \
  --output grant.yaml

mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml

# Admin creates a session
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session init demo-session \
    --server workspace \
    --namespace mcp-team-acme \
    --agent-id cursor \
    --trust low \
    --expires-in 4h \
    --output session.yaml
MCP_PLATFORM_API_PROFILE=admin \
  mcp-runtime access session apply --file session.yaml

# ── 4. Connect via adapter and call tools ─────────────────────────────────────
mcp-runtime adapter proxy \
  --runtime-url https://mcp.mcpruntime.org/workspace/mcp \
  --server workspace \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099 &

# Initialize MCP session
INIT=$(curl -si http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
        "protocolVersion":"2025-06-18","capabilities":{},
        "clientInfo":{"name":"demo","version":"1"}}}')
SID=$(echo "$INIT" | grep -i mcp-session-id | awk '{print $2}' | tr -d '\r')

curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' > /dev/null

# Call tools
curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{
        "name":"echo","arguments":{"message":"hello from the platform!"}}}'

curl -s http://localhost:8099 \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
        "name":"add","arguments":{"a":42,"b":58}}}'

# ── 5. Inspect the platform ───────────────────────────────────────────────────
mcp-runtime status
mcp-runtime server policy inspect workspace --namespace mcp-team-acme
mcp-runtime access grant list
mcp-runtime access session list
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
# → Analytics at https://platform.mcpruntime.org (Analytics → Tools tab)
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
