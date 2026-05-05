# AGENTS.md — developer and AI-agent guide

This file is the **onboarding and operations runbook** for the MCP Runtime repo. Humans and coding agents (Cursor, Copilot, Codex, etc.) should use it to **run the right checks**, **find the right code**, and **debug the stack** without re-deriving structure from scratch. It complements `README.md` (product overview) with **workstation commands**, **layout**, and **failure modes**.

If instructions conflict, prefer **this repo** (`README`, CRDs, `v1alpha1` types) over generic Kubernetes or MCP advice.

## Repository map (where to look)

| Area | Path | Notes |
|------|------|--------|
| User-facing CLI | `cmd/mcp-runtime/`, `internal/cli/root/`, `internal/cli/<command>/`, `internal/cli/core/` | Entrypoint, foldered Cobra command routing, command-owned behavior for `setup`, `status`, `registry`, `server`, `access`, …, and shared CLI kernel code |
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
| Agent tool config | `.claude/`, `.codex/skills/` | `.claude/skills` should symlink to `../.codex/skills` so Claude Desktop and the Codex CLI use the same local skills |

**Patterns worth mirroring:** search for similar packages before adding new abstractions; keep CLI errors consistent with `internal/cli/core/errors.go` and `pkg/errx/`.

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

Optional but used in CI: `staticcheck ./...` (install the pinned CI version with `go install honnef.co/go/tools/cmd/staticcheck@v0.7.0`).

**Targeted tests** (prefer these while iterating; full `./...` can be slow):

- `go test ./internal/operator/... ./internal/cli/... -race -count=1`
- `go test ./test/golden/... -count=1` (CLI help snapshots; update `test/golden/cli/testdata/*.golden` when you change Cobra help text on purpose)
- `go test ./test/integration/...` (needs `KUBEBUILDER_ASSETS`; see `Makefile.operator` and CI for envtest setup)
- `E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh` for repeated local Kind e2e debugging without recreating the cluster or rebuilding cached images; set `OPENAI_API_KEY` in env or `.env` for optional real-client prompts, and omit cache mode for CI-equivalent fresh runs.
- `services/api` and `services/ui`: `go test -race -count=1 ./...` inside each directory (CI runs these explicitly)

**CI** (`.github/workflows/ci.yaml`) runs: `gofmt` check, `go vet`, `staticcheck`, unit tests, golden tests, service tests, `test/integration`, SBOM generation, then Kind e2e on `main`/`PR` branches. Normal PRs, `main`, and manual runs first validate that `OPENAI_API_KEY` is accepted by OpenAI; if validation fails, Kind e2e is skipped with a warning. Those runs use `E2E_SCENARIOS=all` when the key validates. Dependabot PRs use `E2E_SCENARIOS=smoke-auth,governance` so dependency bumps still cover MCP ingress/auth and grant/session behavior without provider API secrets. Security workflows add pinned gosec, Trivy, and dependency-review checks. Align local changes with that before opening a PR.

**Docs sync for CLI help:** when you edit `docs/cli.md`, `docs/getting-started.md`, `docs/publish-mcp-server.md`, or any page that shows CLI commands, verify the exact command description, subcommands, flags, and defaults from live help output before push. Use:

```bash
./bin/mcp-runtime --help
./bin/mcp-runtime <group> --help
./bin/mcp-runtime <group> <subcommand> --help
```

Do not hand-wave command behavior from memory when the docs are meant to reflect Cobra help text. Agents should copy the real wording or update prose/examples to match the live help output for the commands they touched.

## Conventions for code changes

