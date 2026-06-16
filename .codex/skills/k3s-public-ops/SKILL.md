---
name: k3s-public-ops
description: Operate or debug the MCP Runtime public k3s deployment and related hack scripts, including setup, clean/restore, rollout, registry TLS/auth, ImagePullBackOff, node DNS vs pod DNS, and post-change live validation. Use when touching hack/deploy/mcpruntime-org/, config/deployments/mcpruntime-org.env.example, public TLS registry behavior, or k3s deployment docs.
---

# k3s Public Ops

Use this skill for the `mcpruntime.org` style public k3s deployment. Prefer the
documented user-facing commands and scripts over private shortcuts.

## Source Of Truth

- Env profile: `config/deployments/mcpruntime-org.env`
- Example profile: `config/deployments/mcpruntime-org.env.example`
- Runbook: `docs/k3s-deployment-runbook.md`
- Readiness/debug guide: `docs/cluster-readiness.md`
- Scripts (canonical): `hack/deploy/mcpruntime-org/{setup,clean,restore,rollout,multitenancy-test}.sh`
- Script index: `hack/README.md`

## Non-Negotiables

- Rebuild `./bin/mcp-runtime` before setup or rollout validation. Stale CLI
  binaries can stamp stale registry image refs.
- `cluster doctor` uses `KUBECONFIG` env, not `--kubeconfig`:

```bash
KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml ./bin/mcp-runtime cluster doctor
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
KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml ./bin/mcp-runtime cluster doctor
kubectl --kubeconfig /private/tmp/mcpruntime-k3s.yaml get pods -A
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
- build API/UI images for `MCP_IMAGE_PLATFORM`
- push image blobs into the bundled registry
- deploy API/UI refs as `registry.<domain>/<repo>:<tag>`
- ensure `mcp-sentinel/mcp-runtime-registry-pull` exists and is attached to
  API/UI deployments
- finish both rollout status checks successfully

Run with a unique tag:

```bash
MCP_ROLLOUT_TAG=verify-rollout-$(date +%m%d%H%M%S) \
  bash hack/deploy/mcpruntime-org/rollout.sh
```

Then verify:

```bash
kubectl --kubeconfig /private/tmp/mcpruntime-k3s.yaml \
  get deploy mcp-platform-api mcp-runtime-control mcp-analytics-api mcp-sentinel-ui -n mcp-sentinel \
  -o jsonpath='{range .items[*]}{.metadata.name}{"|"}{range .spec.template.spec.imagePullSecrets[*]}{.name}{","}{end}{"|"}{range .spec.template.spec.containers[*]}{.image}{";"}{end}{"|"}{.status.readyReplicas}{"/"}{.status.replicas}{"\n"}{end}'

KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml ./bin/mcp-runtime cluster doctor
```

## Registry Debug Shortcuts

Get admin key:

```bash
ADMIN_KEY="$(kubectl --kubeconfig /private/tmp/mcpruntime-k3s.yaml \
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
