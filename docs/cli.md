# CLI reference

This guide walks through every `mcp-runtime` command using the real example servers
in the repository so you can follow along, test the platform, and build a demo from
a single working script.

## Quick reference

| Goal | Commands |
|---|---|
| Log in | `auth login` → `auth use <profile>` |
| Deploy a server | `server init` → `server validate` → `server build image` → `registry push` → `server deploy` |
| Grant an agent access | `access grant init` → `server validate --grant-file` → `access grant apply` |
| Create a session manually | `access session init` → `access session apply` |
| Connect an MCP client | `adapter proxy --server ... --agent ... --auto-refresh` |
| Check platform health | `status` |
| Inspect a running server | `server list` · `server get` · `server policy inspect` |
| View analytics logs | `sentinel status` · `sentinel logs api` |
| Diagnose cluster issues | `cluster doctor` |

**Example servers in this repo:**

| Server | Language | Run command | Tools |
|---|---|---|---|
| `workspace-assistant-mcp` | Go | `go run .` | `echo`, `add`, `upper`, `lower`, `create_task`, `draft_release_note`, `slugify` |
| `data-utility-mcp` | Python | `python app.py` | `echo`, `add`, `multiply`, `upper`, `lower`, `ping`, `reverse` |
| `text-analysis-mcp` | Rust | `cargo run` | `repeat`, `word_count`, `extract_keywords` |

All three listen on `http://localhost:8088/mcp` by default.
`--from-server http://localhost:8088` appends `/mcp` automatically.

---

## Access model

Three roles control what each command can do:

| Role | Who | How to authenticate |
|---|---|---|
| User | Team member deploying servers | `mcp-runtime auth login` |
| Admin | Platform admin or kube operator | Platform API admin role, or `--use-kube` with cluster-admin RBAC |
| Operator | Cluster operator | `KUBECONFIG` with cluster-admin RBAC — no platform login needed |

Commands are labelled **[User]**, **[Admin]**, or **[Operator]** throughout this page.

---

## Profiles

Credentials are saved in `~/.mcpruntime/config.json`. Each `auth login` creates a
named profile. Switch with `auth use` or override for a single command with
`MCP_PLATFORM_API_PROFILE`.

```bash
# Save as the default profile
mcp-runtime auth login --api-url https://platform.example.com

# Save under a named profile
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password '...' \
  --profile alice

# Switch the active profile
mcp-runtime auth use alice

# Use a different profile for one command without switching
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

# Check which profile is active
mcp-runtime auth status

# Remove saved credentials
mcp-runtime auth logout
```

---

## Command map