- **Scope:** Change only what the task needs; do not “clean up” unrelated files. Match naming and patterns in the nearest similar code.
- **Tests:** Add or adjust tests in the same package when behavior changes. For CLI output, expect golden file updates.
- **Branch names:** Use `component/feature_name` for task branches. Pick the component from the same scope list used for commit messages, and write the feature name in lowercase snake_case, for example `doc/commit_message_guidance`, `cli/registry_status`, or `operator/ingress_defaults`.
- **Agent branch / PR flow:** Agents must create and push changes on a new branch, and open or update a PR from that branch. Agents must not push directly to `main`.
- **Commit messages:** Use `fix(<component>): ...` for bug fixes and `feat(<component>): ...` for user-facing behavior. Use `doc: ...` for README / AGENTS / docs-only edits, and `website: ...` for `website/` changes. Prefer components that match repo areas, such as `cli`, `operator`, `api`, `crd`, `access`, `policy`, `k8sclient`, `manifest`, `metadata`, `sentinel`, `registry`, `ingress`, `services-api`, `ui`, `ingest`, `processor`, `mcp-proxy`, `traefik-plugin`, `config`, `examples`, `test`, or `ci`. Keep the subject concise and imperative; add a body only when the reason, risk, or verification needs context.
- **Docs you were not asked to edit:** Avoid adding new top-level docs unless the task needs them; this file, `README`, and existing doc trees are the defaults for agents.
- **Secrets and prod:** This repo is **alpha**; do not hardcode real credentials. Use the existing secret and env patterns documented below.
- **Agent skills:** Keep `.claude/skills` as a symlink to `../.codex/skills`; see `.claude/README.md` before changing local agent-tool configuration.

## Local dev setup (Kind and CLI)

- **Prereqs:** Docker, Kind, `kubectl`, `curl`, `jq`, Python 3; Go for building the CLI.
- **Quick start:**

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
./bin/mcp-runtime bootstrap                              # preflight cluster prerequisites
MCP_SETUP_WAIT_TIMEOUT=900 ./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http
./bin/mcp-runtime cluster doctor                         # post-install registry/component diagnostics
kubectl port-forward -n traefik svc/traefik 18080:8000   # expose ingress
```

`setup --test-mode` is not a no-build path: it builds and pushes the operator,
gateway proxy, and Sentinel images with `latest` tags to the configured or
bundled registry, then deploys pods that pull those images. In Kind test mode,
implicit internal image refs use `registry.registry.svc.cluster.local:5000/...`
so the documented containerd mirror matches the image host exactly.

- **Status:** `./bin/mcp-runtime status`
- **Contributor smoke:** for dashboard access, local image push, MCP JSON-RPC request, and Sentinel event checks, follow `docs/getting-started.md#3-contributor-test-mode-cluster`.
- **Preflight only (no apply):** `./bin/mcp-runtime bootstrap`. For k3s: add `--apply --provider k3s` to install bundled CoreDNS / local-path manifests (server node only).

## Endpoints and auth

- UI: `http://localhost:18080/`
- Grafana: `/grafana` · Prometheus: `/prometheus` · API base: `http://localhost:18080/api`
- MCP (test): `http://localhost:18080/demo-one/mcp`, `http://localhost:18080/demo-two/mcp`
- PII redaction: `config/ingress/overlays/http` with Traefik plugin `pii-redactor@file`. Reapply: `./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http`. The plugin is built from `services/traefik-plugins/pii-redactor` (local `localplugins` mount) so a published image tag is not required for local dev.
- **API keys:**

```bash
# Direct Sentinel API / x-api-key admin requests.
kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d

# Ingest/proxy analytics requests.
kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.INGEST_API_KEYS}' | base64 -d

# Browser/API-key login through the UI.
kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d
```

  `setup` should keep `UI_API_KEY` included in the comma-separated `API_KEYS`
  and `ADMIN_API_KEYS` lists, and should keep ingest-only keys in
  `INGEST_API_KEYS`. If direct `/api/...` curl calls return `401`, run
  `./bin/mcp-runtime cluster doctor`; it flags UI/API/admin key mismatches. Roll
  the API, UI, ingest, and proxy/gateway workloads after patching
  `mcp-sentinel-secrets`.

- **Platform admin bootstrap (one-shot):**

```bash
kubectl apply -f k8s/21-platform-admin-bootstrap-job.yaml
kubectl wait --for=condition=complete job/mcp-sentinel-platform-admin-bootstrap -n mcp-sentinel --timeout=120s
kubectl patch secret mcp-sentinel-secrets -n mcp-sentinel --type merge -p '{"stringData":{"PLATFORM_ADMIN_PASSWORD":""}}'
```

  `PLATFORM_ADMIN_PASSWORD` is bootstrap-only; the API deployment should not keep it in steady-state environment variables.

