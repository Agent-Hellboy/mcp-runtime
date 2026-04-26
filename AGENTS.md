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

- **What `MCP_PLATFORM_DOMAIN` does:** with `export MCP_PLATFORM_DOMAIN=mcpruntime.org` (apex only, no `https://`), the CLI/operator resolve **registry**, **MCP**, and **platform** hostnames as:
  - `registry.mcpruntime.org` (image pulls and registry ingress)
  - `mcp.mcpruntime.org` (default ingress host for `MCPServer` when you use host-based routing)
  - `platform.mcpruntime.org` (dashboard / admin UI — the primary user-facing entrypoint)
- **Expected public URLs (after DNS and TLS):**
  - Dashboard UI: `https://platform.mcpruntime.org/` (also serves `/api`, `/grafana`, `/prometheus` under the same host so users do not need raw IPs / dev path-based routing)
  - Registry: `https://registry.mcpruntime.org` (or HTTP before TLS, depending on overlay)
  - Each MCP server (default `IngressPath` is `/{metadata.name}/mcp`): e.g. `https://mcp.mcpruntime.org/demo-one/mcp` for a server named `demo-one` in the default shape
- **Let’s Encrypt and DNS:** the setup TLS flow requests a single `Certificate` whose **SANs** include `registry.<domain>`, `mcp.<domain>`, and `platform.<domain>` whenever those names are in env-derived config. **All three** public DNS A/AAAA (or CNAME) records must exist and point to the **same** public ingress IP (or stable LB). A typo in DNS (e.g. `regsitry` instead of **registry**, or `platfrom` instead of **platform**) will break the matching hostname. Port **80** must hit Traefik for **HTTP-01** before a cert is issued.
- **Run:** `./bin/mcp-runtime setup --with-tls --acme-email <addr>`. You can set `MCP_PLATFORM_DOMAIN` as above, or set `MCP_REGISTRY_INGRESS_HOST` / `MCP_MCP_INGRESS_HOST` / `MCP_PLATFORM_INGRESS_HOST` if you do not use the platform domain (or want to override an individual hostname). Staging: `--acme-staging` / `MCP_ACME_STAGING=1`. The `registry-tls` `Secret` lives in the `registry` namespace; the platform UI cert is provisioned as `mcp-sentinel-platform-tls` in the `mcp-sentinel` namespace. Private CA without ACME: omit `--acme-email` and use the `mcp-runtime-ca` path per `config/cert-manager/`.
- **Internal / enterprise CA:** Install your `ClusterIssuer` first, then: `--with-tls --tls-cluster-issuer <name>` (or `MCP_TLS_CLUSTER_ISSUER`). Setup does not create the issuer; it applies the `Certificate` and waits. Mutually exclusive with `--acme-email`.
- **Operator default host:** `MCP_PLATFORM_DOMAIN` and related env can drive `MCP_DEFAULT_INGRESS_HOST` to `mcp.<domain>` when configured.

## Debugging checklist (common failures)

