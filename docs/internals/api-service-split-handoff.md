# API service split — implementation handoff

Execution checklist for a coding agent. **Design rationale and contracts live in
[`api-service-split.md`](./api-service-split.md)** — read it first; this file is the
ordered, file-level task list, the current-state delta, and the acceptance criteria.

Scope reminder (locked): split `services/api` (`mcp-sentinel-api`) into **platform-api**,
**runtime-control**, **analytics-api**. HS256 shared-secret multi-`aud` JWT via
`pkg/platformauth`; Traefik path-prefix routing; **`/api/v1/*` is the only public surface
(no `/api/*` compat)**; big-bang cutover; runtime-control holds zero Postgres (hybrid via
platform-api `/internal/*`).

---

## 0. Current state (what is already done — do NOT redo)

**Milestone gate (required):** after each M1.x carve-out, run the live-cluster
checks in §"Milestone validation script" below. Do **not** start the next milestone
until that script is green and any failures are fixed (rebuild image, rollout, re-run).

Last verified: **2026-06-15** on branch `refactor/api-service` — M1.3–M1.6 gate
**28/28 PASS** on `kind-mcp-runtime`.

Branch: `refactor/api-service`. Work already committed/working-tree:

| Area | Status | Notes |
|------|--------|-------|
| `pkg/apihttp` | **DONE (library only)** | `codes.go`, `error.go`, `http.go`, `query.go`, `pagination.go` + tests. Envelope `{"error":<code>,"message":...}`, `WriteError`, `ParseLimit`/`ParseCursor`/`ListMeta`/`NextLink`, `QueryInt`. **Not yet called by any handler.** |
| `pkg/platformauth` | **DONE + TESTED (M1.1)** | Shared principal/claims, required audiences, HS256 sign/verify, middleware/context helpers, OIDC interface, and cached HTTP API-key resolver. Root race tests and live multi-audience JWT verification passed. |
| Enriched platform JWT | **DONE + TESTED (M1.2)** | Access tokens resolve the full principal once at mint time, carry all three audiences, and verify without a Postgres lookup. Live login produced `platform`, `runtime-control`, and `analytics-api` audiences and authorized a protected runtime request. |
| Internal platform API | **DONE + TESTED (M1.2)** | Token-gated `/internal/auth/resolve`, `/internal/identity/resolve-ids`, `/internal/audit`, teams CRUD, team memberships, operations snapshot, and namespace reads are wired into the monolith. `INTERNAL_AUTH_TOKEN` is rendered and mounted. |
| Analytics extraction | **DONE + TESTED (M1.3)** | `services/analytics-api/` with `/api/v1/*` routes, ClickHouse usage/events, HTTP identity resolver + `platformauth`. Monolith analytics routes removed. `k8s/08-analytics-api.yaml` deployed; cluster validation passed (`/api/v1/stats`, `/api/v1/events`, `/api/v1/user/analytics/usage`). |
| Runtime extraction | **DONE + TESTED (M1.4)** | `services/runtime-control/` with HTTP `platformclient` (no Postgres), `/api`+`/api/v1` runtime routes, admin operations/deployments facade, user API keys. `k8s/08-runtime-control.yaml` deployed; cluster: `/api/v1/runtime/servers` → `200`; monolith `/api/runtime/servers` → `404`. |
| Platform extraction | **DONE + TESTED (M1.5)** | `services/platform-api/` with Postgres `platformstore`, auth/registry/admin, `/internal/*`, `/ready` (Postgres ping), `pkg/svcboot` shared bootstrap. `k8s/08-platform-api.yaml` deployed; analytics/runtime `PLATFORM_API_URL` → `mcp-platform-api:8080`. Monolith stripped to `/health` only. Gate: login/me/internal-resolve on platform-api; monolith auth routes → `404`. |
| `/api/v1` only | **DONE + TESTED (M1.6)** | platform-api and runtime-control dropped `/api/*` dual mounts; analytics-api was already v1-only. `route_ownership_test.go` per service; gate script `m16` checks legacy `404` + v1 still works. |
| Secret rename | **DONE** | `PLATFORM_JWT_SECRET` → `JWT_SECRET` in `auth.JWTSecretFromEnv` (`services/api/auth/seed.go`), `main.go`, `k8s/08-api.yaml`, `k8s/21-...bootstrap-job.yaml`, setup secret render (`analytics.go`). `INTERNAL_AUTH_TOKEN` added to `k8s/02-secrets.yaml.example` + setup render. |
| Cutover markers | **DONE (comments only)** | `config/ingress/base/dynamic-config.yaml` (registry forwardAuth) and `k8s/09-ui.yaml` (`API_UPSTREAM`) carry "Post api-service-split cutover: mcp-platform-api..." comments. Values NOT yet repointed. |

