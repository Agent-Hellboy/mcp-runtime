---
name: qa-cluster-bringup
description: Stand up a real Kind+test-mode MCP Runtime cluster end-to-end (the contributor flow from docs/getting-started.md#3-contributor-test-mode-cluster) so the qa-e2e-* skills can run real validation. Use when Codex is asked to QA on a live cluster, prepare a target environment for qa-e2e-operations / qa-e2e-security / qa-e2e-ui / qa-e2e-perf, recover a broken contributor cluster, or verify a fresh setup --test-mode end-to-end. Owns cluster lifecycle; the other QA skills assume this has run.
---

# QA — Cluster bring-up (real Kind contributor flow)

## Overview

This skill provisions or recovers the **real** contributor cluster described in
`docs/getting-started.md#3-contributor-test-mode-cluster`. It is the entry
point for every other `qa-e2e-*` skill. It is **not** a unit-test skill — it
boots a Kind cluster, builds and pushes runtime images, installs the operator
and Sentinel stack, deploys the bundled Go MCP server, applies a working
grant + session, and exits only after a real MCP `tools/call` succeeds through
Traefik.

Default policy: **reuse if present, create if missing.** Never tear down an
existing `kind-mcp-runtime` cluster without explicit user confirmation —
contributors may have in-flight work on it.

## Step 1 — Decide cluster mode

State the mode in the report.

- **reuse** (default if `kind-mcp-runtime` cluster exists and `kubectl
  --context kind-mcp-runtime get nodes` succeeds). Skip Kind creation;
  re-run `bootstrap` and `cluster doctor` only.
- **create** (no `kind-mcp-runtime` context, or user asked for a clean
  cluster). Full path: Kind create → build → setup → deploy demo → grant.
- **rebuild-from-broken** (cluster exists but `cluster doctor` fails). Try
  targeted repair first (rollout restart, re-apply `pipeline deploy`); only
  recreate with the user's explicit ok.

Detect with:

```bash
kind get clusters | grep -qx mcp-runtime && echo "reuse" || echo "create"
kubectl config get-contexts -o name | grep -qx kind-mcp-runtime || echo "no-context"
```

## Step 2 — Host preflight

Run from repo root. Missing tools become recorded blockers, not silent skips.

```bash
command -v docker kind kubectl curl jq python3 go
docker info >/dev/null
STRICT_DEPS_CHECK=1 make deps-check
```

If `make deps-check` reports missing tools, stop here and report — do not
`brew install` or `apt install` without explicit user consent.

## Step 3 — Cluster create (only in **create** mode)

Exactly as documented; the containerd mirror is required for image pulls in
test mode.

```bash
cat > /tmp/mcp-runtime-kind.yaml <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
      endpoint = ["http://127.0.0.1:32000"]
EOF

kind create cluster --name mcp-runtime --config /tmp/mcp-runtime-kind.yaml
kubectl config use-context kind-mcp-runtime
```

## Step 4 — Build CLI and install platform

```bash
make deps
make build
./bin/mcp-runtime bootstrap

MCP_SETUP_WAIT_TIMEOUT=900 \
  ./bin/mcp-runtime setup --test-mode \
  --ingress-manifest config/ingress/overlays/http
```

In **reuse** mode, skip `kind create`; still run `make build` (CLI may be
stale) and `bootstrap`. Skip `setup` only if `cluster doctor` already
reports a healthy install; otherwise rerun setup so manifests catch up to
HEAD.

## Step 5 — Health gate

The skill must not exit Step 5 successfully until every check passes.

```bash
./bin/mcp-runtime status
./bin/mcp-runtime cluster status
./bin/mcp-runtime registry status
./bin/mcp-runtime sentinel status
./bin/mcp-runtime cluster doctor
kubectl get pods -A | grep -Ev 'Running|Completed' || echo OK
```

If `cluster doctor` reports admin/UI/ingest key mismatches, roll the API,
UI, ingest, and gateway deployments after patching `mcp-sentinel-secrets`
(see `CLAUDE.md` → API keys). Do not paper over a `Degraded` reading.

## Step 6 — Expose the gateway

```bash
pgrep -f 'port-forward.*svc/traefik' >/dev/null \
  || kubectl port-forward -n traefik svc/traefik 18080:8000 \
       >/tmp/mcp-runtime-traefik-pf.log 2>&1 &

curl -fsS -o /dev/null http://localhost:18080/ && echo "dashboard reachable"
```

## Step 7 — Deploy the bundled Go MCP example

Use the metadata from `docs/getting-started.md#3-contributor-test-mode-cluster`
exactly — gateway policy, analytics, and required headers all depend on the
documented shape.

```bash
cat > /tmp/go-example-mcp.yaml <<'EOF'
version: v1
servers:
  - name: go-example-mcp
    route: /go-example-mcp/mcp
    publicPathPrefix: go-example-mcp
    port: 8088
    namespace: mcp-servers
    envVars:
      - { name: MCP_PATH, value: /go-example-mcp/mcp }
    tools:
      - { name: add,   requiredTrust: low }
      - { name: upper, requiredTrust: medium }
    auth:
      mode: header
      humanIDHeader: X-MCP-Human-ID
      agentIDHeader: X-MCP-Agent-ID
      sessionIDHeader: X-MCP-Agent-Session
    policy:
      mode: allow-list
      defaultDecision: deny
      policyVersion: v1
    session: { required: true }
    gateway: { enabled: true }
    analytics:
      enabled: true
      ingestURL: http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events
      apiKeySecretRef: { name: go-example-mcp-analytics, key: api-key }
EOF

API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.INGEST_API_KEYS}' | base64 -d | cut -d, -f1)"
kubectl create secret generic go-example-mcp-analytics -n mcp-servers \
  --from-literal=api-key="$API_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -

./bin/mcp-runtime server build image go-example-mcp \
  --metadata-file /tmp/go-example-mcp.yaml \
  --dockerfile examples/go-mcp-server/Dockerfile \
  --context examples/go-mcp-server \
  --registry registry.registry.svc.cluster.local:5000 \
  --tag dev

./bin/mcp-runtime registry push \
  --image registry.registry.svc.cluster.local:5000/go-example-mcp:dev

rm -rf /tmp/go-example-mcp-manifests
./bin/mcp-runtime pipeline generate \
  --file /tmp/go-example-mcp.yaml \
  --output /tmp/go-example-mcp-manifests
./bin/mcp-runtime pipeline deploy --dir /tmp/go-example-mcp-manifests
kubectl rollout status deploy/go-example-mcp -n mcp-servers --timeout=180s
```

## Step 8 — Apply baseline grant + session

```bash
cat > /tmp/go-example-access.yaml <<'EOF'
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata: { name: go-example-local, namespace: mcp-servers }
spec:
  serverRef: { name: go-example-mcp }
  subject: { humanID: local-user, agentID: local-agent }
  maxTrust: high
  policyVersion: v1
  toolRules:
    - { name: add,   decision: allow }
    - { name: upper, decision: allow }
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata: { name: local-session, namespace: mcp-servers }
spec:
  serverRef: { name: go-example-mcp }
  subject: { humanID: local-user, agentID: local-agent }
  consentedTrust: high
  policyVersion: v1
EOF
kubectl apply -f /tmp/go-example-access.yaml

until ./bin/mcp-runtime server policy inspect go-example-mcp --namespace mcp-servers \
  | grep -q local-session; do sleep 2; done
sleep 6   # proxy sidecar polls; do not skip this wait
```

## Step 9 — Real MCP traffic gate

Bring-up is only "done" once a real `tools/call` returns `5`. This catches
the regressions that unit tests miss (Traefik routing, gateway policy
materialization, proxy reload, auth headers, analytics path).

```bash
BASE=http://localhost:18080/go-example-mcp/mcp
PROTO=2025-06-18
H=(-H "content-type: application/json"
   -H "accept: application/json, text/event-stream"
   -H "Mcp-Protocol-Version: $PROTO"
   -H "X-MCP-Human-ID: local-user"
   -H "X-MCP-Agent-ID: local-agent"
   -H "X-MCP-Agent-Session: local-session")

SESSION="$(curl -si "${H[@]}" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' "$BASE" \
  | awk -F': ' 'tolower($1)=="mcp-session-id"{print $2}' | tr -d '\r')"
[ -n "$SESSION" ] || { echo "FAIL: no session id"; exit 1; }

curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$BASE" >/dev/null

RESP="$(curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}' "$BASE")"
echo "$RESP" | jq -e '.. | .text? // empty' | grep -q '"5"' \
  || { echo "FAIL: tools/call did not return 5: $RESP"; exit 1; }
```

If this fails, do not declare bring-up successful. Capture
`kubectl logs -n mcp-servers <pod> -c mcp-gateway --tail=120` and
`kubectl logs -n traefik deploy/traefik --tail=120`, then return.

## Step 10 — Report

The bring-up report is short and concrete. Other `qa-e2e-*` skills consume it
to know the environment is ready.

- Mode: reuse | create | rebuild-from-broken.
- Cluster context: `kubectl config current-context`.
- Image SHAs pushed (operator, gateway proxy, sentinel api/ui/ingest/processor,
  go-example-mcp).
- `cluster doctor` summary line.
- Traefik port-forward pid + log path.
- Demo `tools/call` result: `5` ✓ / details on failure.
- Test-mode credentials reminder (do **not** print passwords; just say
  `PLATFORM_DEV_LOGIN` is enabled).
- Known follow-ups (e.g. `MCPServer` `PartiallyReady` is expected on Kind
  without LB status — set `MCP_INGRESS_READINESS_MODE=permissive` only if a
  later skill needs strict readiness).

## When NOT to use this skill

- Unit / golden / envtest validation — those run without a cluster; do them
  in the package directly per `CLAUDE.md` "Targeted tests."
- Production / TLS / DNS validation — this skill is dev/HTTP only. Production
  needs the full `MCP_PLATFORM_DOMAIN` flow with cert-manager, which is out of
  scope.
- Tearing down a cluster a contributor is using — confirm before destructive
  steps; prefer `kubectl delete` of just the affected namespaces.
