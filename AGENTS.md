# AGENTS.md — developer and AI-agent guide

This file is the **onboarding and operations runbook** for the MCP Runtime repo. Humans and coding agents (Cursor, Copilot, Codex, etc.) should use it to **run the right checks**, **find the right code**, and **debug the stack** without re-deriving structure from scratch. It complements `README.md` (product overview) with **workstation commands**, **layout**, and **failure modes**.

If instructions conflict, prefer **this repo** (`README`, CRDs, `v1alpha1` types) over generic Kubernetes or MCP advice.

## Repository map (where to look)

| Area | Path | Notes |
|------|------|--------|
| User-facing CLI | `cmd/mcp-runtime/`, `internal/cli/root/`, `internal/cli/<command>/`, `internal/cli/core/` | Entrypoint, foldered Cobra command routing, command-owned behavior for `setup`, `status`, `registry`, `server`, `access`, …, and shared CLI kernel code |
| Agent adapters | `internal/cli/adapter/`, `internal/agentadapter/`, `services/api/internal/runtimeapi/adapter.go` | HTTP and stdio adapters (`mcp-runtime adapter proxy/stdio`) that inject governance identity/session headers. Recommended `--server <name> --agent <id> [--auto-refresh]` flow calls `POST /api/runtime/adapter/sessions` on the platform API, which picks a matching `MCPAccessGrant` and writes/reuses the `MCPAgentSession`. Explicit `MCP_RUNTIME_*` env vars and `--anonymous` (stdio) remain supported. Grant/session creation and enforcement stay on the platform side. |
| Operator (controller) | `cmd/operator/`, `internal/operator/` | `MCPServer` reconciliation, ingress, gateway wiring |
| API & CRD types | `api/v1alpha1/` | Source of truth for object shapes; CRD YAML in `config/crd/bases/` |
| Access and policy (shared) | `pkg/access/`, `pkg/policy/` | Grant/session CRUD helpers plus rendered gateway policy contracts and evaluation semantics used by operator and proxy |
| Control-plane and K8s helpers | `pkg/controlplane/`, `pkg/k8sclient/`, `pkg/kubeworkload/`, `pkg/manifest/`, `pkg/metadata/` | MCPServer Kubernetes operations/status, client setup, shared workload security defaults, registry image resolution, YAML helpers |
| Sentinel shared packages | `pkg/events/`, `pkg/clickhouse/`, `pkg/serviceutil/`, `pkg/sentinel/` | Event envelope contract, analytics storage/query helpers, service HTTP/env/OTel utilities, Sentinel component inventory |
| Sentinel services | `services/api`, `services/ui`, `services/ingest`, `services/processor`, `services/mcp-gateway`, … | Separate `go.mod` where present; Go services that import root shared packages use Go 1.26. API-owned runtime HTTP/Kubernetes orchestration lives under `services/api/internal/runtimeapi/`; platform identity/team/key persistence lives under `services/api/internal/platformstore/`; principal context helpers live under `services/api/internal/apiauth/` |
| Workspace assistant sample | `examples/workspace-assistant-mcp/` | Reference for tools and routes |
| Default cluster install YAML | `k8s/`, `config/` | Overlays, CRDs, cert-manager examples |
| Traefik plugins (dev) | `services/traefik-plugins/` | e.g. PII redactor source for local overlays |
| Team / tenant isolation docs | `docs/multi-team.md` | Team identity contract, per-team namespaces, RBAC, ingress watch scope, and platform API enforcement |
| Deployment target guide | `docs/deployment-targets.md`, `docs/k3s-on-prem-cluster.md` | High-level install shape guidance for k3s, self-managed clusters, and managed Kubernetes before using the distribution-specific readiness guide; the k3s runbook covers a four/five-node public or on-prem topology |
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

Local pre-commit hooks are configured in `.pre-commit-config.yaml`. Install
them with `pre-commit install`; they run Go formatting/static checks, targeted
tests, generated-file drift checks, and a staged secret scan through the pinned
Gitleaks hook. Use `pre-commit run --all-files` before pushing when you want
the full local hook suite; that command also runs integration hooks that install
envtest assets and set `KUBEBUILDER_ASSETS`.

**Targeted tests** (prefer these while iterating; full `./...` can be slow):