| Command | Role | Status | What it does | Guide |
|---|---|---|---|---|
| `auth` | User | Stable | Save and switch platform credentials | [auth](#auth) |
| `status` | User | Stable | Platform health at a glance | [status](#status) |
| `server` | User / Admin | Stable | Scaffold, validate, build, push, deploy, manage | [Publish a server](publish-mcp-server.md) |
| `registry` | User / Operator | Stable | Push images; inspect the registry | [registry](#registry) |
| `access` | User / Admin | Stable | Grants and sessions for gateway policy | [API reference](api.md) |
| `adapter` | User | Stable | HTTP proxy and stdio shim for agents | [Agent adapters](agent-adapters.md) |
| `team` | Admin | Stable | Create teams and add password users | [Multi-team](multi-team.md) |
| `sentinel` | Operator | Stable | Inspect and operate the analytics stack | [Sentinel](sentinel.md) |
| `bootstrap` | Operator | Stable | Pre-install cluster checks | [Cluster readiness](cluster-readiness.md) |
| `setup` | Operator | Stable | Install the full platform stack | [setup](#setup) |
| `cluster` | Operator | Stable | Initialize clusters, manage cert-manager | [Deployment targets](deployment-targets.md) |
| `server validate` | User | Alpha | Validate metadata and grant/session YAML | [server](#server-validate) |
| `server init --from-server` | User | Alpha | Auto-discover tools from a running server | [server init](#server-init) |

**Status labels:** `Stable` — works end-to-end, tested in production use. `Alpha` — functional but API or UX may change.

---

## auth

**[User]**

```bash
# Interactive
mcp-runtime auth login --api-url https://platform.example.com

# Non-interactive (CI)
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --token-stdin < token.txt

# Email + password
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password '...' \
  --profile alice

mcp-runtime auth use alice
mcp-runtime auth status
mcp-runtime auth logout
```

---

## status

**[User]** — kubeconfig optional for sentinel detail

```bash
mcp-runtime status                                         # registry, operator, platform API
mcp-runtime registry status                               # registry pod + endpoint
KUBECONFIG=~/.kube/config mcp-runtime sentinel status     # sentinel stack
```

---

## server

**[User]** by default — **[Admin]** with `--use-kube`

> Full guide: [Publish an MCP Server](publish-mcp-server.md)

The developer flow is five steps: **init → validate → build → push → deploy**.

### server init

`server init` creates `.mcp/servers.yaml` with tool names, trust levels, side effects,
and policy. Tool names must exactly match what your server implements.

Use `--from-server` to discover them automatically from a running local instance:

```bash
# workspace-assistant-mcp (Go)
cd examples/workspace-assistant-mcp
go run . &
SERVER_PID=$!
mcp-runtime server init workspace-demo --from-server http://localhost:8088
kill $SERVER_PID
# Discovered: aaa-ping, add, create_task, draft_release_note, echo, lower, slugify, upper
```

```bash
# data-utility-mcp (Python)
cd examples/data-utility-mcp
pip install "mcp[cli]"
python app.py &
SERVER_PID=$!
mcp-runtime server init data-util --from-server http://localhost:8088
kill $SERVER_PID
# Discovered: add, echo, lower, multiply, ping, reverse, upper
```

```bash
# text-analysis-mcp (Rust)
cd examples/text-analysis-mcp
cargo run &
SERVER_PID=$!
mcp-runtime server init text-analysis --from-server http://localhost:8088
kill $SERVER_PID
# Discovered: extract_keywords, repeat, word_count
```

Manual alternative when you already know the tool names:

```bash
# --tool name                         adds an allow rule with read side-effect and low trust
# --tool-spec name:trust:side-effect  full control  (trust: low|medium|high,
#                                                    side-effect: read|write|destructive)
mcp-runtime server init workspace-demo \
  --tool echo \
  --tool add \
  --tool upper \
  --tool-spec create_task:medium:write \
  --tool-spec draft_release_note:medium:write
```

### server validate

Catches tool name mismatches before a build. A mismatch causes `tool_side_effect_unknown`
errors at the gateway at runtime.

```bash
mcp-runtime server validate --metadata-dir .mcp

# Validate a grant alongside the metadata
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

# Cross-check against the locally running server
mcp-runtime server validate --metadata-dir .mcp --from-server http://localhost:8088
```

### server build image

Run from the directory where the Dockerfile lives:

```bash
cd examples/workspace-assistant-mcp
mcp-runtime server build image workspace-demo --tag v1
```

The command prints the exact image ref to use in the push step:

```
registry.example.com/acme/workspace-demo:v1
```

Use `--platform linux/amd64` when building on Apple Silicon for k3s or EKS nodes.

### registry push

Use the exact ref printed by `server build image`:

```bash
mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant

# Other scopes
mcp-runtime registry push --image ... --scope org      # org-wide catalog
mcp-runtime registry push --image ... --scope public   # anonymous catalog
```

### server deploy

```bash
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp

# Re-deploy after a code or image change
mcp-runtime server deploy workspace-demo \
  --scope tenant \
  --metadata-dir .mcp \
  --update
```

### Full example — workspace-assistant-mcp

```bash
cd examples/workspace-assistant-mcp

go run . &
SERVER_PID=$!
mcp-runtime server init workspace-demo --from-server http://localhost:8088
kill $SERVER_PID

mcp-runtime server validate --metadata-dir .mcp

mcp-runtime auth use alice
mcp-runtime server build image workspace-demo --tag v1
# prints: registry.example.com/acme/workspace-demo:v1

mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant

mcp-runtime server deploy workspace-demo --scope tenant --metadata-dir .mcp

mcp-runtime server list
mcp-runtime server get workspace-demo --namespace mcp-team-acme
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme
```

### Inspect and manage

```bash
mcp-runtime server list
mcp-runtime server get workspace-demo --namespace mcp-team-acme
mcp-runtime server status --namespace mcp-team-acme
mcp-runtime server connect-config workspace-demo --namespace mcp-team-acme --client claude
mcp-runtime server policy inspect workspace-demo --namespace mcp-team-acme
mcp-runtime server delete workspace-demo
mcp-runtime server generate --metadata-dir .mcp --output manifests/
```

---

## catalog

**[User]** platform API only

```bash
mcp-runtime catalog tools
mcp-runtime catalog tools --query invoice --risk high
mcp-runtime catalog tools --namespace mcp-team-acme --side-effect write
mcp-runtime catalog tool refund_invoice --server payments --output json
```

The catalog is visibility-only. It shows tools from visible servers with trust,
side effect, computed or declared risk, drift (`declared`, `ungoverned`,
`missing`), and copyable connect config.

### Direct Kubernetes operations (--use-kube) [Admin]

```bash
mcp-runtime server create workspace-demo --image repo/workspace-demo --tag v1 \
  --namespace mcp-team-acme --use-kube
mcp-runtime server apply  --file server.yaml --use-kube
mcp-runtime server export workspace-demo --namespace mcp-team-acme --use-kube
mcp-runtime server patch  workspace-demo --namespace mcp-team-acme \
  --patch '{"spec":{"imageTag":"v2"}}' --use-kube
mcp-runtime server logs   workspace-demo --namespace mcp-team-acme --follow --use-kube
```

---

## registry

**[User]** for `push` — **[Operator]** for `status`, `info`, `provision`

```bash
# Inspect
mcp-runtime registry status
mcp-runtime registry info

# Configure an external registry
mcp-runtime registry provision --url registry.example.com

# Push — always use the exact ref from server build image
mcp-runtime registry push \
  --image registry.example.com/acme/workspace-demo:v1 \
  --scope tenant
```

---

## access

**[User]** for grants — **[Admin]** for session `apply`

> Full reference: [API reference](api.md)

Tool names in grants must exactly match `.mcp/servers.yaml`. Run
`server validate --grant-file grant.yaml` before applying to catch mismatches
that cause `tool_side_effect_unknown` at the gateway.

### Grants

```bash
# Allow specific tools (read side-effect, low trust)
mcp-runtime access grant init workspace-ops \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --tool upper \
  --output grant.yaml

# Mixed allow/deny rules: --tool-rule name:allow|deny:low|medium|high
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

# Manage
mcp-runtime access grant list
mcp-runtime access grant list    --namespace mcp-team-acme
mcp-runtime access grant get     workspace-ops --namespace mcp-team-acme
mcp-runtime access grant disable workspace-ops --namespace mcp-team-acme
mcp-runtime access grant enable  workspace-ops --namespace mcp-team-acme
mcp-runtime access grant delete  workspace-ops --namespace mcp-team-acme
```

### Sessions

Agents normally get sessions automatically via `adapter --auto-refresh`. Use
`session init` + `session apply` only for explicit manual sessions.

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
mcp-runtime access grant init workspace-to-globex \
  --server workspace-demo \
  --namespace mcp-team-acme \
  --team-id <globex-team-uuid> \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant-cross.yaml
mcp-runtime access grant apply --file grant-cross.yaml

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

**[User]**

> Full guide: [Agent adapters](agent-adapters.md)

The adapter injects platform session and governance headers before every request
reaches the MCP server. When `--server` is set, the adapter creates the session
automatically. `--agent` (session name) is required in that case. `--agent-id`
sets the identity header forwarded to the server.

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

Once the adapter is running, point any MCP client at `http://127.0.0.1:8099`.
Session creation and governance headers are handled transparently.

---

## team

**[Admin]** — all `team` commands require the platform API admin role.

> Full guide: [Multi-team isolation](multi-team.md)

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list

MCP_PLATFORM_API_PROFILE=admin mcp-runtime team create acme --name "Acme Corp"

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

Note: `team init` is deprecated. Use `team create`.

---

## sentinel

**[Operator]** — requires `KUBECONFIG` with cluster-admin RBAC.

> Full guide: [Sentinel](sentinel.md)

```bash
KUBECONFIG=~/.kube/config mcp-runtime sentinel status
KUBECONFIG=~/.kube/config mcp-runtime sentinel events

# Logs — supports --follow, --tail, --since, --previous
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs api --since 15m --follow
KUBECONFIG=~/.kube/config mcp-runtime sentinel logs ingest --tail 200

# Restart
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart gateway
KUBECONFIG=~/.kube/config mcp-runtime sentinel restart --all

# Port-forward a component locally
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward ui
KUBECONFIG=~/.kube/config mcp-runtime sentinel port-forward grafana
```

Component names for `logs` and `restart`:
`clickhouse`, `zookeeper`, `kafka`, `ingest`, `processor`, `api`, `ui`,
`gateway`, `prometheus`, `grafana`, `otel-collector`, `tempo`, `loki`, `promtail`

---

## bootstrap

**[Operator]** — run before `setup` on a fresh cluster.

> Full guide: [Cluster readiness](cluster-readiness.md)

```bash
mcp-runtime bootstrap
mcp-runtime bootstrap --provider k3s
mcp-runtime bootstrap --apply --provider k3s    # automated fix on k3s
```

---

## setup

**[Operator]** — runs pre-flight checks automatically before installing anything.

```bash
# Recommended: drive all flags from an env file
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

Key env vars for `--env-file` (see `config/deployments/mcpruntime-org.env.example`):

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

**[Operator]**

> Full guide: [Deployment targets](deployment-targets.md)

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


## Further reading

| Topic | Link |
|---|---|
| Build, push, deploy flow | [Publish an MCP Server](publish-mcp-server.md) |
| MCPServer, MCPAccessGrant, MCPAgentSession fields | [API reference](api.md) |
| HTTP proxy and stdio adapter | [Agent adapters](agent-adapters.md) |
| Multi-team namespaces and RBAC | [Multi-team isolation](multi-team.md) |
| Sentinel logs, events, restart | [Sentinel](sentinel.md) |
| Distro-specific cluster prerequisites | [Cluster readiness](cluster-readiness.md) |
| Kind, EKS, k3s deployment | [Deployment targets](deployment-targets.md) |