**M2 (M2.1 RBAC + M2.2 setup/CLI + M2.3 ingress/UI) cluster-tested.** Monolith scaled to 0;
Traefik gateway routes `/api/v1/*` to split services. Setup builds three API images, doctor probes
runtime-control `/api/v1/runtime/*` plus platform/analytics `/health`+`/ready`, Prometheus scrapes
9090/9094/9095. **M2.5 harness** (e2e/smoketest/CI → split APIs, NetworkPolicy egress) landed.
**M2.4 partial:** analytics-api uses `pkg/apihttp` envelopes + `ParseLimit`; `/events/filter` folded into `GET /api/v1/events`.
Full monolith delete + platform/runtime `apihttp` pass remain.

Verification completed for M1.1-M1.2:

- `go test -race -count=1 ./pkg/platformauth/... ./pkg/apihttp/...`
- `cd services/api && go test -race -count=1 ./internal/platforminternal ./internal/platformstore ./auth .`
- `cd services/api && go build ./...`
- Fresh Kind cluster: all 38 permissive development `cluster doctor` checks passed.
- Rolled the checkout-built API image to all three replicas and verified login minted
  the enriched multi-audience token and `/api/runtime/servers` returned HTTP `200`.
- Rolled the M1.2 image and exercised the internal API through the in-cluster Service:
  missing token `401`, ID resolution `200`, audit ingestion `202`, and teams list `200`.
- `E2E_CACHE_MODE=1 E2E_KEEP_CLUSTER=1 CLUSTER_NAME=mcp-runtime
  E2E_SCENARIOS=smoke-auth,governance bash test/e2e/kind.sh` passed.

---

## 1. Conventions (repo-specific — follow exactly)

- Go version from `go.mod` (1.26). Each `services/*` is its **own module** with
  `replace mcp-runtime => ../..` (two dots — services are one level under `services/`).
- Per-touched-package, before pushing:
  ```bash
  gofmt -s -l .                      # must be empty
  go build ./... && go vet ./...     # in repo root AND each services/* dir
  go test ./... -count=1 -race       # same
  ```
- Shared code goes in the **root module** `pkg/` (all service modules import it via the
  replace). Service-private code stays under that service's `internal/`.
- Branch is already `refactor/api-service`; commit per the sequence below
  (`feat(services-api):`, `feat(platformauth):`, `feat(runtime-control):`, etc.). Never push to `main`.
- Mirror nearest existing patterns; CLI/HTTP errors via `pkg/apihttp` (new) and `pkg/errx`.
- Skills symlink `.claude/skills → ../.codex/skills`; after non-trivial changes update the
  affected `.codex/skills/*/SKILL.md`.
- Use `graphify query "<question>"` (graph at `graphify-out/graph.json`) before grepping;
  `graphify update .` after code changes.

---

## 2. Work breakdown (ordered; each task is independently buildable + testable)

### Milestone 1 — foundations + three buildable binaries (NO K8s/routing changes)