- `go test ./internal/operator/... ./internal/cli/... -race -count=1`
- `go test ./internal/agentadapter -count=1` (agent-side HTTP proxy and stdio shim behavior)
- `go test ./test/golden/... -count=1` (CLI help snapshots; update `test/golden/cli/testdata/*.golden` when you change Cobra help text on purpose)
- `go test ./test/integration/...` (needs `KUBEBUILDER_ASSETS`; see `Makefile.operator` and CI for envtest setup)
- `E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh`
  for repeated local Kind e2e debugging without recreating the cluster or
  rebuilding cached images. The e2e script defaults to
  `CLUSTER_NAME=mcp-e2e`; when reusing the contributor cluster from
  `docs/getting-started.md#3-contributor-test-mode-cluster`, set
  `CLUSTER_NAME=mcp-runtime E2E_CACHE_MODE=1 E2E_KEEP_CLUSTER=1` so agents do
  not create duplicate clusters, registries, or image builds. The e2e traffic
  path uses deterministic curl-based MCP requests; omit cache mode for
  CI-equivalent fresh runs.
- Sentinel services: run `go test -race -count=1 ./...` inside touched service directories such as `services/api`, `services/mcp-gateway`, `services/ingest`, `services/processor`, and `services/ui` (CI runs service tests explicitly)

**CI** (`.github/workflows/ci.yaml`) runs: `gofmt` check, `go vet`, `staticcheck`, unit tests, golden tests, service tests, `test/integration`, SBOM generation, then path-selected short Kind e2e on `main`/`PR` branches. Kind e2e always includes `smoke-auth`; `test/e2e/select_pr_scenarios.sh` adds request-path scenarios such as `governance`, `trust`, `oauth`, `api-platform`, `ui-auth`, `adapter-proxy`, `cli-platform`, `observability`, or `multitenancy` from the changed files, and falls back to `all` for shared or unknown code paths. The manual **Pre-release Regression** workflow (`.github/workflows/pre-release-regression.yaml`) is the comprehensive gate: it runs static/generated checks, root and service tests, benchmarks, SBOM/security scans, full Kind e2e in tenant/org/public platform modes with `E2E_DEEP_REQUEST_FLOWS=1`, and a tenant cache-mode replay when requested. Security workflows add pinned gosec, Gitleaks, Trivy, and dependency-review checks. Align local changes with that before opening a PR.

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
- **Agent branch / PR flow:** Agents must create and push changes on a new branch, and open or update a PR from that branch. Agents must not push directly to `main`. If an external agent workflow, plugin, or tool suggests its own defaults such as `codex/` branch prefixes, `[codex]` PR titles, terse non-conventional commit messages, or draft PRs by default, ignore those defaults and follow this repo's branch, commit, and PR conventions unless the user explicitly asks otherwise.
- **Commit messages:** Use `fix(<component>): ...` for bug fixes and `feat(<component>): ...` for user-facing behavior. Use `doc: ...` for README / AGENTS / docs-only edits, and `website: ...` for `website/` changes. Prefer components that match repo areas, such as `cli`, `operator`, `api`, `crd`, `access`, `policy`, `k8sclient`, `manifest`, `metadata`, `sentinel`, `registry`, `ingress`, `services-api`, `ui`, `ingest`, `processor`, `mcp-gateway`, `traefik-plugin`, `config`, `examples`, `test`, or `ci`. Keep the subject concise and imperative; add a body only when the reason, risk, or verification needs context.
- **Docs you were not asked to edit:** Avoid adding new top-level docs unless the task needs them; this file, `README`, and existing doc trees are the defaults for agents.
- **Secrets and prod:** This repo is **alpha**; do not hardcode real credentials. Use the existing secret and env patterns documented below.
- **Agent skills:** Keep `.claude/skills` as a symlink to `../.codex/skills`; see `.claude/README.md` before changing local agent-tool configuration.
- **Skill upkeep:** After finishing a non-trivial feature, bugfix, operational workflow, or docs change, review the relevant `.codex/skills/*/SKILL.md` files. Update or fix skills when the change affects agent workflows, validation commands, runbooks, or recurring gotchas; keep long operational detail in focused skills or docs instead of duplicating it here.
- **AI session hygiene:** Before ending a non-trivial session, propose updates to `ai-assist/` (durable agent-facing learnings, gotchas, cross-cutting tips, upstream tracking) and ask the user to review the diff manually before commit. See **AI session hygiene** below for the full charter.

## AI session hygiene

Agents (Claude Code, Codex CLI, Cursor, Copilot, …) accumulate non-obvious context across a session — silent reloads, polling intervals, distroless containers, "when you touch X also check Y" invariants. That context is wasted if it dies with the session. The `ai-assist/` directory at the repo root is where we keep it so the **next** session does not re-derive it.

