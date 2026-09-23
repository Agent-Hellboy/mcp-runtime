---
name: k3s-public-ops
description: Operate or debug the MCP Runtime public k3s deployment and related hack scripts, including setup, clean/restore, rollout, registry TLS/auth, ImagePullBackOff, node DNS vs pod DNS, and post-change live validation. Use when touching hack/deploy/mcpruntime-org/, config/deployments/mcpruntime-org.env.example, public TLS registry behavior, or k3s deployment docs.
---

# k3s Public Ops

Use this skill for the `mcpruntime.org` style public k3s deployment. For
distribution-neutral target selection and kubeconfig setup, start with
[`docs/deployment-targets.md`](../../../docs/deployment-targets.md); this skill
and its scripts cover the tested public k3s implementation. Prefer documented
user-facing commands and scripts over private shortcuts.

## Source Of Truth

- Env profile: `config/deployments/mcpruntime-org.env`
- Example profile: `config/deployments/mcpruntime-org.env.example`
- Runbook: `docs/k3s-deployment-runbook.md`
- Readiness/debug guide: `docs/cluster-readiness.md`
- Scripts (canonical): `hack/deploy/mcpruntime-org/{setup,clean,restore,rollout,multitenancy-test}.sh`
- Script index: `hack/README.md`
- User path: `docs/quickstart.md` (published CLI install, hosted platform login,
  server publish, grant, adapter, and analytics UI)
- CLI release workflow: `.github/workflows/release.yaml`

## Current production VM