#### M1.1 — `pkg/platformauth` (KEYSTONE) — **COMPLETE, TESTED**
Create `pkg/platformauth/` in the root module:
- `claims.go` — `Claims` embedding `jwt.RegisteredClaims` + `Email/Role/Namespace`,
  `Teams []TeamClaim`, `AllowedNamespaces []string`, `APIKeyID`, `AuthType`. **Relocate
  `Principal`/`PrincipalTeam`** (and their methods `UserID/HasNamespace/TeamRole/
  TeamForNamespace`) verbatim from `services/api/internal/platformstore/types.go:58-122`.
  Add `ToPrincipal(Claims) Principal` and `ClaimsFromPrincipal(Principal) Claims`.
  In `platformstore`, replace the struct defs with **type aliases**
  (`type Principal = platformauth.Principal`, `type PrincipalTeam = platformauth.PrincipalTeam`)
  so the existing `apiauth.Principal = platformstore.Principal` chain still compiles.
- `audience.go` — `AudiencePlatform="platform"`, `AudienceRuntime="runtime-control"`,
  `AudienceAnalytics="analytics-api"`, `RequiredAudiences() []string`. Reuse
  `serviceutil.AudienceMatches` (`pkg/serviceutil/auth.go:47`) — do not reimplement.
- `sign.go` — `Sign(secret []byte, p Principal, ttl, aud []string) (string, error)`; HS256;
  `Issuer="mcp-runtime"`.
- `verify.go` — `Verify(secret []byte, token, expectedAudience string) (Claims, error)`;
  HS256-only method check + issuer + `AudienceMatches`; **no Postgres**.
- `middleware.go` — `Authenticator{Secret, Audience, ServiceAPIKeys, AdminAPIKeys,
  LegacyAdminKeys, UserKeyResolver, OIDC, PublicFallback}` with `Middleware`/`RequireRole`
  (port of `services/api/main.go` `auth`/`authOrPublicCatalog`/`requireRole`/
  `authenticateRequest`, ~lines 985-1131). **Move** the context + audit helpers from
  `services/api/internal/apiauth/auth.go` (`WithPrincipal`, `FromContext`, `RequestIP`,
  `RequestSource`, `AuditSource`, `AuditIdentityLabel`, `RoleAdmin`/`RoleUser`,
  `UnknownRequestIP`) here; leave `internal/apiauth` as thin aliases or delete it.
- `oidc.go` — `OIDCVerifier` wrapping `keyfunc.JWKS` (port of the OIDC branch in
  `main.go` ~1067-1119). Only platform-api will construct one.
- `UserKeyResolver` interface: `ResolveAPIKey(ctx, rawKey string) (Principal, bool, error)`.
- **Tests:** Sign→Verify round-trip; multi-`aud` token accepted under each audience;
  wrong-audience / expired / `none`-alg / issuer-mismatch rejected; `ToPrincipal` fidelity.
- **Acceptance met:** root race tests pass; the existing monolith builds against the
  relocated aliases; the shared resolver cache is keyed by API-key hash.

#### M1.2 — enriched token + internal endpoints (still one binary) — **COMPLETE, TESTED**
- `platformstore.CreateAccessToken` (`auth.go:360`): build full `Claims` by calling
  `PrincipalForUserID` **once at mint time** (role/email/namespace/teams[id,slug,name,
  namespace,role]/allowedNamespaces) + `aud = RequiredAudiences()`, then `platformauth.Sign`.
- Reimplement `AuthenticateJWT` as `platformauth.Verify` + `ToPrincipal` (drop the per-verify
  `PrincipalForUserID` Postgres round-trip).
- Add platform-api internal handlers on the monolith (see spec §"Internal API contract"):
  `POST /internal/auth/resolve`, `POST /internal/identity/resolve-ids`, `POST /internal/audit`,
  and identity CRUD `POST/GET/DELETE /internal/identity/teams[/{slug}]`,
  `GET /internal/identity/namespaces[/{name}]`. Bind in-cluster only; gate with
  `Authorization: Bearer <INTERNAL_AUTH_TOKEN>`. `/internal/audit` returns `202`.