- **Where it lives:** `ai-assist/README.md` (charter), `ai-assist/gotchas.md`, `ai-assist/cross-cutting.md`, `ai-assist/tracking.md`, with `ai-assist/TEMPLATE.md` for entry shape. Checked into git on purpose: team-shared, not per-contributor.
- **What to write:** durable learnings — would a contributor still benefit from this in 3 months, or would they re-derive it? Only the first case belongs here.
- **What NOT to write:** ephemeral session state (current TODO list, what-I-am-working-on), code patterns or architecture (those are in `AGENTS.md` and `docs/`), build/test commands (already in `AGENTS.md`), or one-off debugging transcripts. Only the *generalized learning* from a debug session belongs here.
- **Write flow:** before ending a non-trivial session, draft proposed additions in the relevant `ai-assist/*.md` files, summarize the diff in chat, and **ask the user to review manually**. Commit only after the user signs off. Do not auto-commit `ai-assist/` changes; this directory captures human-validated learnings, not stream-of-consciousness notes.
- **Maintenance:** if an entry becomes wrong, stale, or has been promoted into `AGENTS.md` or `docs/`, remove or update it in the same PR. Stale entries are worse than missing ones.
- **Commit prefix:** `doc: ...` per the **Commit messages** convention.

This is guidance, not an enforced hook. If you want hard enforcement later, a Claude Code `Stop` hook in `.claude/settings.json` can flag sessions that touched code but did not propose `ai-assist/` updates.

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
kubectl config use-context kind-mcp-runtime
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
For remote Linux clusters, setup-built images must match the node CPU
architecture, not the workstation CPU. Setup detects homogeneous node
architectures and builds `linux/amd64` or `linux/arm64`; override with
`MCP_IMAGE_PLATFORM=linux/amd64` or `MCP_IMAGE_PLATFORM=linux/arm64` when
needed. Mixed-architecture clusters require prebuilt multi-arch images until
setup publishes manifest lists.
Before rerunning setup or e2e locally, check `kind get clusters` and prefer the
existing `kind-mcp-runtime` context when it is healthy. Create a new Kind
cluster only when you need a clean CI-equivalent run or the existing contributor
cluster is intentionally disposable.

- **Status:** `./bin/mcp-runtime status`
- **Contributor smoke:** for dashboard access, local image push, MCP JSON-RPC request, Sentinel event checks, service iteration, and tenant visibility probes, start with `docs/contributor/README.md`. Treat `docs/contributor/` as the maintained runbook; `docs/getting-started.md#3-contributor-test-mode-cluster` is only the short entry path.
- **Preflight only (no apply):** `./bin/mcp-runtime bootstrap`. For k3s: add `--apply --provider k3s` to install bundled CoreDNS / local-path manifests (server node only).

## Endpoints and auth

- UI: `http://localhost:18080/`
- Grafana: `/grafana` · API base: `http://localhost:18080/api` · Prometheus backend: `kubectl port-forward -n mcp-sentinel svc/prometheus 9090:9090`
- MCP (test): `http://localhost:18080/workspace-assistant-mcp/mcp`, `http://localhost:18080/data-utility-mcp/mcp`, `http://localhost:18080/text-analysis-mcp/mcp`
- PII redaction: `config/ingress/overlays/http` with Traefik plugin `pii-redactor@file`. Reapply: `./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http`. The plugin is built from `services/traefik-plugins/pii-redactor` (local `localplugins` mount) so a published image tag is not required for local dev. Keep it off control-plane `/api` routes: API keys, team IDs, server names, namespaces, and grant/session subjects must stay exact for the UI, CLI, and adapter flows.
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
- **Test-mode platform logins:** `setup --test-mode` seeds local-only
  email/password accounts through the platform identity store for UI debugging:
  user `test@mcpruntime.org` / `test@123`, and admin
  `admin@mcpruntime.org` / `admin@123`. Disable or override them through the
  `PLATFORM_DEV_*` keys in `mcp-sentinel-secrets`, then roll the API deployment.