- SSH host: `root@${MCP_PRODUCTION_SSH_HOST}` from `config/deployments/mcpruntime-org.env`
- Preferred workstation key: `~/.ssh/id_ed25519`
- Production kubeconfig: use the team-shared file when provisioned; otherwise
  follow [Obtain and select cluster access](../../../docs/k3s-deployment-runbook.md#obtain-and-select-cluster-access)
  to retrieve it securely from the k3s server and validate the API endpoint,
  context, and TLS. Never assume a contributor temp path exists or commit
  kubeconfig material.

Use the VM password only for a one-time interactive SSH-key installation. Never
store that password in this skill, `AGENTS.md`, repository env files, shell
history, or command arguments. After key installation, verify:

```bash
source config/deployments/mcpruntime-org.env
ssh root@"${MCP_PRODUCTION_SSH_HOST}" 'hostname && kubectl config current-context'
```

Then copy or provision the approved kubeconfig locally, set `KUBECONFIG`, and
run `cluster doctor` before any production mutation.

## Non-Negotiables

- Before every production deployment, ask which MCP Runtime branch or ref to
  build and deploy. Show the current branch/ref and working-tree state as
  context, but do not silently choose `main` or the currently checked-out ref.
- Build `./bin/mcp-runtime` from the selected ref before setup, rollout, or
  validation. Confirm the binary was built from that ref before using it;
  stale CLI binaries can stamp stale registry image refs. Do not switch refs or
  discard local changes without the user's direction.
- For production image builds, use the workstation's selected Docker daemon
  and set `MCP_IMAGE_PLATFORM` to the target node architecture (currently
  `linux/amd64`). Keep `KUBECONFIG` on the shared production context. Use
  `MCP_REGISTRY_PUSH_MODE=public` to push images to
  `registry.<domain>/<image>:<unique-tag>`.
- mcp-auth is a separate release track. Leave its Deployment unchanged unless
  an update is requested. The default candidate source is the published Docker
  Hub image `docker.io/princekrroshan01/mcp-auth-server:latest`; copy it into
  the Runtime registry under a unique `MCP_AUTH_IMAGE_TAG`. Build from
  `/Users/proshan/mcp-auth` only when intentionally testing source changes;
  then require a selected `MCP_AUTH_BUILD_REF`, a clean checkout whose HEAD
  matches that ref, and `MCP_AUTH_IMAGE_SOURCE=local`. Both paths preserve the
  existing auth config, data PVC, signing key, and TLS Secret.
- CIMD support in mcp-auth is enabled by default. Set
  `MCP_AUTH_CLIENT_ID_METADATA_ENABLED=false` only when explicitly opting out.
  After rollout, verify the public authorization metadata advertises support
  and use a new or cleared Claude/Codex OAuth client entry so cached DCR
  credentials do not mask CIMD behavior. See the TypeScript SDK resource example
  in `examples/mcp-auth-sdk-typescript/`.
- Ask whether this rollout should update mcp-auth. If yes, ask whether to
  deploy published Docker Hub `latest` (recommended) or intentionally build a
  selected local mcp-auth ref for source testing. For local-source testing,
  inspect `/Users/proshan/mcp-auth`, check out the requested ref only with the
  user's direction, and require a clean worktree. For a published-image
  update, use `MCP_AUTH_IMAGE_SOURCE=published`; do not build the sibling repo.
  If no update is requested, retain the currently deployed mcp-auth image.
- Treat the selected Runtime ref and the mcp-auth build choice as required
  inputs to each production rollout, even when a user has already authorized
  deployment generally. Do not begin production mutation until both are clear.
- Keep candidate deployment and release publication separate. A platform image
  rollout does not publish new CLI binaries, and a GitHub CLI release does not
  deploy platform images. Do not tag/publish the Runtime or mcp-auth release
  until the candidate has passed the user path below and the user explicitly
  asks to publish it.
- Before running the deployment command, inspect its `--help`, the rollout
  script, and the production env profile, then ask about deployment flags or
  overrides that affect this rollout. Present only relevant choices together
  with the profile's current values and recommended defaults; do not invent
  values or silently override production settings. Wait for answers to any
  unresolved flag choices before mutating production.
- Preserve existing production certificates. Prefer the targeted rollout path
  for application image updates; do not run setup in a way that requests or
  reissues certificates as part of an ordinary deploy. Before any setup or TLS
  operation, inspect existing Certificate/Secret readiness and the configured
  ClusterIssuer, and retain the current issuer and TLS Secret references. Only
  request certificate issuance when the user explicitly asks for a TLS change
  or inspection shows issuance is necessary, and explain that consequence
  before proceeding.
- After rollout, verify the candidate through the documented customer journey:
  use the built CLI against `https://platform.mcpruntime.org` to log in,
  publish a uniquely named temporary `qa-audit-*` MCP server, grant an agent,
  call a tool through the adapter, and confirm the event in **Analytics →
  Tools** in the platform UI. Run browser checks for signed-out and signed-in
  UI surfaces when credentials are available. Clean up every temporary resource
  and report any skipped authenticated/browser flow as blocked; do not treat
  `cluster doctor` alone as release acceptance.
- The `test/e2e/production-remote.sh` and `production-vm.sh` suites reset and
  provision disposable VMs. Never point them at the `mcpruntime.org`
  production cluster; use targeted temporary user resources for production
  acceptance instead.
- `cluster doctor` uses `KUBECONFIG` env, not `--kubeconfig`:

```bash
KUBECONFIG="$HOME/.kube/config" ./bin/mcp-runtime cluster doctor
```

- For public bundled HTTPS, platform and tenant pull refs should use the
  TLS-covered hostname, for example `registry.mcpruntime.org/...`.
- Do not use a registry Service ClusterIP as a bundled-HTTPS pull ref unless
  the cert has the matching IP SAN. The usual symptom is:
  `x509: cannot validate certificate for <ClusterIP> because it doesn't contain any IP SANs`.
- Pod DNS and node image-pull DNS are different. Pods can resolve
  `registry.registry.svc.cluster.local`; k3s/containerd on the node usually
  cannot unless `/etc/rancher/k3s/registries.yaml` explicitly mirrors that
  exact host.
- Public registry ingress is auth-protected. Platform workloads using
  `registry.<domain>` need an image pull secret; setup should create and attach
  `mcp-runtime-registry-pull` for platform namespaces before registry auth is
  re-enabled. Unauthenticated pulls may fail with `no basic auth credentials`.

## Public k3s Setup Validation

Run the actual script path:

```bash
bash -n hack/deploy/mcpruntime-org/setup.sh
bash hack/deploy/mcpruntime-org/setup.sh
KUBECONFIG="$HOME/.kube/config" ./bin/mcp-runtime cluster doctor
kubectl --kubeconfig "$KUBECONFIG" get pods -A
```

Healthy setup signs:

- setup ends with `Platform setup complete`
- no `ErrImagePull` / `ImagePullBackOff`
- operator and Sentinel images use `registry.<domain>:<tag>`, not a Service IP
- operator and Sentinel workloads reference `mcp-runtime-registry-pull` when
  they pull from the public registry hostname
- `cluster doctor` passes all checks

## Clean / Restore Validation

`hack/deploy/mcpruntime-org/clean.sh --restore-platform` must work after setup.
It restores TLS and cert-manager runtime material only; it must not restore
tenant/user data.

Validate:

```bash
bash -n hack/deploy/mcpruntime-org/clean.sh
MCP_DEPLOY_ENV=config/deployments/mcpruntime-org.env \
  hack/deploy/mcpruntime-org/clean.sh --restore-platform
```

If restore hits Kubernetes metadata conflicts, sanitize backup manifests before
apply. Do not apply stale `resourceVersion`, `uid`, `managedFields`, or
`kubectl.kubernetes.io/last-applied-configuration`.

## Rollout Validation

`hack/deploy/mcpruntime-org/rollout.sh` is a live script. It must:

- rebuild `./bin/mcp-runtime`
- build API/UI images for `MCP_IMAGE_PLATFORM` using the workstation's
  selected Docker daemon
- push image blobs into the bundled registry (public mode uses the
  `registry.<domain>` hostname and the existing platform registry credential)
- optionally update mcp-auth only when requested: published Docker Hub image
  by default, or local source when intentionally testing a confirmed clean ref
  with `MCP_AUTH_IMAGE_SOURCE=local`
- deploy API/UI refs as `registry.<domain>/<repo>:<tag>`
- ensure `mcp-sentinel/mcp-runtime-registry-pull` exists and is attached to
  API/UI deployments
- finish both rollout status checks successfully

Run with a unique tag. For the public production cluster, set
`MCP_IMAGE_PLATFORM=linux/amd64`, set `MCP_REGISTRY_PUSH_MODE=public`, and
point `MCP_SETUP_KUBECONFIG` to the shared kubeconfig currently on the
`prod-mcp-runtime` context. The production profile remains the source for the
domain and other deployment settings.

The user-facing release check is separate from the rollout command. Follow
`docs/quickstart.md` with the candidate CLI, then verify the same server,
connect configuration, and Analytics → Tools output in the hosted UI. The
GitHub release workflow only publishes CLI binaries; do not publish a new CLI
or mcp-auth release until these checks pass.

Run with a unique tag:

```bash
MCP_ROLLOUT_TAG=verify-rollout-$(date +%m%d%H%M%S) \
  bash hack/deploy/mcpruntime-org/rollout.sh
```

Then verify:

```bash
kubectl --kubeconfig "$KUBECONFIG" \
  get deploy mcp-platform-api mcp-runtime-api mcp-analytics-api mcp-sentinel-ui -n mcp-sentinel \
  -o jsonpath='{range .items[*]}{.metadata.name}{"|"}{range .spec.template.spec.imagePullSecrets[*]}{.name}{","}{end}{"|"}{range .spec.template.spec.containers[*]}{.image}{";"}{end}{"|"}{.status.readyReplicas}{"/"}{.status.replicas}{"\n"}{end}'

KUBECONFIG="$KUBECONFIG" ./bin/mcp-runtime cluster doctor
```

## Registry Debug Shortcuts

Get admin key:

```bash
ADMIN_KEY="$(kubectl --kubeconfig "$KUBECONFIG" \
  get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d)"
```

Check public registry route:

```bash
curl -k -i -H "x-api-key: $ADMIN_KEY" https://registry.mcpruntime.org/v2/
curl -k -I -u "platform-service:$ADMIN_KEY" \
  https://registry.mcpruntime.org/v2/mcp-platform-api/manifests/<tag>
```

Expected:

- admin or Basic-auth request returns 200 for existing manifests
- no-auth public request returns 401/403
- Traefik 404 means ingress/router is wrong, not image data

## Multitenancy Script

`hack/deploy/mcpruntime-org/multitenancy-test.sh` is intentionally platform-API-only. It unsets
`KUBECONFIG`. Validate it with public endpoints:

This production QA must exercise `server build image`, `server push`, and
`server deploy` through the platform CLI. To additionally verify the
namespace-local image pull Secret after each deploy, run with
`VERIFY_DEPLOY_PULL_SECRET=1` and a production `KUBECONFIG`; the script keeps
KUBECONFIG unset for CLI calls and uses the saved path only for read-only checks.

```bash
PLATFORM_URL=https://platform.mcpruntime.org \
MCP_URL=https://mcp.mcpruntime.org \
REGISTRY_HOST=registry.mcpruntime.org \
ADMIN_EMAIL=admin@mcpruntime.org \
ADMIN_PASSWORD='...' \
hack/deploy/mcpruntime-org/multitenancy-test.sh
```

Use `SKIP_SETUP=1` only after the generated teams, users, servers, grants, and
sessions already exist for the selected `RUN_ID`.