- **“ingressHost is required” (operator):** set `spec.ingressHost` on the `MCPServer`, or operator env `MCP_DEFAULT_INGRESS_HOST`, or `MCP_PLATFORM_DOMAIN` for `mcp.<domain>` defaults.
- **Port mismatch:** the bundled Go example listens on `8088` by default; align `MCPServer` `port` / `servicePort` and container `PORT` if you overrode them.
- **Analytics 401:** use gateway/ingest URL and key, not the app’s random env. Example: `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events` and `ANALYTICS_API_KEY` from `mcp-sentinel-secrets` (`API_KEYS` key).
- **Secret not found in workload namespace:** copy `mcp-sentinel-secrets` or use a shared secret reference.
- **Dashboard / API 401:** align `API_KEYS` and `UI_API_KEY` and roll the API deployment.
- **Ingress / routes:** `kubectl get ingress -A` and confirm paths match the gateway and demo servers you expect.
- **Private / HTTP in-cluster registry / k3s:** Pull and push can fail with `https` vs `http` or `registry.local` DNS on nodes. See **k3s and HTTP registry (config files)** below, set **`MCP_REGISTRY_*`** before `pipeline generate` when you want `ClusterIP:port` in manifests, and raise **`MCP_DEPLOYMENT_TIMEOUT`** if setup rollouts time out on slow first pulls.
- **Prod DNS / ACME:** with `MCP_PLATFORM_DOMAIN=example.com`, setup derives `registry.example.com`, `mcp.example.com`, and `platform.example.com`. All three public DNS records must point at the ingress IP and port 80 must reach Traefik for HTTP-01. If cert-manager reports NXDOMAIN, verify from outside and inside the cluster: `getent hosts registry.example.com`, `getent hosts mcp.example.com`, `getent hosts platform.example.com`, and `kubectl run dns-check --rm -i --restart=Never --image=busybox:1.36 -- nslookup platform.example.com`.
- **Platform UI 404 / wrong host:** when `MCP_PLATFORM_DOMAIN` (or `MCP_PLATFORM_INGRESS_HOST`) is set, setup applies a host-based ingress `mcp-sentinel-platform-ui` in `mcp-sentinel`. Verify with `kubectl get ingress mcp-sentinel-platform-ui -n mcp-sentinel -o yaml`; the rule should be host=`platform.<domain>` routing `/` to `mcp-sentinel-ui:8082` (and `/api`, `/grafana`, `/prometheus` to those services). If the dashboard returns Traefik default 404, check that DNS resolves `platform.<domain>` to the cluster ingress, then `kubectl logs -n traefik deploy/traefik --tail=120` for routing errors. The dev path-based gateway (`mcp-sentinel-gateway`) keeps working when `MCP_PLATFORM_DOMAIN` is unset.
- **Prod registry 404 / image pulls say “not found”:** if `registry-cert` is Ready but pods fail to pull `registry.<domain>/<repo>:<tag>`, check the public registry route: `curl -k -i https://registry.<domain>/v2/`. Expected is HTTP 200 with `docker-distribution-api-version: registry/2.0`; Traefik `404 page not found` means the ingress/router is not active. Check `kubectl logs -n traefik deploy/traefik --tail=120` and `kubectl get ingress registry -n registry -o yaml`. In prod, the registry ingress must not reference the dev-only `pii-redactor@file` middleware.
- **Prod MCP server URLs:** prefer path-based public routing for clients: `https://mcp.<domain>/<server-name>/mcp`. Use `spec.publicPathPrefix: <server-name>` and set the server’s `MCP_PATH` to `/<server-name>/mcp`; avoid examples that require a custom `Host` header such as `go.example.local`.

### k3s and HTTP registry (dev / test without registry TLS)

**When this section applies**

- **Dev HTTP registry (typical on k3s or lab):** run `./bin/mcp-runtime setup` **without** `--with-tls`. Setup logs `TLS: disabled (dev HTTP mode)` for the internal registry; the registry serves **HTTP**, so you need the **Docker** and **k3s** config in the table below on every machine that builds/pushes or pulls.
- **`--test-mode`:** meant mainly for **Kind** flows. It **skips building and pushing** operator/gateway images and expects images to be **loaded into Kind** already. It does not remove the need for HTTP registry config if other workloads still pull from an in-cluster HTTP registry. On a real k3s cluster you usually run **full setup without `--test-mode`** so images are built and pushed, then you still need **insecure** registry settings for that HTTP registry on Docker and k3s as below.

The platform can install a **plain HTTP** Docker distribution registry (typical in dev with `./bin/mcp-runtime setup` **without** `--with-tls`). Runtimes default to **HTTPS** for any registry; you must allow **HTTP (insecure)** in two places: the host where you run **Docker** (build/push) and every **k3s node** (kubelet/containerd pull for Pods).

| Component | Config file (path) | What to set |
|----------|--------------------|------------|
| **Docker** (laptop, CI, bastion) | **Docker Desktop:** *Settings* → *Docker Engine* (JSON editor) | Add `insecure-registries` (see example below) |
| **Docker** (Linux, `dockerd`) | `/etc/docker/daemon.json` | Same JSON; then restart Docker (below) |
| **k3s** (server and each agent) | `/etc/rancher/k3s/registries.yaml` | Mirror the registry to `http://…` and allow TLS skip for that host (see example); then restart `k3s` / `k3s-agent` on that node |

**1. Docker — `insecure-registries` (push/pull from your workstation)**  
Create or merge into the JSON (use your real registry `host:port` from `kubectl get svc -n registry` or the address you push to, e.g. a node IP and node port, or a LoadBalancer address):

```json
{
  "insecure-registries": ["10.0.0.1:5000", "registry.local:5000"]
}
```

- **macOS/Windows (Docker Desktop):** apply the JSON in **Settings → Docker Engine** → **Apply & restart** (or restart the app).
- **Linux:** write `/etc/docker/daemon.json` (merge with any existing keys such as `log-drivers` if you already have a file), then `sudo systemctl restart docker` (or your distro’s equivalent). Any user running `docker push` that targets this host must have this in effect on that machine.

**2. k3s — `registries.yaml` (Pod image pulls on the node)**  
On **each** k3s node that can schedule Pods that use the registry, edit or create `/etc/rancher/k3s/registries.yaml`. The key under `mirrors` must **match the registry host and port** used in the image name (e.g. if the image is `10.43.109.51:5000/my-app:tag`, the mirror key must be `10.43.109.51:5000`).