- `UserKeyResolver`: platform-api implements directly via
  `platformstore.AuthenticateUserAPIKey`; provide an HTTP-client impl (30–60s in-process
  cache keyed by key hash) for the other services.
- **Acceptance met:** monolith builds; focused race tests pass; internal endpoints and
  cached HTTP resolver are unit-tested; the checkout-built API image passed live login,
  protected-request, cluster-doctor, and `smoke-auth,governance` validation.

#### M1.3 — carve `services/analytics-api` — **COMPLETE, TESTED**
- New module `services/analytics-api/` (`module mcp-analytics-api`, `replace mcp-runtime => ../..`).
- Move `services/api/internal/analytics/` in; extract `internal/usage/` from `main.go`
  (`handleAnalyticsUsage`, `handleUserAnalyticsUsage`, `analyticsPrincipal*Scope`,
  `queryAnalyticsTools`, `analyticsWhereClause`, helpers, ~`main.go:405-980`) — these take
  Principal-from-context (now fully populated by the verified JWT) + an optional id-resolver
  client (calls platform-api `/internal/identity/resolve-ids` for display names).
- `main.go`+`routes.go` registering only analytics routes; `Authenticator{Audience: AudienceAnalytics}`.
- Remove those routes/handlers from the monolith.
- **Acceptance:** `cd services/analytics-api && go build ./... && go test ./... -race`.

#### M1.4 — carve `services/runtime-control` — **COMPLETE, TESTED**
- New module `services/runtime-control/` (`module mcp-runtime-control`).
- Move `services/api/internal/runtimeapi/` (+`access/`), `admin/`, `deployments/`, `runtime/`.
- **Remove `RuntimeServer.platformStore`** (`internal/runtimeapi/server.go:21,34`). Replace
  identity/CRUD/audit Postgres usage with HTTP clients to platform-api `/internal/*`
  (resolver, audit buffer+retry, identity). Keep only the **K8s half** of teams/namespaces;
  team-create orchestration per spec §"Hybrid orchestration".
- Keep ClickHouse read for `/api/.../dashboard/summary`. Keep registry-push emptyDir +
  `POD_IP` + `PORT` on this service.
- `Authenticator{Audience: AudienceRuntime, UserKeyResolver: httpResolver}`.
- **Acceptance:** module builds + tests pass; **add a regression test** that an API-key
  caller resolves to a Principal **with** teams/namespaces (fixes `runtimeapi/user_keys.go:76-81`).

#### M1.5 — reduce `services/api` → `services/platform-api` — **COMPLETE**
- Rename/relocate the remainder to `services/platform-api/` (`module mcp-platform-api`):
  Postgres `platformstore`, `auth/`, `identity/`, `registry/` (authz+credentials+config),
  `migrations/`, internal endpoints, OIDC/JWKS, migrated team/namespace identity CRUD,
  bootstrap-only entrypoint.
- Add `pkg/svcboot` (root module): factor the triplicated bootstrap (API-key map building,
  `serviceutil.StartMetricsServer`, otelhttp wrap, graceful shutdown, OTEL name, `/health`)
  out of `main.go:316-358`. All three `main.go` use it.
- Assign distinct ports: platform-api 8080/9090, runtime-control 8084/9094, analytics-api
  8085/9095. Add `/ready` per spec (platform=Postgres ping, analytics=ClickHouse ping,
  runtime=K8s initialized → `503 runtime_error`).
- Per-service `Dockerfile` (clone of `services/api/Dockerfile`, distroless, build context =
  repo root, copy only the subpackages that service needs).
- **Acceptance:** all three `services/*` build + test independently; root `go build ./...`,
  `go test ./... -race` green; `gofmt -s -l .` empty.

#### M1.6 — mount `/api/v1` — **COMPLETE**
- Each service registers handlers under `/api/v1/*` as the **only** public surface
  (no `/api/*`). `/internal/*` and `/health`,`/ready` stay unversioned.
- **Acceptance:** route-ownership unit test per service (foreign path → 404; e.g.
  runtime-control does not serve `/api/v1/auth/login`).