### Platform domain and TLS (short)

- **What `MCP_PLATFORM_DOMAIN` does:** with `export MCP_PLATFORM_DOMAIN=mcpruntime.org` (apex only, no `https://`), the CLI/operator resolve **registry**, **MCP**, and **platform** hostnames as:
  - `registry.mcpruntime.org` (image pulls and registry ingress)
  - `mcp.mcpruntime.org` (default ingress host for `MCPServer` when you use host-based routing)
  - `platform.mcpruntime.org` (dashboard / admin UI — the primary user-facing entrypoint)
- **Expected public URLs (after DNS and TLS):**
  - Dashboard UI: `https://platform.mcpruntime.org/` (also serves `/api` under the same host). `/grafana` and `/prometheus` are intentionally **not** exposed on the public platform host — those tools have no built-in auth in this stack. Reach them with `kubectl port-forward -n mcp-sentinel svc/grafana 3000:3000` / `svc/prometheus 9090:9090`, or front them with your own auth-aware ingress.
  - Registry: `https://registry.mcpruntime.org` (or HTTP before TLS, depending on overlay)
  - Each MCP server (default `IngressPath` is `/{metadata.name}/mcp`): e.g. `https://mcp.mcpruntime.org/demo-one/mcp` for a server named `demo-one` in the default shape
- **Let’s Encrypt and DNS:** the setup TLS flow requests `registry/registry-cert` for `registry.<domain>` and `mcp.<domain>` when those names are in env-derived config. `platform.<domain>` is separate: the `mcp-sentinel-platform-ui` Ingress in `mcp-sentinel` asks cert-manager to write `mcp-sentinel-platform-tls`. **All three** public DNS A/AAAA (or CNAME) records must exist and point to the **same** public ingress IP (or stable LB). A typo in DNS (e.g. `regsitry` instead of **registry**, or `platfrom` instead of **platform**) will break the matching hostname. Port **80** must hit Traefik for **HTTP-01** before certs are issued.
- **Run:** `./bin/mcp-runtime setup --with-tls --acme-email <addr>`. You can set `MCP_PLATFORM_DOMAIN` as above, or set `MCP_REGISTRY_INGRESS_HOST` / `MCP_MCP_INGRESS_HOST` / `MCP_PLATFORM_INGRESS_HOST` if you do not use the platform domain (or want to override an individual hostname). Staging: `--acme-staging` / `MCP_ACME_STAGING=1`. The `registry-tls` `Secret` lives in the `registry` namespace; the platform UI cert is provisioned as `mcp-sentinel-platform-tls` in the `mcp-sentinel` namespace. Private CA without ACME: omit `--acme-email` and use the `mcp-runtime-ca` path per `config/cert-manager/`.
- **Internal / enterprise CA:** Install your `ClusterIssuer` first, then: `--with-tls --tls-cluster-issuer <name>` (or `MCP_TLS_CLUSTER_ISSUER`). Setup does not create the issuer; it applies the `Certificate` and waits. Mutually exclusive with `--acme-email`.
- **Operator default host:** `MCP_PLATFORM_DOMAIN` and related env can drive `MCP_DEFAULT_INGRESS_HOST` to `mcp.<domain>` when configured.

## Debugging checklist (common failures)

