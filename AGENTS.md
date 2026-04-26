# AGENTS.md — developer and AI-agent guide

This file is the **onboarding and operations runbook** for the MCP Runtime repo. Humans and coding agents (Cursor, Copilot, Codex, etc.) should use it to **run the right checks**, **find the right code**, and **debug the stack** without re-deriving structure from scratch. It complements `README.md` (product overview) with **workstation commands**, **layout**, and **failure modes**.

If instructions conflict, prefer **this repo** (`README`, CRDs, `v1alpha1` types) over generic Kubernetes or MCP advice.

## Repository map (where to look)

| Area | Path | Notes |
|------|------|--------|
| User-facing CLI | `cmd/mcp-runtime/`, `internal/cli/` | `setup`, `status`, `registry`, `server`, `access`, … |
| Operator (controller) | `cmd/operator/`, `internal/operator/` | `MCPServer` reconciliation, ingress, gateway wiring |
| API & CRD types | `api/v1alpha1/` | Source of truth for object shapes; CRD YAML in `config/crd/bases/` |
| Access control (shared) | `pkg/access/` | Grants, sessions, policy pieces used by API and gateway |
| K8s helpers, manifests, metadata | `pkg/k8sclient/`, `pkg/manifest/`, `pkg/metadata/` | Registry image resolution, YAML helpers |
| Sentinel services | `services/api`, `services/ui`, `services/ingest`, `services/processor`, `services/mcp-proxy`, … | Separate `go.mod` where present; test in subdirs in CI |
| Example MCP server | `examples/go-mcp-server/` | Reference for tools and routes |
| Default cluster install YAML | `k8s/`, `config/` | Overlays, CRDs, cert-manager examples |
| Traefik plugins (dev) | `services/traefik-plugins/` | e.g. PII redactor source for local overlays |
| Site / public docs (if editing) | `website/` | Not required for control-plane work |
| E2E | `test/e2e/`, `test/integration/` | Kind script and envtest-based integration tests |

**Patterns worth mirroring:** search for similar packages before adding new abstractions; keep CLI errors consistent with `internal/cli/errors.go` and `pkg/errx/`.

## Build, test, and quality (before you push)

Use **Go** from `go.mod` (see `go version` / toolchain). From the repo root:

```bash
# Format (CI fails if this prints paths)
gofmt -s -l .   # if empty, OK; else run: gofmt -s -w .

go build -o bin/mcp-runtime ./cmd/mcp-runtime

# Fast feedback (matches most of CI for the main module)
go test ./... -count=1 -race
go vet ./...
```

Optional but used in CI: `staticcheck ./...` (install: `go install honnef.co/go/tools/cmd/staticcheck@latest`).

**Targeted tests** (prefer these while iterating; full `./...` can be slow):

- `go test ./internal/operator/... ./internal/cli/... -race -count=1`
- `go test ./test/golden/... -count=1` (CLI help snapshots; update `test/golden/cli/testdata/*.golden` when you change Cobra help text on purpose)
- `go test ./test/integration/...` (needs `KUBEBUILDER_ASSETS`; see `Makefile.operator` and CI for envtest setup)
- `services/api` and `services/ui`: `go test -race -count=1 ./...` inside each directory (CI runs these explicitly)

**CI** (`.github/workflows/ci.yaml`) runs: `gofmt` check, `go vet`, `staticcheck`, unit tests, golden tests, service tests, `test/integration`, then Kind e2e on `main`/`PR` branches. Align local changes with that before opening a PR.

## Conventions for code changes

- **Scope:** Change only what the task needs; do not “clean up” unrelated files. Match naming and patterns in the nearest similar code.
- **Tests:** Add or adjust tests in the same package when behavior changes. For CLI output, expect golden file updates.
- **Docs you were not asked to edit:** Avoid adding new top-level docs unless the task needs them; this file, `README`, and existing doc trees are the defaults for agents.
- **Secrets and prod:** This repo is **alpha**; do not hardcode real credentials. Use the existing secret and env patterns documented below.

## Local dev setup (Kind and CLI)

- **Prereqs:** Docker, Kind, `kubectl`, `curl`, `jq`, Python 3; Go for building the CLI.
- **Quick start:**

```bash
kind create cluster --name mcp-runtime
./bin/mcp-runtime bootstrap                              # preflight cluster prerequisites
./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http
kubectl port-forward -n traefik svc/traefik 18080:8000   # expose ingress
```

- **Status:** `./bin/mcp-runtime status`
- **Preflight only (no apply):** `./bin/mcp-runtime bootstrap`. For k3s: add `--apply --provider k3s` to install bundled CoreDNS / local-path manifests (server node only).

## Endpoints and auth

- UI: `http://localhost:18080/`
- Grafana: `/grafana` · Prometheus: `/prometheus` · API base: `http://localhost:18080/api`
- MCP (test): `http://localhost:18080/demo-one/mcp`, `http://localhost:18080/demo-two/mcp`
- PII redaction: `config/ingress/overlays/http` with Traefik plugin `pii-redactor@file`. Reapply: `./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http`. The plugin is built from `services/traefik-plugins/pii-redactor` (local `localplugins` mount) so a published image tag is not required for local dev.
- **API key:**

```bash
kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d
```

  Keep `API_KEYS` and `UI_API_KEY` aligned; copy the secret to `mcp-servers` if MCP servers need it.

