# Agent Dev & Debug Guide

Practical notes for spinning up, debugging, and exercising the MCP Runtime stack locally—optimized for agent-driven workflows (but still fine for humans).

## 1) Overview
- Audience: anyone developing or debugging the control plane, services, or MCP servers.
- Scope: local Kind flow, endpoints, auth, common break/fix steps, traffic generation, governance resources.

## 2) Dev Setup
- Prereqs: Docker Desktop running, Kind, kubectl, curl, jq, Python 3.
- Quick start (Kind):
  ```bash
  kind create cluster --name mcp-runtime
  ./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http
  kubectl port-forward -n traefik svc/traefik 18080:8000   # expose ingress
  ```
- Status check: `./bin/mcp-runtime status`

## 3) Endpoints & Auth Reference
- UI / Dashboard: `http://localhost:18080/`
- Grafana: `/grafana`
- Prometheus: `/prometheus`
- API base: `http://localhost:18080/api`
- MCP servers (test): `http://localhost:18080/demo-one/mcp`, `http://localhost:18080/demo-two/mcp`
- PII redaction middleware: enabled in the `config/ingress/overlays/http` overlay via Traefik plugin `pii-redactor@file` (Go 1.22). To (re)apply locally: `./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http`. Local Kind/dev flows mount the bundled plugin source from `services/traefik-plugins/pii-redactor` via Traefik `localplugins` so the middleware works without a separately published plugin tag.
- API key (for UI/API):
  ```bash
  kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
    -o jsonpath='{.data.UI_API_KEY}' | base64 -d
  ```
  Ensure `API_KEYS` and `UI_API_KEY` in that secret match; copy the secret to `mcp-servers` namespace when servers need it.

## 4) Common Debugging Checklist
- Ingress host missing -> operator error “ingressHost is required”; set `spec.ingressHost` or env `MCP_DEFAULT_INGRESS_HOST`.
- Port mismatch -> the bundled Go example listens on 8088 by default; set MCPServer `port` and `servicePort` to 8088 unless you intentionally override `PORT`.
- Analytics 401 -> verify proxy/gateway analytics wiring, not app-server env vars:
  - `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events`
  - `ANALYTICS_API_KEY` (from `mcp-sentinel-secrets`, `API_KEYS` key)
- Secret not found -> copy `mcp-sentinel-secrets` into `mcp-servers` or reference a central secret.
- Dashboard/API 401 -> align `API_KEYS` and `UI_API_KEY`; restart API deployment if changed.
- Ingress/paths -> verify with `kubectl get ingress -A`.

## 5) Governance (Grants & Sessions)
- UI can list/toggle grants/sessions; creation is CLI/kubectl today.
- Example manifests:
  ```yaml
  apiVersion: mcpruntime.org/v1alpha1
  kind: MCPAccessGrant
  metadata:
    name: demo-one-grant
    namespace: mcp-servers
  spec:
    subject: {humanID: user-123, agentID: ops-agent}
    serverRef: {name: demo-one, namespace: mcp-servers}
    maxTrust: high
    toolRules:
      - {name: add, decision: allow, requiredTrust: low}
      - {name: upper, decision: allow, requiredTrust: low}
  ---
  apiVersion: mcpruntime.org/v1alpha1
  kind: MCPAgentSession
  metadata:
    name: sess-ops-agent
    namespace: mcp-servers
  spec:
    subject: {humanID: user-123, agentID: ops-agent}
    serverRef: {name: demo-one, namespace: mcp-servers}
    consentedTrust: high
    policyVersion: v1
  ```
- Apply: `kubectl apply -f <file.yaml>`
- Toggle endpoints: `POST /api/runtime/grants/{ns}/{name}/enable|disable`, `POST /api/runtime/sessions/{ns}/{name}/revoke|unrevoke` (requires `x-api-key`).

## 6) Traffic Generation (MCP JSON-RPC)
- Single call (replace `<session>` after initialize):
  ```bash
  PROTO=2025-06-18
  BASE=http://localhost:18080/demo-one/mcp
  curl -i -H "content-type: application/json" \
       -H "accept: application/json, text/event-stream" \
       -H "Mcp-Protocol-Version: $PROTO" \
       -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' $BASE
  # then
  curl -i -H "content-type: application/json" \
       -H "accept: application/json, text/event-stream" \
       -H "Mcp-Protocol-Version: $PROTO" \
       -H "Mcp-Session-Id: <session>" \
       -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}' $BASE
  ```
- Bulk generator (events into ingest):
  ```bash
  python3 - <<'PY'
import json, urllib.request, random, time
bases = ["http://localhost:18080/demo-one/mcp","http://localhost:18080/demo-two/mcp"]
proto = "2025-06-18"; calls = 200
def post(base, payload, sess=None):
    h={"content-type":"application/json","accept":"application/json, text/event-stream","Mcp-Protocol-Version":proto,"Host":"localhost"}
    if sess: h["Mcp-Session-Id"]=sess
    req=urllib.request.Request(base, data=json.dumps(payload).encode(), headers=h)
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.status, r.headers.get("Mcp-Session-Id", sess)
for base in bases:
    st,sess = post(base, {"jsonrpc":"2.0","id":1,"method":"initialize","params":{}})
    post(base, {"jsonrpc":"2.0","method":"notifications/initialized"}, sess)
    for i in range(calls):
        a,b = random.randint(1,50), random.randint(1,50)
        post(base, {"jsonrpc":"2.0","id":i+2,"method":"tools/call","params":{"name":"add","arguments":{"a":a,"b":b}}}, sess)
        time.sleep(0.01)
print("done")
PY
  ```

## 7) Logs & Observability
- Operator: `kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager`
- Components: `kubectl logs -n mcp-sentinel deploy/<api|ingest|processor|ui|gateway>`
- Status table: `./bin/mcp-runtime status`
- Grafana/Prometheus reachable via the ingress base URL.

## 8) Clean Start (keep cluster, wipe workloads)
Use this when you want a “fresh start” without uninstalling k3s/kind itself.

⚠️ Destructive: deletes resources cluster-wide.

```bash
# sanity: confirm you are targeting the intended cluster
kubectl config current-context
kubectl get nodes

# delete everything in every namespace (pods/deployments/jobs/services/ingresses/etc.)
# NOTE: build deletable resource list dynamically to avoid errors on clusters where some optional APIs (e.g. PodSecurityPolicy) are absent.
to_delete="$(kubectl api-resources --verbs=delete --namespaced -o name | paste -sd, -)"
if [ -n "$to_delete" ]; then
  kubectl delete "$to_delete" --all -A --ignore-not-found --grace-period=0 --force
fi

# cluster-scoped resources (best-effort; some may not exist depending on cluster/version)
for r in $(kubectl api-resources --verbs=delete --namespaced=false -o name); do
  kubectl delete "$r" --all --ignore-not-found --grace-period=0 --force || true
done

# delete all non-system namespaces (wipes everything inside them)
ns_to_delete="$(kubectl get ns --no-headers | awk '{print $1}' | grep -E -v '^(kube-system|kube-public|kube-node-lease|default)$')"
if [ -n "$ns_to_delete" ]; then
  printf '%s\n' "$ns_to_delete" | xargs kubectl delete ns
fi

# optional: also wipe the default namespace
kubectl delete all,cm,secret,ing,svc,sa,role,rolebinding,deploy,ds,sts,job,cronjob,pvc --all -n default --ignore-not-found --grace-period=0 --force
```

## 9) Reference Links
- Project README: `README.md`
- K8s manifests: `k8s/`
- Sample MCP server: `examples/go-mcp-server/`
- CRDs: `config/crd/bases/`