- **“ingressHost is required” (operator):** set `spec.ingressHost` on the `MCPServer`, or operator env `MCP_DEFAULT_INGRESS_HOST`, or `MCP_PLATFORM_DOMAIN` for `mcp.<domain>` defaults.
- **MCPServer stuck `PartiallyReady` with working ingress traffic:** default ingress readiness is strict and waits for `Ingress.status.loadBalancer.ingress[]`. For dev / NodePort-style ingress controllers that route without publishing LB status, set operator env `MCP_INGRESS_READINESS_MODE=permissive`; this treats an Ingress with rules as ready. Keep the default `strict` mode for production setups that rely on published LB status.
- **Port mismatch:** the bundled Go example listens on `8088` by default; align `MCPServer` `port` / `servicePort` and container `PORT` if you overrode them.
- **Analytics 401:** use gateway/ingest URL and key, not the app’s random env. Example: `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events` and `ANALYTICS_API_KEY` from `mcp-sentinel-secrets` (`INGEST_API_KEYS` key).
- **Secret not found in workload namespace:** copy `mcp-sentinel-secrets` or use a shared secret reference.
- **Dashboard / API 401:** direct admin `x-api-key` curl calls need a key present in both `API_KEYS` and `ADMIN_API_KEYS`; browser login uses `UI_API_KEY`. Keep `UI_API_KEY` present in both lists, then roll the API and UI deployments after secret changes.
- **Dashboard 308 redirect loop in dev:** the UI service redirects HTTP→HTTPS for non-local hosts when it sees `X-Forwarded-Proto: http`. Override with the `UI_REQUIRE_HTTPS` env on the `mcp-sentinel-ui` deployment: `auto` (default — redirect public hosts only), `true`/`on`/`1`/`yes` (always redirect HTTP for public hosts), `false`/`off`/`0`/`no` (never redirect, use this when the UI is fronted by a non-TLS terminator on a real hostname).
- **Ingress / routes:** `kubectl get ingress -A` and confirm paths match the gateway and demo servers you expect.
- **Custom ingress namespaces:** bundled Traefik manifests watch `registry`, `mcp-sentinel`, and `mcp-servers` only so the controller does not need cluster-wide Secret access. If you place public Ingresses in another namespace, add that namespace to the Traefik `--providers.kubernetesingress.namespaces` list and bind the `traefik-watch` Role there.
- **Private / HTTP in-cluster registry / k3s:** Pull and push can fail with `https` vs `http` or `registry.local` DNS on nodes. See **k3s and HTTP registry (config files)** below, set **`MCP_REGISTRY_*`** before `pipeline generate` when you want `ClusterIP:port` in manifests, and raise **`MCP_DEPLOYMENT_TIMEOUT`** if setup rollouts time out on slow first pulls.
- **Prod DNS / ACME:** with `MCP_PLATFORM_DOMAIN=example.com`, setup derives `registry.example.com`, `mcp.example.com`, and `platform.example.com`. All three public DNS records must point at the ingress IP and port 80 must reach Traefik for HTTP-01. If cert-manager reports NXDOMAIN, verify from outside and inside the cluster: `getent hosts registry.example.com`, `getent hosts mcp.example.com`, `getent hosts platform.example.com`, and `kubectl run dns-check --rm -i --restart=Never --image=busybox:1.36 -- nslookup platform.example.com`.
- **Platform UI 404 / wrong host:** when `MCP_PLATFORM_DOMAIN` (or `MCP_PLATFORM_INGRESS_HOST`) is set, setup applies a host-based ingress `mcp-sentinel-platform-ui` in `mcp-sentinel` (and, when TLS is enabled, a sibling `mcp-sentinel-platform-ui-http` for HTTP→HTTPS redirect). Verify with `kubectl get ingress mcp-sentinel-platform-ui -n mcp-sentinel -o yaml`; the rule should be host=`platform.<domain>` routing `/` to `mcp-sentinel-ui:8082` (and `/api` to the same service). `/grafana` and `/prometheus` are deliberately not on the public host. If the dashboard returns Traefik default 404, check that DNS resolves `platform.<domain>` to the cluster ingress, then `kubectl logs -n traefik deploy/traefik --tail=120` for routing errors. The dev path-based gateway (`mcp-sentinel-gateway`) keeps working when `MCP_PLATFORM_DOMAIN` is unset.
- **Prod registry 404 / image pulls say “not found”:** if `registry-cert` is Ready but pods fail to pull `registry.<domain>/<repo>:<tag>`, check the public registry route: `curl -k -i https://registry.<domain>/v2/`. Expected is HTTP 200 with `docker-distribution-api-version: registry/2.0`; Traefik `404 page not found` means the ingress/router is not active. Check `kubectl logs -n traefik deploy/traefik --tail=120` and `kubectl get ingress registry -n registry -o yaml`. In prod, the registry ingress must not reference the dev-only `pii-redactor@file` middleware.
- **Prod MCP server URLs:** prefer path-based public routing for clients: `https://mcp.<domain>/<server-name>/mcp`. Use `spec.publicPathPrefix: <server-name>` and set the server’s `MCP_PATH` to `/<server-name>/mcp`; avoid examples that require a custom `Host` header such as `go.example.local`.

