# Module 3 — Multi-team production setup

Two teams, separate namespaces, one shared server, cross-team access with
explicit grants. This is the isolation model you will use in production.

**Prerequisites:**
- Module 2 completed (you have deployed a server and understand grants)
- Admin credentials on the platform

---

## What we are building

```
Team Acme owns:   payments server  (namespace: mcp-team-acme)
Team Globex owns: workspace server (namespace: mcp-team-globex)

Acme grants Globex's cursor agent access to payments/list_invoices
```

This is the core multi-team pattern: servers live in team namespaces, grants
carry the team ID so the gateway knows which team the agent is acting for.

---

## Step 1 — Create two teams (admin)

```bash
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
```

Verify:

```bash
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list
```

---

## Step 2 — Alice deploys payments server (Acme)

```bash
mcp-runtime auth login \
  --api-url https://platform.example.com \
  --email alice@acme.com --password 'alice123' \
  --profile alice
mcp-runtime auth use alice
```

Scaffold metadata from the running server, validate, build, push, deploy:

```bash
cd examples/workspace-assistant-mcp
go run . &; SERVER_PID=$!
mcp-runtime server init payments --from-server http://localhost:8088
kill $SERVER_PID

mcp-runtime server validate --metadata-dir .mcp
mcp-runtime server build image payments --tag v1
mcp-runtime registry push --image registry.example.com/acme/payments:v1 --scope tenant
mcp-runtime server deploy payments --scope tenant --metadata-dir .mcp
```

Confirm:

```bash
mcp-runtime server list
# NAME      NAMESPACE      READY   STATUS
# payments  mcp-team-acme  1/1     Ready
```

---

## Step 3 — Get Globex's team UUID

Cross-team grants need the team UUID (not slug). Get it from the admin API:

```bash
# List teams as admin to find the UUID
MCP_PLATFORM_API_PROFILE=admin mcp-runtime team list
# Shows: globex  Globex Corp  mcp-team-globex

# The UUID comes from server get or is shown in access grant list output
# For now note it from: platform dashboard → Teams → Globex → copy UUID
# Or: mcp-runtime access session list will show teamID in output
```

---

## Step 4 — Alice grants Globex access to payments

```bash
mcp-runtime auth use alice

mcp-runtime access grant init payments-to-globex \
  --server payments \
  --namespace mcp-team-acme \
  --team-id <globex-team-uuid> \
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant-cross.yaml

# Validate the grant matches the server metadata
mcp-runtime server validate --metadata-dir .mcp --grant-file grant-cross.yaml

mcp-runtime access grant apply --file grant-cross.yaml
mcp-runtime access grant list --namespace mcp-team-acme
```

The grant is in Acme's namespace (where the server lives) but it is scoped to
Globex's team ID. Globex's agents can use it; nobody else can.

---

## Step 5 — Admin creates a session for Globex's agent

Session apply requires admin role:

```bash
mcp-runtime auth use admin

mcp-runtime access session init globex-payments-session \
  --server payments \
  --namespace mcp-team-acme \
  --team-id <globex-team-uuid> \
  --agent-id cursor \
  --trust low \
  --expires-in 4h \
  --output session-cross.yaml

mcp-runtime access session apply --file session-cross.yaml
mcp-runtime access session list
```

---

## Step 6 — Bob connects via the adapter (Globex)

```bash
mcp-runtime auth use bob   # bob@globex.com

mcp-runtime adapter proxy \
  --runtime-url https://mcp.example.com/payments/mcp \
  --server payments \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099 &
```

Connect Claude Desktop or any MCP client to `http://127.0.0.1:8099`.
Bob can call `echo` and `add` on Acme's payments server — the gateway
enforces the cross-team grant.

---

## Step 7 — Verify isolation

Alice's payments server is NOT accessible without a grant. To confirm:

1. Have Bob try to call a tool without the grant:
   ```bash
   mcp-runtime access grant delete payments-to-globex --namespace mcp-team-acme
   ```
2. Bob's next tool call is denied — no grant, no access.
3. Re-apply the grant to restore access.

Namespace isolation means Bob has no Kubernetes RBAC access to Acme's namespace.
The grant is enforced at the gateway level, not by network policy alone.

---

## Step 8 — See cross-team traffic in analytics

Open the platform dashboard → **Analytics → Tools**.

You will see rows with:
- User: `bob@globex.com`
- Team: `globex`
- Agent: `cursor`
- Server: `payments` (Acme's server)

Cross-team access is visible and audited. Every call shows which team made it,
which server it hit, and whether it was allowed.

---

## What you have built

- Two isolated team namespaces with RBAC and NetworkPolicy
- A server owned by one team, accessed by another via an explicit grant
- A revocable, time-limited session carrying the consuming team's identity
- A full audit trail of cross-team tool calls

---

## Production checklist before going live

- [ ] TLS enabled (`--with-tls --tls-cluster-issuer ...`)
- [ ] Registry mode `bundled-https` or external registry
- [ ] Sessions have reasonable `--expires-in` (4h–24h for day-to-day work)
- [ ] Admin credentials rotated from default
- [ ] `cluster doctor` passes all 37 checks
- [ ] Troubleshooting page bookmarked: [Troubleshooting](../troubleshooting.md)

---

**You have completed the MCP Runtime learning path.**

- [CLI reference](../cli.md) — every command
- [API reference](../api.md) — CRD fields
- [Troubleshooting](../troubleshooting.md) — common errors
- [Contribute](../contributor/README.md) — help build MCP Runtime
