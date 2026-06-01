# Module 2 — Your first governed server

End-to-end hands-on: deploy a real MCP server, create a grant, connect a client,
and observe live traffic in the analytics dashboard.

**Prerequisites:**
- Module 1 completed (you understand Grants, Sessions, and the gateway)
- `mcp-runtime` CLI installed ([download v0.1.0](https://github.com/Agent-Hellboy/mcp-runtime/releases/tag/v0.1.0))
- Account on the live platform (`platform.mcpruntime.org`) or a local cluster running

---

## Step 1 — Log in

```bash
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email you@example.com --password '...' \
  --profile me

mcp-runtime auth status    # confirm profile is active
```

---

## Step 2 — Get the example server

Clone the repo to use the workspace-assistant MCP server:

```bash
git clone https://github.com/Agent-Hellboy/mcp-runtime
cd mcp-runtime/examples/workspace-assistant-mcp
```

This is a Go MCP server with 8 tools: `echo`, `add`, `upper`, `lower`,
`create_task`, `draft_release_note`, `slugify`, `aaa-ping`.

---

## Step 3 — Discover tools and scaffold metadata

Run the server locally so `server init` can call its `tools/list` endpoint:

```bash
go run . &
SERVER_PID=$!

mcp-runtime server init my-server \
  --from-server http://localhost:8088
# Discovered: aaa-ping, add, create_task, draft_release_note,
#             echo, lower, slugify, upper

kill $SERVER_PID
```

Open `.mcp/servers.yaml` and look at what was generated. Notice every tool
has a `sideEffect` and `requiredTrust`. These are what the gateway enforces.

---

## Step 4 — Validate before building

```bash
mcp-runtime server validate --metadata-dir .mcp
```

This catches tool name mismatches before you spend time on a build.

---

## Step 5 — Build, push, deploy

```bash
# Build (from the directory with the Dockerfile)
mcp-runtime server build image my-server --tag v1
# Prints: registry.example.com/myteam/my-server:v1

# Push — use the exact ref printed above
mcp-runtime registry push \
  --image registry.example.com/myteam/my-server:v1 \
  --scope tenant

# Deploy
mcp-runtime server deploy my-server --scope tenant --metadata-dir .mcp
```

---

## Step 6 — Confirm the server is up

```bash
mcp-runtime server list
# NAME       NAMESPACE          READY   STATUS
# my-server  mcp-team-myteam    1/1     Ready
```

```bash
mcp-runtime server get my-server --namespace mcp-team-myteam
mcp-runtime server policy inspect my-server --namespace mcp-team-myteam
```

The policy inspect output shows the full policy document the gateway will
enforce — every tool, its trust level, and its side-effect class.

---

## Step 7 — Create a grant

Grant your cursor agent access to `echo` and `add`:

```bash
mcp-runtime access grant init my-grant \
  --server my-server \
  --namespace mcp-team-myteam \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant.yaml

# Always validate the grant against the metadata before applying
mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml

mcp-runtime access grant apply --file grant.yaml
mcp-runtime access grant list
```

**Why validate first?** If `echo` is not in `.mcp/servers.yaml`, the gateway
returns `tool_side_effect_unknown` and denies the call. Validate catches this
before you deploy.

---

## Step 8 — Connect via the adapter

Start the adapter proxy. It creates the agent session automatically:

```bash
mcp-runtime adapter proxy \
  --runtime-url https://mcp.mcpruntime.org/my-server/mcp \
  --server my-server \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099
```

Point Claude Desktop, Cursor, or any MCP client at `http://127.0.0.1:8099`.

Call the `echo` tool — it goes through. Try `create_task` — it is denied
because it is not in the grant. That is the gateway enforcing policy.

---

## Step 9 — See it in analytics

Open [platform.mcpruntime.org](https://platform.mcpruntime.org) → **Analytics → Tools**.

You should see rows like:

| Server | Tool | User | Team | Agent | Calls | Denied |
|---|---|---|---|---|---|---|
| my-server | echo | you@example.com | myteam | cursor | 3 | 0 |

Every call is recorded with the full identity context. A denied call shows
`Denied: 1`.

---

## What just happened

You deployed a Kubernetes-native MCP server with:

- **No YAML written** — the operator created Deployment, Service, and Ingress
- **Policy enforced at the gateway** — the grant's allow list blocked `create_task`
- **Audit trail** — every call is in the analytics database with user, team, agent, tool, decision

---

## Try breaking it intentionally

1. Delete the grant: `mcp-runtime access grant delete my-grant --namespace mcp-team-myteam`
2. Try calling `echo` again — all calls are now denied
3. Re-apply the grant: `mcp-runtime access grant apply --file grant.yaml`
4. Calls go through again

This is the revocation model. You can also revoke the session:
```bash
mcp-runtime access session list --namespace mcp-team-myteam
mcp-runtime access session revoke <session-name> --namespace mcp-team-myteam
```

---

**Next:** [Module 3 — Multi-team production setup](module-3-multi-team.md)