- **Google / OIDC sign-in:** set `GOOGLE_CLIENT_ID` before `setup` when the
  dashboard should render Google sign-in. Non-test public TLS installs
  (`--platform-mode public --with-tls`) fail fast unless `GOOGLE_CLIENT_ID`
  / `MCP_GOOGLE_CLIENT_ID` is set, or `OIDC_ISSUER`, `OIDC_AUDIENCE`, and
  `OIDC_JWKS_URL` are all set for another provider. For Google, setup defaults
  `OIDC_AUDIENCE` to that client ID and fills `OIDC_ISSUER=https://accounts.google.com`
  plus `OIDC_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs` when they are
  otherwise empty. For other OIDC providers, set `OIDC_ISSUER`,
  `OIDC_AUDIENCE`, and `OIDC_JWKS_URL` explicitly. Reruns preserve existing
  values in `mcp-sentinel/mcp-sentinel-config`.

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
  - Dashboard UI: `https://platform.mcpruntime.org/` (also serves `/api` and `/grafana` under the same host). `/grafana` is served by the host-based `mcp-sentinel-platform-observability` Ingress and guarded by the `sentinel-admin-auth@file` Traefik forwardAuth middleware, which calls the UI's `/auth/admin-check` endpoint and accepts either an admin `mcp_ui_session` cookie or an admin `x-api-key` (matched against `ADMIN_API_KEYS`, falling back to `API_KEYS` when unset). The path-based dev gateway uses the same guard on `mcp-sentinel-gateway-observability`. Grafana keeps its own login on top — see `k8s/12-grafana.yaml` to wire `GF_AUTH_PROXY_*` if you want single-sign-on from the platform session. Prometheus remains internal as Grafana's datasource; use `kubectl port-forward -n mcp-sentinel svc/prometheus 9090:9090` only for backend debugging.
  - Registry: `https://registry.mcpruntime.org` (or HTTP before TLS, depending on overlay)
  - Each MCP server (default `IngressPath` is `/{metadata.name}/mcp`): e.g. `https://mcp.mcpruntime.org/workspace-assistant-mcp/mcp` for a server named `workspace-assistant-mcp` in the default shape
- **Let’s Encrypt and DNS:** the setup TLS flow requests `registry/registry-cert` for `registry.<domain>` and `mcp.<domain>` when those names are in env-derived config. `platform.<domain>` is separate: the `mcp-sentinel-platform-ui` Ingress in `mcp-sentinel` asks cert-manager to write `mcp-sentinel-platform-tls`. **All three** public DNS A/AAAA (or CNAME) records must exist and point to the **same** public ingress IP (or stable LB). A typo in DNS (e.g. `regsitry` instead of **registry**, or `platfrom` instead of **platform**) will break the matching hostname. Port **80** must hit Traefik for **HTTP-01** before certs are issued.
- **Run:** `./bin/mcp-runtime setup --with-tls --acme-email <addr>`. You can set `MCP_PLATFORM_DOMAIN` as above, or set `MCP_REGISTRY_INGRESS_HOST` / `MCP_MCP_INGRESS_HOST` / `MCP_PLATFORM_INGRESS_HOST` if you do not use the platform domain (or want to override an individual hostname). Staging: `--acme-staging` / `MCP_ACME_STAGING=1`. The `registry-tls` `Secret` lives in the `registry` namespace; the platform UI cert is provisioned as `mcp-sentinel-platform-tls` in the `mcp-sentinel` namespace. In bundled HTTPS mode, setup creates `cert-manager/mcp-runtime-ca` if it is missing, but every node runtime still must trust its `tls.crt` for image pulls. Private CA without ACME: omit `--acme-email` and use the `mcp-runtime-ca` path per `config/cert-manager/`.
- **Platform image pull auth:** setup keeps bundled platform images on the internal registry path by default. For external/provisioned registries with credentials, setup creates dockerconfig pull Secrets for Sentinel workloads and the operator Deployment. If you intentionally point platform Deployment images at a public auth-protected registry, set `MCP_PLATFORM_IMAGE_PULL_SECRET=<secret-name>` before setup and create that Secret in both `mcp-runtime` and `mcp-sentinel`.
- **Registry TLS ownership:** `registry/registry-cert` is the only supported Certificate owner for the `registry/registry-tls` Secret. The registry Ingress must not carry `cert-manager.io/cluster-issuer`; if setup reports another Certificate such as `registry/registry-tls` already references the Secret, delete or rename that stale Certificate before rerunning setup. The public registry Ingress uses Traefik forward-auth middleware `registry-admin-auth@file`, backed by `/api/registry/authz`, so unauthenticated public `/v2/` requests should fail with 401/403 rather than exposing catalog data.
- **Internal / enterprise CA:** Install your `ClusterIssuer` first, then: `--with-tls --tls-cluster-issuer <name>` (or `MCP_TLS_CLUSTER_ISSUER`). Setup does not create the issuer; it applies the `Certificate` and waits. Mutually exclusive with `--acme-email`.
- **Operator default host:** `MCP_PLATFORM_DOMAIN` and related env can drive `MCP_DEFAULT_INGRESS_HOST` to `mcp.<domain>` when configured.