```yaml
mirrors:
  "10.43.109.51:5000":
    endpoint:
      - "http://10.43.109.51:5000"
configs:
  "10.43.109.51:5000":
    tls:
      insecure_skip_verify: true
```

- **Control plane:** `sudo systemctl restart k3s`  
- **Agent nodes:** the same `registries.yaml` and `sudo systemctl restart k3s-agent` (paths can differ in air-gapped installs; match your k3s version docs).

If you use the hostname `registry.local` in image refs, add a second `mirrors` / `configs` block for that name. Kubelet uses **node** name resolution, not the cluster’s **CoreDNS**, so for `registry.local` you still need a real DNS name or an entry in **`/etc/hosts` on each node**, unless you set **`MCP_REGISTRY_INGRESS_HOST` to a reachable `IP:port` before** `mcp-runtime pipeline generate` so manifests use a pull address nodes can use without a fake host.

**3. Align with MCP server manifests and CI**  
- **Before** `mcp-runtime pipeline generate`, set **`MCP_REGISTRY_INGRESS_HOST`**, **`MCP_REGISTRY_HOST`**, or **`MCP_PLATFORM_DOMAIN`** (see `pkg/metadata/host_resolve.go`) so default image names are **pullable** from the node, not only `registry.local/...` unless you intend to manage DNS/hosts.  
- **`mcp-runtime server build image <name>`** must match the **`name`** in `.mcp/*.yaml`. Use **`mcp-runtime pipeline generate --dir .mcp --output manifests`** then **`mcp-runtime pipeline deploy --dir manifests`**. A single file: `mcp-runtime server apply --file <path>`. There is no `mcp-runtime server deploy --dir`.

**4. Quick registry reachability (optional)**  
From a network that can reach the registry: `curl -sS "http://<host>:<port>/v2/"` should return `{}`. That does not replace Docker/k3s configuration for TLS mode of the **client** runtimes.

### Production registry and TLS (debugging)

For production with `MCP_PLATFORM_DOMAIN=example.com`, setup derives hostnames `registry.example.com`, `mcp.example.com`, and `platform.example.com`. MCP server base URLs are `https://mcp.example.com/<server_name>/mcp` (default path shape); the dashboard UI is `https://platform.example.com/`. **All three** DNS names must exist publicly and point to your ingress IP.

- **Cert-manager / NXDOMAIN:** if challenges fail with missing DNS, verify from your laptop and from inside the cluster:
  - `getent hosts registry.example.com`, `getent hosts mcp.example.com`, and `getent hosts platform.example.com`
  - `kubectl run dns-check --rm -i --restart=Never --image=busybox:1.36 -- nslookup registry.example.com`
- **Platform UI cert SAN check:** `kubectl get certificate -n registry registry-cert -o yaml` should list all three names under `.spec.dnsNames` (`registry.example.com`, `mcp.example.com`, `platform.example.com`). After issuance, `kubectl get secret registry-tls -n registry -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -A1 "Subject Alternative Name"` should show every SAN.
- **`registry-cert` Ready but image pulls `not found`:** confirm the public registry URL actually routes to the distribution service (not a dead Traefik router). The registry **Ingress** must not reference middleware that is not installed on the Traefik instance serving prod (the base registry ingress does not attach the PII redactor; that middleware exists only in the `config/ingress/overlays/http` dev Traefik stack).
  - `curl -k -i https://registry.example.com/v2/` should return **HTTP 200** and header `docker-distribution-api-version: registry/2.0`. A Traefik **404 page not found** means the registry router is not active (fix ingress annotations or Traefik before debugging registry data).
- **Traefik logs:** `kubectl logs -n traefik deploy/traefik --tail=120` (or k3s Traefik in `kube-system` if you use the bundled service). If you see `middleware "pii-redactor@file" does not exist` on a route that should be the registry, ensure the live **registry** `Ingress` does not set `traefik.ingress.kubernetes.io/router.middlewares: pii-redactor@file` for prod Traefik.
- **Live registry ingress:** `kubectl get ingress registry -n registry -o yaml` and confirm `spec.rules` host and TLS match your domain.
- **Pushed image visibility:** `curl -k -I https://registry.example.com/v2/<repo>/manifests/<tag>` (expect 200 or 404 from the registry, not from Traefik’s default backend).
- **Sentinel rollouts after fixes:** `kubectl rollout status deployment/mcp-sentinel-ingest deployment/mcp-sentinel-api deployment/mcp-sentinel-processor deployment/mcp-sentinel-ui -n mcp-sentinel --timeout=90s`

**Expected when healthy:** `curl -k -i https://registry.<domain>/v2/` returns 200 with `registry/2.0`; `kubectl get certificate registry-cert -n registry -o wide` shows `Ready=True`; mcp-sentinel pods are `1/1` `Running`; `mcp-runtime setup` ends with `Platform setup complete`.

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