### MCP server pod / sidecar checks

Use these when a server is deployed but gateway behavior, grants, or analytics
look wrong:

```bash
SERVER=go-example-mcp
CONTAINER=go-example-mcp

kubectl get mcpservers -n mcp-servers
kubectl get pods -n mcp-servers -o wide
kubectl get pods -n mcp-servers \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.name}{","}{end}{"\n"}{end}'

POD="$(kubectl get pods -n mcp-servers -l app="$SERVER" -o jsonpath='{.items[0].metadata.name}')"
kubectl describe pod -n mcp-servers "$POD"
kubectl logs -n mcp-servers "$POD" -c "$CONTAINER"
kubectl logs -n mcp-servers "$POD" -c mcp-gateway
./bin/mcp-runtime server policy inspect "$SERVER" --namespace mcp-servers
kubectl get mcpaccessgrant,mcpagentsession -n mcp-servers -o wide
```

The sidecar container is named `mcp-gateway`; it runs the `mcp-proxy`
image/process. Many runtime images are distroless, so `/bin/sh` and
`/bin/bash` may not exist. Prefer logs/describe, or attach a debug container:

```bash
kubectl debug -it -n mcp-servers "pod/$POD" \
  --target="$CONTAINER" \
  --image=busybox:1.36 -- sh
```

### k3s and HTTP registry (dev / test without registry TLS)

**When this section applies**

- **Dev HTTP registry (typical on k3s or lab):** run `./bin/mcp-runtime setup` **without** `--with-tls`. Setup logs `TLS: disabled (dev HTTP mode)` for the internal registry; the registry serves **HTTP**, so you need the **Docker** and **k3s** config in the table below on every machine that builds/pushes or pulls.
- **`--test-mode`:** meant mainly for **Kind** contributor and CI flows. It keeps production guardrails off, uses local/dev registry behavior, and still builds and pushes operator, gateway proxy, and Sentinel images with `latest` tags. In Kind, implicit bundled-registry image refs use `registry.registry.svc.cluster.local:5000/...` to match the documented mirror. On k3s, pods still pull from the exact configured or discovered image host, such as `10.43.x.x:5000`, so k3s/containerd needs a matching insecure HTTP mirror when using the bundled plain HTTP registry. On k3s hosts with an empty/minimal `~/.kube/config`, pass `--kubeconfig /etc/rancher/k3s/k3s.yaml`.
- **Bundled registry on k3s:** setup prints the selected registry **Internal URL** after creating the registry Service. If you did not preconfigure a stable registry endpoint, copy that exact `host:port` into `/etc/rancher/k3s/registries.yaml`, restart `k3s` / `k3s-agent`, then rerun setup. If StatefulSet storage was interrupted during a failed run, clear the partial runtime namespaces first.

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
- **TLS cert SAN checks:** `kubectl get certificate -n registry registry-cert -o yaml` should list `registry.example.com` and `mcp.example.com` under `.spec.dnsNames`; `platform.example.com` should be on the `mcp-sentinel-platform-ui` ingress TLS config in `mcp-sentinel`. After issuance, inspect both secrets:
  - `kubectl get secret registry-tls -n registry -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -A1 "Subject Alternative Name"`
  - `kubectl get secret mcp-sentinel-platform-tls -n mcp-sentinel -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -A1 "Subject Alternative Name"`
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
     -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' $BASE
# then
curl -i -H "content-type: application/json" \
     -H "accept: application/json, text/event-stream" \
     -H "Mcp-Protocol-Version: $PROTO" \
     -H "Mcp-Session-Id: <session>" \
     -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}' $BASE
```

If you just applied `MCPAccessGrant` or `MCPAgentSession` resources, remember
that `server policy inspect` only confirms the rendered policy. The proxy
sidecar reloads its local policy file on a short polling loop, so allow a few
seconds before concluding a fresh session-backed request failed with
`session_not_found`.

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