## Debugging checklist (common failures)

- **“ingressHost is required” (operator):** set `spec.ingressHost` on the `MCPServer`, or operator env `MCP_DEFAULT_INGRESS_HOST`, or `MCP_PLATFORM_DOMAIN` for `mcp.<domain>` defaults.
- **MCPServer stuck `PartiallyReady` with working ingress traffic:** default ingress readiness is strict and waits for `Ingress.status.loadBalancer.ingress[]`. For dev / NodePort-style ingress controllers that route without publishing LB status, set operator env `MCP_INGRESS_READINESS_MODE=permissive`; this treats an Ingress with rules as ready. Keep the default `strict` mode for production setups that rely on published LB status.
- **Port mismatch:** the bundled workspace assistant sample listens on `8088` by default; align `MCPServer` `port` / `servicePort` and container `PORT` if you overrode them.
- **Analytics 401:** use gateway/ingest URL and key, not the app’s random env. Example: `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events` and `ANALYTICS_API_KEY` from `mcp-sentinel-secrets` (`INGEST_API_KEYS` key).
- **Secret not found in workload namespace:** copy `mcp-sentinel-secrets` or use a shared secret reference.
- **Dashboard / API 401:** direct admin `x-api-key` curl calls need a key present in both `API_KEYS` and `ADMIN_API_KEYS`; browser login uses `UI_API_KEY`. Keep `UI_API_KEY` present in both lists, then roll the API and UI deployments after secret changes.
- **Dashboard 308 redirect loop in dev:** the UI service redirects HTTP→HTTPS for non-local hosts when it sees `X-Forwarded-Proto: http`. Override with the `UI_REQUIRE_HTTPS` env on the `mcp-sentinel-ui` deployment: `auto` (default — redirect public hosts only), `true`/`on`/`1`/`yes` (always redirect HTTP for public hosts), `false`/`off`/`0`/`no` (never redirect, use this when the UI is fronted by a non-TLS terminator on a real hostname).
- **Ingress / routes:** `kubectl get ingress -A` and confirm paths match the gateway and demo servers you expect.
- **Custom ingress namespaces:** bundled Traefik manifests watch `registry`, `mcp-sentinel`, `mcp-servers`, `mcp-servers-org`, and `mcp-servers-public` only so the controller does not need cluster-wide Secret access. If you place public Ingresses in another namespace, including per-team namespaces such as `mcp-team-acme`, add that namespace to the Traefik `--providers.kubernetesingress.namespaces` list, bind the `traefik-watch` Role there, and allow ingress-controller traffic through the namespace NetworkPolicy. Platform API `team create` performs that setup for the repo-managed `traefik/traefik` Deployment; for k3s / external Traefik (`kube-system/traefik`, cluster-wide ingress watch), setup sets `PLATFORM_TEAM_TRAEFIK_WATCH=disabled` automatically. Override with `PLATFORM_TEAM_TRAEFIK_WATCH=required` only when patching repo-managed Traefik is intentional. See `docs/multi-team.md` for the multi-team namespace/RBAC pattern.
- **Tenant `registry push` 500 / copy timeout:** the CLI uploads a docker save tar to `POST /api/runtime/registry/push`. The API must not copy that tar through `pods/exec` stdin (kubelet stream timeout ~35s). The supported path registers a one-time internal transfer URL and runs a short-lived skopeo helper pod in `mcp-sentinel` that `curl`s the tar then `skopeo copy`s to `registry.<domain>` (rewritten to in-cluster registry Service DNS). Ensure `k8s/08-api-rbac.yaml` includes the `mcp-sentinel` registry-push Role and the API serves `/internal/registry-push/tar/{token}`.
- **Private / HTTP in-cluster registry / k3s:** Pull and push can fail with `https` vs `http` or `registry.local` DNS on nodes. See **k3s and HTTP registry (config files)** below, set **`MCP_REGISTRY_*`** before `server generate` when you want `ClusterIP:port` in manifests, and raise **`MCP_DEPLOYMENT_TIMEOUT`** if setup rollouts time out on slow first pulls.
- **Prod DNS / ACME:** with `MCP_PLATFORM_DOMAIN=example.com`, setup derives `registry.example.com`, `mcp.example.com`, and `platform.example.com`. All three public DNS records must point at the ingress IP and port 80 must reach Traefik for HTTP-01. If cert-manager reports NXDOMAIN, verify from outside and inside the cluster: `getent hosts registry.example.com`, `getent hosts mcp.example.com`, `getent hosts platform.example.com`, and `kubectl run dns-check --rm -i --restart=Never --image=busybox:1.36 -- nslookup platform.example.com`.
- **cert-manager "already installed" but TLS times out:** setup's installed check only tests for CRD existence, not pod health. After a k3s restart or cluster disruption, CRDs survive but pods may be gone. If `kubectl get pods -n cert-manager` shows no Running pods, reinstall: `curl -sL https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml | kubectl apply -f -` and wait for all three pods to be Ready before rerunning setup.
- **Multiple kubeconfigs / wrong cluster targeted by setup:** always pass `--kubeconfig <path>` explicitly when your workstation has more than one kubeconfig. The `KUBECONFIG` env var alone is not sufficient — TLS and cert-manager operations inside setup use `platformKubernetesClients()` which requires the explicit path to be set (fixed in `internal/cli/setup/platform/kube_client.go`). Without this, ClusterIssuer and Certificate resources land on the wrong cluster and the cert never issues on the target cluster.
- **Sentinel pods ImagePullBackOff after setup with registry host drift:** `MCP_REGISTRY_HOST` is the public ingress hostname alias for external access; it must not silently override the platform image pull endpoint. For bundled HTTPS public installs, set `MCP_REGISTRY_ENDPOINT=registry.<domain>` so kubelet pulls match the public TLS certificate. For private HTTP or node-local registry paths, set `MCP_REGISTRY_ENDPOINT` / `MCP_REGISTRY_PULL_HOST` to the exact node-pullable host:port and configure node trust or insecure-registry settings for that exact host.
- **Tenant MCPServer ImagePullBackOff with registry image refs:** `server generate` rewrites MCPServer `spec.image` to `MCP_REGISTRY_PULL_HOST` / `MCP_REGISTRY_ENDPOINT` (default `registry.registry.svc.cluster.local:5000`) so tenant pods use the configured node-pullable registry endpoint. Keep `MCP_REGISTRY_INGRESS_HOST` for `docker push` / `registry push` from your workstation; set `MCP_REGISTRY_PULL_HOST` before `server generate` when tenant pods need a different pull address than platform setup uses. For public TLS registry setups the platform automatically creates an `mcp-runtime-registry-pull` dockerconfig secret in each team namespace (via `ensureManagedNamespace`) and attaches it to the `mcp-workload` SA. Pull secrets must target `mcp-workload`, not `default` — the operator's `ApplyRestrictedPodDefaults` sets `serviceAccountName: mcp-workload` on all MCPServer pods.
- **registry-allow-ingress NetworkPolicy blocks k3s Traefik:** the bundled NetworkPolicy allows traffic from the `traefik` namespace (repo-managed Traefik) but k3s runs Traefik in `kube-system`. The current `config/registry/base/networkpolicy.yaml` includes a `kube-system` + `app.kubernetes.io/name: traefik` pod selector entry to handle k3s deployments; it also includes an `ipBlock: 10.0.0.0/8` fallback rule to address k3s ≥1.35 ipset staleness (see next entry). Both fixes are present in the base manifest — apply it with `kubectl apply -f config/registry/base/networkpolicy.yaml` on k3s clusters.
- **k3s ≥1.35 NetworkPolicy ipset stale for new pods (registry unreachable from new pods):** On k3s 1.35.x+k3s1, the built-in kube-router network policy controller creates ipsets for namespace-based `from` selectors at policy creation/modification time but does NOT update them when new pods are created in allowed namespaces. Symptoms: long-running pods (grafana, tempo, prometheus) can reach `registry.registry.svc.cluster.local:5000` but newly-created pods from the same namespaces get "Connection refused" (TCP RST from iptables REJECT). Diagnosis: `kubectl exec -n mcp-sentinel <long-running-pod> -- wget -qO- http://registry.registry.svc.cluster.local:5000/v2/` succeeds while a fresh job pod in the same namespace fails. Fix: the base `config/registry/base/networkpolicy.yaml` now includes `ipBlock: 10.0.0.0/8` in the ingress allow rule to cover k3s's default pod CIDR (10.42.0.0/16) and flannel's (10.244.0.0/16 is in 10.0.0.0/8). If your cluster uses a different pod CIDR, add the appropriate `ipBlock`. The `cluster doctor` checks 15 and 18 will fail before this fix is applied; they pass after.
- **Platform UI 404 / wrong host:** when `MCP_PLATFORM_DOMAIN` (or `MCP_PLATFORM_INGRESS_HOST`) is set, setup applies a host-based ingress `mcp-sentinel-platform-ui` in `mcp-sentinel`, a sibling `mcp-sentinel-platform-observability` Ingress for `/grafana`, and, when TLS is enabled, `mcp-sentinel-platform-ui-http` for HTTP→HTTPS redirect. Verify with `kubectl get ingress mcp-sentinel-platform-ui mcp-sentinel-platform-observability -n mcp-sentinel -o yaml`; the UI rule should be host=`platform.<domain>` routing `/` to `mcp-sentinel-ui:8082` (and `/api` to the same service), while observability routes `/grafana` to `grafana:3000` with `sentinel-admin-auth@file`. A direct `/prometheus` route is intentionally absent. If the dashboard returns Traefik default 404, check that DNS resolves `platform.<domain>` to the cluster ingress, then `kubectl logs -n traefik deploy/traefik --tail=120` for routing errors. The dev path-based gateway (`mcp-sentinel-gateway`) keeps working when `MCP_PLATFORM_DOMAIN` is unset.
- **Duplicate Traefik:** setup reuses an active external Traefik such as k3s `kube-system/traefik` and refuses `--force-ingress-install` when that would install repo-managed `traefik/traefik` as a second stack. Remove one Traefik install, or rerun setup with `--ingress none` when your platform ingress is already managed outside this repo. External ingress controllers must provide an equivalent registry admin-auth guard to `/api/registry/authz` before exposing `registry.<domain>` publicly.
- **Prod registry 404 / image pulls say “not found”:** if `registry-cert` is Ready but pods fail to pull `registry.<domain>/<repo>:<tag>`, check the public registry route with an admin key: `curl -k -i -H "x-api-key: $ADMIN_API_KEY" https://registry.<domain>/v2/`. Expected is HTTP 200 with `docker-distribution-api-version: registry/2.0`; HTTP 401/403 means the route is active but admin auth was missing or rejected; Traefik `404 page not found` means the ingress/router is not active. Check `kubectl logs -n traefik deploy/traefik --tail=120` and `kubectl get ingress registry -n registry -o yaml`. In prod, the registry ingress must not reference the dev-only `pii-redactor@file` middleware.
- **Prod MCP server URLs:** prefer path-based public routing for clients: `https://mcp.<domain>/<server-name>/mcp`. Use `spec.publicPathPrefix: <server-name>` and set the server’s `MCP_PATH` to `/<server-name>/mcp`; `mcp-runtime server deploy` does this automatically for its default `/<name>/mcp` route. Avoid examples that require a custom `Host` header such as `go.example.local`.