### Milestone 2 — deployment, routing, contract, cutover

#### M2.1 — manifests + RBAC
- Replace `k8s/08-api.yaml` with `08-platform-api.yaml`, `08-runtime-control.yaml`,
  `08-analytics-api.yaml` (Deployment/Service/SA each; keep securityContext, resources,
  replicas≈3; distinct ports; env subsets per spec §"Secrets and environment"). Only
  runtime-control gets the registry-push emptyDir + `POD_IP` + `PORT=8084`.
- Split `k8s/08-api-rbac.yaml`: **runtime-control** keeps the entire broad ClusterRole
  (renamed `mcp-runtime-control`, incl. impersonation), the `registry-push` Role and
  `team-secrets` ClusterRole (renamed/rebound); re-point CRB `mcp-sentinel-platform-admin`
  `roleRef`; **keep `mcp-runtime-traefik-watch` name unchanged**. **platform-api** gets only
  the `user-key-secret` namespace Role. **analytics-api** gets no Role/ClusterRole
  (`automountServiceAccountToken: false`).
- Update `test/manifest/api_rbac_test.go` (→ rename) for new names; assert platform/analytics
  have no broad ClusterRole/impersonation.

#### M2.2 — setup / CLI — **DONE + TESTED**
- `internal/cli/setup/platform/images.go`: `AnalyticsImageSet` → `PlatformAPI/RuntimeControl/
  AnalyticsAPI`; `analyticsComponents` → three entries (new Dockerfiles); `assignAnalyticsImage`
  → three cases; fix index-based assignment in `prepareAnalyticsImages`; `registryHostFromImage`.
- `internal/cli/setup/platform/analytics.go`: apply lists → six new manifests; rollout-wait
  lists → three deployments; image substitution → three replacements; `imageList()` → three.
- `internal/cli/cluster/doctor/`: split the `mcp-sentinel-api` constant into three + ports;
  repoint runtime probes to `mcp-runtime-control:8084`; broaden the `app=` NetworkPolicy match;
  add platform/analytics `/health`+`/ready` probes.
- `k8s/11-prometheus.yaml`: three scrape jobs (9090/9094/9095).
- `k8s/21-platform-admin-bootstrap-job.yaml`: image → `mcp-platform-api:latest`; ensure the
  bootstrap-only path lives in the platform-api binary.

#### M2.3 — ingress + forwardAuth + UI
- `internal/cli/setup/ingressmanifest/render.go`: rewrite the rule block to the ordered,
  most-specific-first `/api/v1/...` rules in spec §"Traefik path rules" (registry-push Exact
  first; platform admin paths before generic `/api/v1/admin`; UI `/` last). Update `render_test.go`.
- Repoint registry forwardAuth `address` (both `config/ingress/base/dynamic-config.yaml` and
  `config/ingress/overlays/http/dynamic-config.yaml`) to
  `http://mcp-platform-api.mcp-sentinel.svc.cluster.local:8080/api/v1/registry/authz`;
  update `test/manifest/registry_forward_auth_test.go` suffix.
- UI: remove the browser `/api/*` proxy (`services/ui/main.go` `apiProxy`); keep `API_UPSTREAM`
  for server-side `/auth/*`/OIDC and repoint `k8s/09-ui.yaml` to the platform-api Service.

#### M2.4 — API-contract pass (apply `pkg/apihttp` to handlers)
- Migrate every handler to the `apihttp` envelope + stable codes; **delete
  `runtimeapi.errorCode`** (message-derived). Migrate single-field `{"error":...}` responders.
- **Analytics handlers** (`internal/analytics/handlers.go`): replace `writeQueryError` with
  `apihttp.WriteError` + `CodeQueryFailed`; replace the local silent-clamp `queryInt` with
  `apihttp.ParseLimit`/`QueryInt` (invalid → `400 invalid_query_param`).