### Platform domain and TLS (short)

- **Let’s Encrypt:** `./bin/mcp-runtime setup --with-tls --acme-email <addr>`. Set `MCP_PLATFORM_DOMAIN` to an apex (e.g. `mcpruntime.com`) and DNS for `registry.<domain>` and `mcp.<domain>`, or set `MCP_REGISTRY_HOST` / `MCP_REGISTRY_INGRESS_HOST` for public names. Port 80 must hit Traefik for HTTP-01. With a platform domain, cert-manager can issue one `Certificate` with both SANs; the `registry-tls` `Secret` lives in the `registry` namespace (copy to other namespaces if the `mcp.` `Ingress` is elsewhere). Staging: `--acme-staging` / `MCP_ACME_STAGING=1`. Private CA without ACME: omit `--acme-email` and use the `mcp-runtime-ca` path per `config/cert-manager/`.
- **Internal / enterprise CA:** Install your `ClusterIssuer` first, then: `--with-tls --tls-cluster-issuer <name>` (or `MCP_TLS_CLUSTER_ISSUER`). Setup does not create the issuer; it applies the `Certificate` and waits. Mutually exclusive with `--acme-email`.
- **Operator default host:** `MCP_PLATFORM_DOMAIN` and related env can drive `MCP_DEFAULT_INGRESS_HOST` to `mcp.<domain>` when configured.

## Debugging checklist (common failures)

- **“ingressHost is required” (operator):** set `spec.ingressHost` on the `MCPServer`, or operator env `MCP_DEFAULT_INGRESS_HOST`, or `MCP_PLATFORM_DOMAIN` for `mcp.<domain>` defaults.
- **Port mismatch:** the bundled Go example listens on `8088` by default; align `MCPServer` `port` / `servicePort` and container `PORT` if you overrode them.
- **Analytics 401:** use gateway/ingest URL and key, not the app’s random env. Example: `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events` and `ANALYTICS_API_KEY` from `mcp-sentinel-secrets` (`API_KEYS` key).
- **Secret not found in workload namespace:** copy `mcp-sentinel-secrets` or use a shared secret reference.
- **Dashboard / API 401:** align `API_KEYS` and `UI_API_KEY` and roll the API deployment.
- **Ingress / routes:** `kubectl get ingress -A` and confirm paths match the gateway and demo servers you expect.

## Governance (grants and sessions)

- **UI** can create/apply grants and sessions and toggle grant enablement and session state.
- **CLI:** `mcp-runtime access grant apply --file <file.yaml>` and `mcp-runtime access session apply --file <file.yaml>`. `kubectl apply -f` is still a valid fallback.
- **Example**

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

- **HTTP API (requires `x-api-key`):** `POST /api/runtime/grants`, `POST /api/runtime/sessions`; the API checks that `serverRef` matches an existing `MCPServer` (best-effort, not transactional). Toggles: `POST /api/runtime/grants/{ns}/{name}/enable|disable`, `POST /api/runtime/sessions/{ns}/{name}/revoke|unrevoke`.
- **Kind e2e** applies generated access YAML, waits for gateway policy materialization, and exercises real MCP JSON-RPC for allow/deny.

## Traffic generation (MCP JSON-RPC)

**Single call** (set `<session>` from the `initialize` response):

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

**Bulk (Python)** — fires many `tools/call` events for ingest testing:

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

## Logs and observability

- Operator: `kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager`
- Sentinel: `kubectl logs -n mcp-sentinel deploy/<api|ingest|processor|ui|gateway>`
- **Cluster summary:** `./bin/mcp-runtime status`
- Dashboards: Grafana and Prometheus via the ingress base URL in dev.

## Clean start (keep the cluster, wipe user workloads)

Use when you need a **fresh** install without removing Kind/k3s. **Destructive** to application namespaces and most namespaced resources.

```bash
kubectl config current-context
kubectl get nodes

to_delete="$(kubectl api-resources --verbs=delete --namespaced -o name | paste -sd, -)"
if [ -n "$to_delete" ]; then
  kubectl delete "$to_delete" --all -A --ignore-not-found --grace-period=0 --force
fi

for r in $(kubectl api-resources --verbs=delete --namespaced=false -o name); do
  kubectl delete "$r" --all --ignore-not-found --grace-period=0 --force || true
done

ns_to_delete="$(kubectl get ns --no-headers | awk '{print $1}' | grep -E -v '^(kube-system|kube-public|kube-node-lease|default)$')"
if [ -n "$ns_to_delete" ]; then
  printf '%s\n' "$ns_to_delete" | xargs kubectl delete ns
fi

kubectl delete all,cm,secret,ing,svc,sa,role,rolebinding,deploy,ds,sts,job,cronjob,pvc --all -n default --ignore-not-found --grace-period=0 --force
```

## Further reading

- **README** (`README.md`) — high-level product and quick start
- **K8s YAML** — `k8s/`
- **CRDs** — `config/crd/bases/`
- **API docs (published)** — https://mcpruntime.org/docs/ and https://mcpruntime.org/docs/api
- **Sample server** — `examples/go-mcp-server/`
- **Website source** — `website/` (documentation site, separate from the Go control plane)

---

*Tip for agents: after substantive edits, run the narrowest `go test` for touched packages, then `go test ./...` before suggesting merge. Update golden files only when help text or CLI output should change on purpose.*