### MCP server pod / sidecar checks

Use these when a server is deployed but gateway behavior, grants, or analytics
look wrong:

```bash
SERVER=workspace-assistant-mcp
CONTAINER=workspace-assistant-mcp

kubectl get mcpservers -n mcp-servers
kubectl get pods -n mcp-servers -o wide
kubectl get pods -n mcp-servers \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.name}{","}{end}{"\n"}{end}'

POD="$(kubectl get pods -n mcp-servers -l app="$SERVER" -o jsonpath='{.items[0].metadata.name}')"
kubectl describe pod -n mcp-servers "$POD"
kubectl logs -n mcp-servers "$POD" -c "$CONTAINER"
kubectl logs -n mcp-servers "$POD" -c mcp-gateway
./bin/mcp-runtime auth login --api-url <platform-url>   # platform API default
./bin/mcp-runtime server policy inspect "$SERVER" --namespace mcp-servers
kubectl get mcpaccessgrant,mcpagentsession -n mcp-servers -o wide
```

The sidecar container is named `mcp-gateway`; it runs the `mcp-gateway`
image/process. Many runtime images are distroless, so `/bin/sh` and
`/bin/bash` may not exist. Prefer logs/describe, or attach a debug container:

```bash
kubectl debug -it -n mcp-servers "pod/$POD" \
  --target="$CONTAINER" \
  --image=busybox:1.36 -- sh
```

