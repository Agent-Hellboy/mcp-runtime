# Gotchas

Non-obvious behavior that has already cost a session. New entries follow
`TEMPLATE.md`. Keep it tight; promote to `AGENTS.md` if it becomes
universal contributor knowledge.

---

### `CLAUDE.md` is a symlink to `AGENTS.md`

The repo root has `CLAUDE.md -> AGENTS.md`. Editing `CLAUDE.md` directly
works (follows the link), but the canonical filename is `AGENTS.md`.
Diffs and commits should reference `AGENTS.md`; PR reviewers will look
there. If a contributor adds `CLAUDE.md` as a separate file, that's a
bug — the symlink should be restored.

References:
- `CLAUDE.md` (symlink target)
- `AGENTS.md` (canonical)

Added: 2026-05-12

---

### Proxy sidecar reloads policy on a polling loop, not on apply

After `kubectl apply` of an `MCPAccessGrant` or `MCPAgentSession`,
`./bin/mcp-runtime server policy inspect` may already show the rendered
policy while the `mcp-gateway` sidecar is still on the prior version.
Allow ~6–10 seconds after the rendered policy reflects the change
before concluding a fresh `tools/call` failed for governance reasons —
the proxy reads the mounted file on a short poll interval.

References:
- `AGENTS.md` → **MCP server pod / sidecar checks**
- `docs/getting-started.md#3-contributor-test-mode-cluster` (the
  documented `sleep 6` after policy materialization)

Added: 2026-05-12

---

### `setup --test-mode` still builds and pushes images

Test mode is **not** a no-build shortcut. `./bin/mcp-runtime setup
--test-mode` builds and pushes the operator, gateway proxy, and Sentinel
images (with `latest` tags) to the configured or bundled registry, then
deploys pods that pull those images. Plan for the disk/CPU cost of a
full build whenever this command runs; don't expect "just deploy from
existing images."

References:
- `AGENTS.md` → **Local dev setup (Kind and CLI)**
- `docs/getting-started.md#3-contributor-test-mode-cluster`

Added: 2026-05-12

---

### Bundled registry TLS does not configure node trust

`setup --registry-mode bundled-https` makes the registry pod serve HTTPS
and renders platform image refs with a stable internal registry host, but
kubelet still pulls through the node's container runtime. Nodes must be
able to resolve or mirror that image host and trust the issuing CA; the
cluster-side Certificate and Service changes alone do not update
containerd, k3s, Docker, or host DNS.

References:
- `docs/cluster-readiness.md` → **Registry setup modes**
- `config/registry/overlays/internal-tls/`

Added: 2026-05-20

---

### Preserve subject.teamID on platform access apply

When `mcp-runtime access grant apply --file ...` or `session apply` goes
through the platform API, the CLI must copy `spec.subject.teamID` into the
API body. Cross-team adapter tests rely on an explicit foreign team subject;
dropping it makes the platform default back to the server-owning team and the
grant no longer proves delegated access.

References:
- `internal/cli/platformapi/client.go` (`CreateAccessGrant`, `CreateAgentSession`)
- `docs/multi-team.md` → **Public and cross-team access**

Added: 2026-05-20

---

### `applySetupPlanToCLIConfig` must not overwrite an already-resolved registry endpoint

`applySetupPlanToCLIConfig` (in `internal/cli/setup/platform/deploy.go`) sets
`core.DefaultCLIConfig.RegistryEndpoint` as part of applying the setup plan.
On k3s / TLS installs where `MCP_PLATFORM_DOMAIN` is set, `RegistryEndpoint`
is already resolved to `registry.<domain>` before this function runs. If the
function unconditionally overwrites it with `registry.registry.svc.cluster.local:5000`,
platform pods end up with an unresolvable DNS name and the setup fails.

**Fix:** Only overwrite `RegistryEndpoint` when it is still the default placeholder
(`"registry.local"` or empty). The `core.DefaultRegistryEndpoint` sentinel
exists precisely to detect "not yet configured" vs. "resolved by domain config."