- Add cursor pagination (`apihttp.ParseCursor`/`ListMeta`/`NextLink`) to events + unbounded lists.
- **Fold `/events/filter` into `GET /api/v1/events?...`** (query filters); drop the separate route.
- Per-service `services/*/openapi.yaml`, served at `GET /api/v1/openapi.yaml`.

#### M2.5 — cutover + delete (see spec §"Cutover", §"Cutover grep")
- Build/push three images; apply new manifests alongside `mcp-sentinel-api`; wait `/ready`;
  flip ingress; roll UI; smoke (login, `/api/v1/stats`, `/api/v1/runtime/servers`, a registry
  push exercising the POD_IP transfer); apply Prometheus; run doctor.
- Delete `mcp-sentinel-api` Deployment/Service/SA, old RBAC (keep `mcp-runtime-traefik-watch`),
  `services/api/` + its Dockerfile + registry image, the UI proxy code; grep `mcp-sentinel-api`
  and `08-api` (+ the extra files in spec §"Cutover grep") for stragglers.
- Add NetworkPolicy egress: runtime-control & analytics-api → platform-api:8080.

### Milestone 3 — contract hardening (AFTER the split is verified green)

Prereq: M2 cutover done, the §4 verification matrix is green, all three services live. This
restores the cross-service safety the monolith got for free from the Go compiler, without a
Pact broker or runtime coupling — ~80% of consumer-driven-contract safety, far less machinery.

#### M3.1 — shared `/internal/*` DTOs (restore cross-module compile safety)
- Create `pkg/internalapi/` (root module) holding the request/response structs for **every**
  `/internal/*` endpoint: `auth/resolve`, `identity/resolve-ids`, `audit`, `identity/teams[/{slug}]`,
  `identity/namespaces[/{name}]`. Provider (platform-api) and consumers (runtime-control,
  analytics-api) all import these structs — a field rename becomes a **compile error on every
  side**, the way it was before the split. Reference `platformauth.Principal`/`Claims` from the DTOs.
- **Acceptance:** all three modules build against the shared DTOs; no hand-rolled duplicate
  request/response structs remain in any service's `internal/`.

#### M3.2 — provider tests validate real responses against the committed OpenAPI
- Per service, add a test that boots the handler (`httptest`), issues representative requests,
  and asserts each response **validates against that service's `openapi.yaml`** using an
  OpenAPI validator (`github.com/pb33f/libopenapi-validator` or `kin-openapi`
  `openapi3filter`). The committed spec becomes the **executable contract** — drift between a
  handler and its spec fails the build.
- Cover at minimum: one success + one error-envelope per public route group, and **every**
  `/internal/*` endpoint (request body + response).
- **Acceptance:** `go test ./...` in each service includes spec-validation tests; deliberately
  breaking a field vs the spec makes the test fail.

#### M3.3 — CI wiring + documented upgrade trigger
- Add the spec-validation tests to the per-service jobs in `.github/workflows/ci.yaml`.
- Optional: OpenAPI lint + breaking-change check (e.g. `oasdiff`) of each `openapi.yaml`
  against its previous committed version.
- **Document the upgrade trigger** (in this doc + `docs/security/authz-matrix.md` or
  `docs/sentinel.md`): adopt real consumer-driven contracts (`pact-go` + a Pact Broker /
  PactFlow `can-i-deploy` gate) **only when an external client or an independently released
  team** starts consuming these APIs. Until then, shared-DTO + OpenAPI-validation is the
  intended ceiling — do not add a broker speculatively.

---

## 3. Gotchas (will bite if ignored)

1. **Registry push is pod-local.** Push handler + `/internal/registry-push/tar` + emptyDir +
   `POD_IP` + `PORT` must ALL be on runtime-control; the skopeo helper curls
   `http://POD_IP:PORT/internal/registry-push/tar`, so `PORT` must equal runtime-control's
   listen port (8084). Never expose `/internal/registry-push/tar` on ingress.