### k3s / Public Registry Ops

For public k3s setup, cleanup/restore, rollout, multitenancy smoke tests,
registry TLS/auth, node DNS versus pod DNS, and ImagePullBackOff debugging,
use `.codex/skills/k3s-public-ops/SKILL.md` plus
`docs/k3s-deployment-runbook.md` and `docs/cluster-readiness.md`.

Key reminder: kubelet/containerd image pulls use node DNS and node registry
trust, not pod CoreDNS. Production public k3s installs should prefer
TLS-covered image refs such as `registry.<domain>/...` with the platform-created
pull Secret attached, while private HTTP/node-local registry paths require exact
`/etc/rancher/k3s/registries.yaml` configuration on every node.

## Governance (grants and sessions)

- **UI** can create/apply grants and sessions and toggle grant enablement and session state.
- **CLI (platform API default):** run `mcp-runtime auth login --api-url <platform-url>`,
  then scaffold with `access grant init` / `access session init` when helpful, and
  apply grants with `mcp-runtime access grant apply --file <file.yaml>`.
  **Session apply via platform API is admin-only.** Adapter flows usually skip
  manual session apply; use `adapter stdio|proxy --server ... --agent ...
  --auto-refresh`.
- **Admin fallback:** `kubectl apply -f` or apply commands with `--use-kube`
  require admin/operator kubeconfig/RBAC and bypass platform auth.