References:
- `internal/cli/setup/platform/deploy.go` (`applySetupPlanToCLIConfig`)
- `internal/cli/setup/platform/platform_registry_resolve.go` (`resolveInternalPlatformRegistryURLClientGo`)
- `AGENTS.md` → **Debugging checklist** → Sentinel pods ImagePullBackOff

Added: 2026-05-24

---

### MCPServer pods use `mcp-workload` SA, not `default`

The operator's `ensureWorkloadServiceAccount` creates a `mcp-workload`
ServiceAccount in every MCPServer namespace. `ApplyRestrictedPodDefaults`
then sets `spec.serviceAccountName = mcp-workload` on every generated
Deployment. Any imagePullSecrets or RBAC that needs to reach operator-
managed pods must be on `mcp-workload`, not `default`.

**Gotcha:** patching pull secrets onto the `default` SA is silently ineffective
for MCPServer pods — they don't share the SA.

References:
- `pkg/kubeworkload/defaults.go` (`DefaultServiceAccountName`, `EnsureServiceAccount`)
- `internal/operator/deployment.go` (`ensureWorkloadServiceAccount`, `buildImagePullSecrets`)

Added: 2026-05-24

---

### Events API (`GET /api/v1/events`) is admin-only; server-events uses platform JWT

Two analytics query paths exist with different auth requirements:

- `GET /api/v1/events` — ClickHouse event log on analytics-api; requires an
  admin `x-api-key` or admin platform JWT.
- `GET /api/v1/runtime/server-events` — platform-layer events proxy; accepts
  the platform JWT (`Authorization: Bearer <token>`).

For scripts and tests that need to verify tool-call events, prefer
`/api/v1/runtime/server-events` so the caller only needs a platform login,
not direct access to cluster secrets.

References:
- `services/analytics-api/internal/analytics/handlers.go` (`Events`)
- `services/runtime-api/internal/runtimeapi/server_events.go` (`HandleRuntimeServerEvents`)
- `services/runtime-api/routes.go` route `/api/v1/runtime/server-events`

Added: 2026-05-24

---

### Bootstrap deadlock: platform pods must pull via ClusterIP, not public registry ingress

Using `registry.mcpruntime.org` (the auth-gated public ingress) as the
platform pod image pull URL causes a circular bootstrap deadlock on fresh
installs: kubelet can't pull the Sentinel API pod image because the
`registry-admin-auth@file` Traefik middleware calls `/api/v1/registry/authz`
on Sentinel API — which isn't running yet.

**Fix:** `resolveInternalPlatformRegistryURLClientGo` returns the ClusterIP
(`10.x.x.x:5000`) for platform pod pulls when `endpoint == ingressHost`.
Tenant MCPServer pods can use `registry.mcpruntime.org` because by the time
they are deployed, Sentinel is already running.

References:
- `internal/cli/setup/platform/platform_registry_resolve.go`
- `AGENTS.md` → **Debugging checklist** → Sentinel pods ImagePullBackOff

Added: 2026-05-24

---

### Bundled Go example image is distroless — no shell

`kubectl exec -it <pod> -c go-example-mcp -- /bin/sh` fails on the
bundled Go MCP example because the image is distroless. Same caveat
applies to several other runtime images. Use `kubectl logs`, `kubectl
describe`, or `kubectl debug --image=busybox:1.36 --target=<container>`
to inspect the pod namespace instead of expecting an interactive shell.

References:
- `examples/workspace-assistant-mcp/Dockerfile`
- `AGENTS.md` → **MCP server pod / sidecar checks**

Added: 2026-05-12

---

### Admin forward-auth probes need platform auth headers

When `/grafana` or `/prometheus` are protected by Traefik admin
forward-auth, E2E requests through the Sentinel gateway need the
platform `x-api-key` header even if the upstream service also has its
own auth, such as Grafana Basic auth. The UI `/auth/admin-check`
forward-auth endpoint must also skip HTTP-to-HTTPS redirects because
Traefik calls it by internal service DNS.

References:
- `services/ui/main.go:1156`
- `test/e2e/kind.sh:5213`
- `test/e2e/kind.sh:5256`

Added: 2026-05-15