2. **Audit durability.** `/internal/audit` client must buffer + retry (queue ~1000, backoff
   100ms–30s, drop-oldest + metric) so a brief platform-api blip doesn't drop runtime audit.
3. **Enriched-claim staleness.** Team/namespace changes only take effect on next login/refresh;
   keep TTL short. This is the deliberate trade for zero-Postgres downstream services.
4. **OIDC only at platform-api.** runtime/analytics accept only platform-minted tokens; confirm
   no client posts raw OIDC tokens to runtime endpoints.
5. **Liveness vs readiness.** `/health` must stay `200` during dependency blips; only `/ready`
   reflects Postgres/ClickHouse/K8s — it gates Service endpoints and rollout.
6. **Type-alias keystone (M1.1).** Doing the `platformstore.Principal = platformauth.Principal`
   alias is what keeps the monolith compiling through the whole refactor — do it first.

---

## 3.5 Milestone validation script (live cluster gate)

After each M1.x carve-out:

1. **Trivy** — scan shipped binaries (Go stdlib CVEs are not visible in `go.mod` alone):
   ```bash
   bash hack/trivy-sentinel-images.sh platform-api analytics-api runtime-control
   # or all sentinel images:
   bash hack/trivy-sentinel-images.sh
   ```
2. **Live cluster** — rebuild/load touched image(s), rollout, then:
   ```bash
   bash hack/validate-api-split-milestone.sh all   # or m13 | m14 | m15 | m16 | m2
   ```
   Or run both in one step:
   ```bash
   bash hack/milestone-gate.sh all
   ```

Requires `kind-mcp-runtime` with `mcp-platform-api`, `mcp-analytics-api`,
`mcp-runtime-control`, and the shrinking `mcp-sentinel-api` deployed. CI mirrors
the image scans in `.github/workflows/security-trivy.yaml` (`trivy-sentinel-images`
matrix). Extend the cluster script when M2+ land (ingress paths, UI proxy removal).
**Do not start the next milestone while Trivy or any cluster check fails.**

---

## 4. Verification matrix

| Layer | Command / check |
|-------|-----------------|
| Format | `gofmt -s -l .` empty |
| Build/vet | root + each `services/*`: `go build ./... && go vet ./...` |
| Unit | `go test ./pkg/platformauth/... ./pkg/apihttp/... -race`; each service `go test ./... -race` |
| Contract | cross-service JWT test (multi-`aud` accepted by all three; single-`platform` token rejected by runtime/analytics); route-ownership 404 tests; API-key→full-Principal regression test |
| Manifests/golden | `go test ./test/manifest/... ./internal/cli/setup/... ./test/golden/... -count=1` |
| forwardAuth | `go test ./test/manifest/...` after the host/suffix change |
| Live (Kind, `qa-cluster-bringup`) | `setup --test-mode`; `cluster doctor` green for all three; via Traefik exercise `/api/v1/auth/login` (platform), `/api/v1/stats` (analytics), `/api/v1/runtime/servers` (runtime), an end-to-end registry push, and `/api/v1/user/api-keys` vs `/api/v1/user/analytics/usage` to confirm the split prefix routes correctly; team-create rollback with an injected K8s failure |
| Security/UI | `qa-e2e-security` (auth/governance unchanged) and `qa-e2e-ui` (direct-routing auth flows) before deleting the UI proxy |
| Contract (M3) | each service builds against shared `pkg/internalapi` DTOs; OpenAPI spec-validation tests pass; breaking a field vs the committed `openapi.yaml` fails CI |

---

## 5. Docs/skills to refresh at cutover

`docs/sentinel.md`, `docs/internals/request-flows.md` (split the diagram participant into
three), `docs/security/authz-matrix.md`, troubleshooting runbooks, and skills
`mcp-runtime-troubleshooting`, `k3s-public-ops`, `qa-e2e-operations`, `k8s-hardening-audit`,
`supply-chain-audit` (three images, three SAs, one broad ClusterRole). Propose `ai-assist/`
notes for user review per `CLAUDE.md`.