- **Team isolation:** use one namespace per team plus first-class team identity. `MCPServer.spec.teamID` marks the server owner, `SubjectRef.teamID` constrains grants/sessions to callers from that team, and the gateway matches all non-empty `humanID` / `agentID` / `teamID` fields exactly. Keep single-team examples in `mcp-servers`.
- **Platform access hardening:** platform API server/grant/session writes reject shared-catalog writes by non-admin callers and reject cross-namespace `serverRef.namespace`. Server writes default/validate `spec.teamID` against the authenticated principal namespace. Grant/session writes default missing `subject.teamID` from the referenced server team, but preserve explicit foreign `subject.teamID` values for delegated cross-team access.
- **Advisory identity checks:** access apply commands warn, but do not block, when `subject.humanID` looks malformed, case-ambiguous, whitespace-padded, or namespace-encoded.
- **Side effects:** each listed `MCPServer.spec.tools[]` entry must declare `sideEffect: read|write|destructive`; grants must set `allowedSideEffects` explicitly. Empty or omitted `allowedSideEffects` allows no side-effect classes.
- **Example**

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: workspace-assistant-grant
  namespace: mcp-servers
spec:
  subject: {humanID: user-123, agentID: ops-agent}
  serverRef: {name: workspace-assistant-mcp, namespace: mcp-servers}
  maxTrust: high
  allowedSideEffects: [read]
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
  serverRef: {name: workspace-assistant-mcp, namespace: mcp-servers}
  consentedTrust: high
  policyVersion: v1
```

- **HTTP API (requires `x-api-key`):** `POST /api/runtime/grants`, `POST /api/runtime/sessions`; the API checks that `serverRef` matches an existing `MCPServer` (best-effort, not transactional). Toggles: `POST /api/runtime/grants/{ns}/{name}/enable|disable`, `POST /api/runtime/sessions/{ns}/{name}/revoke|unrevoke`.
- **Kind e2e** applies generated access YAML, waits for gateway policy materialization, and exercises real MCP JSON-RPC for allow/deny.

## Traffic generation (MCP JSON-RPC)

**Single call** (set `<session>` from the `initialize` response):

```bash
PROTO=2025-06-18
BASE=http://localhost:18080/workspace-assistant-mcp/mcp
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
bases = ["http://localhost:18080/workspace-assistant-mcp/mcp","http://localhost:18080/data-utility-mcp/mcp","http://localhost:18080/text-analysis-mcp/mcp"]
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
- Dashboards: Grafana via the ingress base URL in dev, or via `https://platform.<domain>/grafana` as an admin. Prometheus stays internal and is queried through Grafana's datasource; port-forward `svc/prometheus` only for backend debugging.
- Full request tracing: run
  `E2E_SCENARIOS=smoke-auth,governance,trust,oauth,observability bash test/e2e/kind.sh`.
  The observability scenario must find one trace through the gateway,
  `mcp-sentinel-ingest`, and `mcp-sentinel-processor`, with `kafka.produce`,
  `kafka.consume`, and `clickhouse.insert_event` spans visible through both
  direct Tempo and Grafana's Tempo datasource.

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
- **Workspace assistant sample** — `examples/workspace-assistant-mcp/`
- **Website source** — `website/` (documentation site, separate from the Go control plane)

---

*Tip for agents: after substantive edits, run the narrowest `go test` for touched packages, then `go test ./...` before suggesting merge. Update golden files only when help text or CLI output should change on purpose.*

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
