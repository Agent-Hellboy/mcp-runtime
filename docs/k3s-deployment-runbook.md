# MCP Runtime — k3s Deployment Runbook

Operational guide for deploying, re-deploying, and testing MCP Runtime on the
four-node k3s cluster with public DNS and Let's Encrypt TLS. Complements the
cluster-creation guide in [k3s-on-prem-cluster.md](k3s-on-prem-cluster.md).

## Reference cluster

A four-node k3s cluster: one control-plane node and three workers (one of which
has scheduling disabled). DNS wildcard `*.mcpruntime.org` points to the primary
worker node. Node names and IPs are internal — check your KUBECONFIG or
`kubectl get nodes` for the actual addresses.

## Prerequisites

```bash
# Verify KUBECONFIG
export KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml
kubectl get nodes

# Build the binary first (from repo root, on your workstation)
go build -o bin/mcp-runtime ./cmd/mcp-runtime
```

All setup commands below assume the repo root as the working directory and the
KUBECONFIG export is in your shell.

## Required environment variables

Saved deployment profile (committed template + local override):

```bash
cp config/deployments/mcpruntime-org.env.example config/deployments/mcpruntime-org.env
# edit mcpruntime-org.env — see Environment variable reference below
```

`config/deployments/mcpruntime-org.env` is gitignored. The `.example` file is the
team-shared template; your local `.env` holds workstation-specific paths.

The hack scripts under `hack/deploy/mcpruntime-org/` source this file by
default. Override the path with `MCP_DEPLOY_ENV=/path/to/other.env`. See
`hack/README.md` for the full layout.

### Environment variable reference

#### Cluster access and domain

| Variable | Required | Used by | Purpose |
|----------|----------|---------|---------|
| `KUBECONFIG` | yes | all hack scripts, manual `kubectl` | Path to the k3s kubeconfig. Must match the cluster you target. |
| `MCP_SETUP_KUBECONFIG` | yes (setup) | `hack/deploy/mcpruntime-org/setup.sh` | Same as `KUBECONFIG`; passed to `mcp-runtime setup --kubeconfig`. |
| `MCP_PLATFORM_DOMAIN` | yes | setup | Apex domain only (no `https://`). Derives `registry.`, `mcp.`, and `platform.` hostnames. |
| `MCP_PLATFORM_ADMIN_EMAIL` | yes (non-test setup) | setup | Seeds the platform admin account during bootstrap. |

#### Image build and registry pulls

| Variable | Required | Used by | Purpose |
|----------|----------|---------|---------|
| `MCP_IMAGE_PLATFORM` | strongly recommended | setup, rollout | Target OS/arch for images built on your workstation (for example `linux/amd64` when nodes are amd64). Omitting on an arm64 laptop builds images nodes cannot run. |
| `MCP_REGISTRY_ENDPOINT` | yes (`bundled-https`) | setup, rollout (via configmap patch) | Hostname nodes use to **pull** platform and tenant images. With public TLS, set to `registry.<domain>` — **not** the registry Service ClusterIP. |
| `MCP_REGISTRY_INGRESS_HOST` | optional | rollout, CLI build/push | Public registry hostname for `docker push` / `registry push`. Defaults from `MCP_PLATFORM_DOMAIN` when unset. |
| `MCP_REGISTRY_HOST` | do not set | — | Public ingress hostname; derived from `MCP_PLATFORM_DOMAIN`. Do not use as the internal pull URL. |
| `MCP_REGISTRY_INTERNAL` | optional | rollout | Override registry ClusterIP:port for **build/push** inside rollout script only. Pull path still uses `MCP_REGISTRY_ENDPOINT` in configmap. |

#### Setup behavior (read by `hack/deploy/mcpruntime-org/setup.sh`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `MCP_SETUP_WAIT_TIMEOUT` | `900` | Seconds to wait for setup rollouts. |
| `MCP_CERT_TIMEOUT` | `5m` (CLI default) | Certificate issuance wait on first install. Use `15m` on fresh clusters. |
| `MCP_SETUP_PLATFORM_MODE` | `tenant` | Passed to `setup --platform-mode`. |
| `MCP_SETUP_REGISTRY_MODE` | `bundled-https` | Passed to `setup --registry-mode`. |
| `MCP_SETUP_INGRESS` | `none` | `none` when k3s Traefik in `kube-system` already serves ingress. |
| `MCP_SETUP_TLS_CLUSTER_ISSUER` | `letsencrypt-prod` | ClusterIssuer name on reruns. **Do not** pass `--acme-email` when this issuer already exists. |
| `MCP_SETUP_SKIP_CERT_MANAGER_INSTALL` | unset | Set to `1` when cert-manager is already installed (typical reruns). |

#### k3s Traefik integration (written to `mcp-sentinel-config`)

| Variable | Typical value | Purpose |
|----------|---------------|---------|
| `PLATFORM_TRAEFIK_NAMESPACE` | `kube-system` | Namespace of the live Traefik deployment on k3s. |
| `PLATFORM_TEAM_TRAEFIK_WATCH` | `disabled` | Prevents `team create` from patching repo-managed `traefik/traefik` when k3s Traefik is external. |

#### Browser sign-in (public / tenant UI)

| Variable | Required | Purpose |
|----------|----------|---------|
| `GOOGLE_CLIENT_ID` | yes (public TLS) | Google OAuth client for dashboard sign-in. |
| `MCP_GOOGLE_CLIENT_ID` | optional | Alias for `GOOGLE_CLIENT_ID`. |
| `OIDC_ISSUER` | optional | Non-Google provider; setup fills Google defaults when `GOOGLE_CLIENT_ID` is set. |
| `OIDC_AUDIENCE` | optional | OIDC audience; defaults to Google client ID. |
| `OIDC_JWKS_URL` | optional | JWKS URL for token validation. |

#### Platform-runtime backup (`hack/deploy/mcpruntime-org/clean.sh`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `MCP_TLS_BACKUP_DIR` | `~/.mcpruntime/backups/mcpruntime-org` | Root directory for timestamped platform-runtime snapshots. |
| `MCP_RESTORE_TLS_AFTER_SETUP` | `1` | When `1`, `hack/deploy/mcpruntime-org/setup.sh` runs `hack/deploy/mcpruntime-org/restore.sh` after setup. |
| `MCP_DEPLOY_ENV` | `config/deployments/mcpruntime-org.env` | Env file path for all hack scripts. |

Backup scope is **platform-runtime state only** (TLS, cert-manager, OIDC,
bootstrap secrets — not tenant users, teams, MCP CRs, or registry images).

#### Rollout-only (`hack/deploy/mcpruntime-org/rollout.sh`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `MCP_ROLLOUT_TAG` | `verify-MMDDHHMM` | Image tag for API/UI build and push. |

#### Multitenancy test (`hack/deploy/mcpruntime-org/multitenancy-test.sh`)

These are **not** in the deployment profile — export them when running the test against production URLs:

| Variable | Example | Purpose |
|----------|---------|---------|
| `PLATFORM_URL` | `https://platform.mcpruntime.org` | Platform API base (no trailing slash). |
| `MCP_URL` | `https://mcp.mcpruntime.org` | Public MCP ingress base. |
| `REGISTRY_HOST` | `registry.mcpruntime.org` | Registry hostname for tenant image build/push. |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | test admin creds | Platform admin login when not using token. |
| `ADMIN_TOKEN` | optional | Admin API token instead of password login. |

The test script clears `KUBECONFIG` internally — tenant flows are platform-API-only.

#### Do not set on this TLS production cluster

| Variable | Why |
|----------|-----|
| `MCP_REGISTRY_ENDPOINT=10.x.x.x:5000` | ClusterIP breaks `bundled-https` TLS cert validation on pod pulls. |
| `MCP_ACME_EMAIL` on reruns | Re-applies Let's Encrypt issuer and can trigger duplicate-cert rate limits. Use `MCP_SETUP_TLS_CLUSTER_ISSUER` instead. |
| `MCP_RUNTIME_TEST_MODE=1` | Dev/test-mode guardrails; omit for production-shaped installs. |

#### Minimal profile example

```bash
export KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml
export MCP_PLATFORM_DOMAIN=mcpruntime.org
export MCP_IMAGE_PLATFORM=linux/amd64
export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com
export MCP_REGISTRY_ENDPOINT=registry.mcpruntime.org
export GOOGLE_CLIENT_ID=<google-oauth-client-id>
```

See `config/deployments/mcpruntime-org.env.example` for the full saved profile used by the hack scripts.

## Step 0: Back up platform-runtime state before any wipe

Let's Encrypt enforces a **5 duplicate-certificate / 7 days per domain** rate
limit. Use the helper script to back up platform-runtime material (TLS,
cert-manager ownership, OIDC, bootstrap secrets) before wiping app namespaces:

```bash
hack/deploy/mcpruntime-org/clean.sh --yes --wait
```

Tenant/user data (teams, Postgres identity store, MCP CRs, registry images) is
**not** preserved — platform-runtime state only. See
[Deployment Targets - k3s Production](deployment-targets.md#option-a-bundled-https-registry-on-prem-reference).

Manual TLS-only backup (legacy):

```bash
kubectl get secret registry-tls -n registry -o yaml \
  > /tmp/registry-tls-backup.yaml 2>/dev/null || true
kubectl get secret mcp-sentinel-platform-tls -n mcp-sentinel -o yaml \
  > /tmp/platform-tls-backup.yaml 2>/dev/null || true
```

Restore after setup (prefer automatic restore via `hack/deploy/mcpruntime-org/setup.sh`):

```bash
hack/deploy/mcpruntime-org/restore.sh
# or from clean.sh:
hack/deploy/mcpruntime-org/clean.sh --restore-platform
```

## Safe cluster wipe (app workloads only)

Deleting kube-system resources breaks k3s's reconciliation loop (CoreDNS,
Traefik, svclb-traefik, local-path-provisioner all become unrecoverable
without an SSH restart). Only delete app namespaces.

```bash
# 1. Back up TLS secrets (see Step 0)

# 2. Delete only app namespaces — leave kube-system untouched
kubectl get ns --no-headers \
  | awk '{print $1}' \
  | grep -Ev '^(kube-system|kube-public|kube-node-lease|default)$' \
  | xargs -r kubectl delete ns --grace-period=0

# 3. Delete cluster-scoped MCP resources
kubectl delete mcpserver,mcpaccessgrant,mcpagentsession \
  --all -A --ignore-not-found 2>/dev/null || true
kubectl delete clusterrole,clusterrolebinding \
  -l app.kubernetes.io/managed-by=mcp-runtime \
  --ignore-not-found 2>/dev/null || true
```

### If you accidentally wiped kube-system

If kube-system pods are gone (no CoreDNS, no Traefik), restart k3s on the
control plane to trigger full reconciliation from
`/var/lib/rancher/k3s/server/manifests/`:

```bash
ssh root@103.181.176.28 "systemctl restart k3s"
# Wait for CoreDNS, Traefik, and svclb pods to come up
kubectl wait pod -n kube-system \
  -l app.kubernetes.io/name=traefik \
  --for=condition=Ready --timeout=120s
```

Verify port 80 is reachable before running setup with TLS:

```bash
curl -sm5 http://registry.mcpruntime.org/ && echo "port 80 OK"
# Expected: "404 page not found" from Traefik
```

## Setup

### First install (creates Let's Encrypt ClusterIssuer and certificates)

```bash
cp config/deployments/mcpruntime-org.env.example config/deployments/mcpruntime-org.env
# add GOOGLE_CLIENT_ID to mcpruntime-org.env when browser sign-in is required

export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com

MCP_SETUP_WAIT_TIMEOUT=900 MCP_CERT_TIMEOUT=15m \
./bin/mcp-runtime setup \
  --kubeconfig /private/tmp/mcpruntime-k3s.yaml \
  --with-tls \
  --acme-email ops@example.com \
  --ingress none \
  --registry-mode bundled-https \
  --platform-mode tenant
```

### Reruns / upgrades (reuse existing certs — avoids LE rate limits)

When cert-manager already issued `registry-cert` and
`mcp-sentinel-platform-tls`, **do not** pass `--acme-email` again. Use the saved
profile and helper script:

```bash
hack/deploy/mcpruntime-org/setup.sh
```

For code-only changes (registry push, team create, API fixes) without a full
platform rebuild, use the targeted Sentinel rollout:

```bash
hack/deploy/mcpruntime-org/rollout.sh
```

That rebuilds/pushes `mcp-sentinel-api` and `mcp-sentinel-ui`, applies RBAC,
patches `mcp-sentinel-config` (`PLATFORM_TEAM_TRAEFIK_WATCH=disabled`,
`MCP_REGISTRY_ENDPOINT=registry.mcpruntime.org`), and waits for rollouts.

That sources `config/deployments/mcpruntime-org.env` (or the `.example` template)
and runs setup with `--tls-cluster-issuer letsencrypt-prod` and
`--skip-cert-manager-install`. Existing certificates stay on the same revision
when SANs are unchanged.

Equivalent manual command:

```bash
set -a && source config/deployments/mcpruntime-org.env && set +a
MCP_SETUP_WAIT_TIMEOUT=900 ./bin/mcp-runtime setup \
  --kubeconfig "$KUBECONFIG" \
  --with-tls \
  --tls-cluster-issuer letsencrypt-prod \
  --skip-cert-manager-install \
  --ingress none \
  --registry-mode bundled-https \
  --platform-mode tenant
```

**Why no `--test-mode`:** CI does not publish pre-built container images, so
every deployment builds operator/gateway/Sentinel images from the source tree
regardless. Without `--test-mode`, setup requires `MCP_PLATFORM_ADMIN_EMAIL`
to be explicitly set, but is otherwise identical. The only run-time effect of
`--test-mode` is setting `MCP_RUNTIME_TEST_MODE=1` inside deployed pods. For
a clean production deployment that avoids that flag, provide the admin email env
var above.

**Flag notes:**
- `MCP_PLATFORM_DOMAIN=mcpruntime.org` — derives `registry.`, `mcp.`, and
  `platform.` hostnames; do not also export a registry ClusterIP as
  `MCP_REGISTRY_ENDPOINT`.
- `MCP_PLATFORM_ADMIN_EMAIL` — required by non-test-mode setup validation;
  seeds the platform admin account in the `mcp-sentinel-secrets` Secret.
- `--ingress none` — k3s already runs Traefik in `kube-system`; avoids
  installing a second ingress stack. Setup sets `PLATFORM_TRAEFIK_NAMESPACE=kube-system`
  and `PLATFORM_TEAM_TRAEFIK_WATCH=disabled` so `team create` does not patch
  k3s Traefik (it watches ingresses cluster-wide).
- `--registry-mode bundled-https` — bundled registry with TLS ingress at
  `registry.mcpruntime.org`.
- `--tls-cluster-issuer letsencrypt-prod` (reruns) — reuses the existing
  ClusterIssuer; cert-manager keeps current certs when specs are unchanged.
- `--acme-email` (first install only) — creates/applies the Let's Encrypt
  ClusterIssuer; omit on reruns to avoid duplicate ACME orders.
- `MCP_CERT_TIMEOUT=15m` — extends the default 5-minute certificate-issuance
  wait on a fresh cluster.
- `--kubeconfig` — must be passed explicitly when multiple kubeconfig files
  exist on the workstation. The `KUBECONFIG` env var alone is not sufficient
  because TLS and cert-manager operations use a package-level client that
  requires the explicit path (see `internal/cli/setup/platform/kube_client.go`).

If setup reports "cert-manager already installed" but TLS issuance times out,
check two things: (1) port 80 is being served by Traefik; (2) cert-manager
pods are actually Running — the "already installed" check only tests for CRD
existence, not pod health. After a k3s restart the CRDs survive but pods may
be gone. Reinstall manually if needed:
```bash
kubectl get pods -n cert-manager
# If not running:
curl -sL https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml \
  | kubectl apply -f -
kubectl wait pod -n cert-manager --all --for=condition=Ready --timeout=120s
```

### Post-setup check

```bash
./bin/mcp-runtime cluster doctor

# Confirm TLS certs are Ready
kubectl get certificate registry-cert -n registry
kubectl get certificate -n mcp-sentinel

# Check Sentinel pods
kubectl get pods -n mcp-sentinel
```

Expected: all mcp-sentinel pods `1/1 Running`, certificate `READY=True`.

## Tenant push and deploy smoke test

After setup, verify a non-admin team member can publish and deploy:

```bash
ADMIN_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.ADMIN_API_KEYS}' | base64 -d | cut -d, -f1)"

# Admin: create team + user
MCP_PLATFORM_API_URL=https://platform.mcpruntime.org MCP_PLATFORM_API_TOKEN="$ADMIN_KEY" \
  ./bin/mcp-runtime team create myteam --name "My Team"
MCP_PLATFORM_API_URL=https://platform.mcpruntime.org MCP_PLATFORM_API_TOKEN="$ADMIN_KEY" \
  ./bin/mcp-runtime team user create myteam \
  --email member@example.com --password 'YourPassword123!' --role member

# Team member: login, build, push, deploy from metadata
MCP_PLATFORM_API_URL=https://platform.mcpruntime.org \
  ./bin/mcp-runtime auth login --email member@example.com --password 'YourPassword123!' \
  --profile myteam-user

cd examples/workspace-assistant-mcp
# .mcp/servers.yaml already exists in the example; for a new server run:
# ../../bin/mcp-runtime server init <name> --tool <tool> --metadata-dir .mcp

MCP_PLATFORM_API_URL=https://platform.mcpruntime.org MCP_PLATFORM_API_PROFILE=myteam-user \
  ../../bin/mcp-runtime server build image workspace-assistant-mcp \
  --metadata-dir .mcp \
  --tag verify-e2e \
  --platform linux/amd64

IMAGE_REF="$(awk '$1=="image:"{i=$2} $1=="imageTag:"{t=$2} END{print i ":" t}' .mcp/servers.yaml)"

MCP_PLATFORM_API_URL=https://platform.mcpruntime.org MCP_PLATFORM_API_PROFILE=myteam-user \
  ../../bin/mcp-runtime registry push --scope tenant --image "$IMAGE_REF"

MCP_PLATFORM_API_URL=https://platform.mcpruntime.org MCP_PLATFORM_API_PROFILE=myteam-user \
  ../../bin/mcp-runtime server deploy workspace-assistant-mcp \
  --scope tenant \
  --metadata-dir .mcp
```

Expected: push succeeds in under ~30s; deploy reports `status Ready`; the team
namespace contains `mcp-runtime-registry-pull` and a running MCPServer pod.
The `.mcp` metadata must contain `tools[*].sideEffect`; `server deploy` copies
that metadata into the platform request so governed `tools/call` requests can
authorize side effects.

If `team create` returns `500 failed to provision team namespace`, confirm
`PLATFORM_TEAM_TRAEFIK_WATCH=disabled` is present in `mcp-sentinel-config`
(or set it in `config/deployments/mcpruntime-org.env` before rerunning setup).

## Multi-tenancy end-to-end test

```bash
hack/deploy/mcpruntime-org/multitenancy-test.sh
```

Default assumptions:
- `PLATFORM_URL=https://platform.mcpruntime.org`
- `MCP_URL=https://mcp.mcpruntime.org`
- `REGISTRY_HOST=registry.mcpruntime.org` (image build tagging and push target resolution)
- Team owners push images with `registry push --scope tenant` (platform API), not `admin registry push`
- Builds and deploys `acme-tools`, `globex-tools`, and `techcorp-tools` example servers
- Creates Acme, Globex, and TechCorp teams, applies cross-tenant grants
- Verifies adapter success, dashboard events, and no-kubeconfig smoke checks

To skip the build/deploy and only verify an existing setup:

```bash
SKIP_SETUP=1 hack/deploy/mcpruntime-org/multitenancy-test.sh
```

## Troubleshooting

### TLS cert not issued after 5+ minutes

1. `kubectl describe challenge -A` — look for ACME HTTP-01 status
2. `kubectl logs -n cert-manager deploy/cert-manager --tail=60`
3. Check Traefik is serving port 80: `curl -sm5 http://mcp.mcpruntime.org/`
4. Verify DNS: `dig registry.mcpruntime.org +short` should return
   `103.181.177.16`
5. If a stale Certificate owns `registry/registry-tls`, delete it before
   rerunning setup:
   ```bash
   kubectl delete certificate registry-tls -n registry --ignore-not-found
   ```

### Setup fails "bundled registry platform setup requires MCP_REGISTRY_ENDPOINT"

You omitted `--test-mode` and used `--registry-mode auto`. For this k3s cluster
use `--registry-mode bundled-https` (included in `hack/deploy/mcpruntime-org/setup.sh`).
Do not export a ClusterIP as `MCP_REGISTRY_ENDPOINT` on the public TLS deployment.

### Setup fails "MCP_IMAGE_PLATFORM does not match Kubernetes node architecture"

Set `MCP_IMAGE_PLATFORM=linux/amd64` (cluster nodes are amd64; local Mac is arm64).

### kube-system empty / HelmChart CRD missing

See **If you accidentally wiped kube-system** above. Restart k3s on the control
plane; do not try to manually re-create the HelmChart CRDs.

### Namespaces stuck in Terminating

```bash
for ns in $(kubectl get ns --no-headers | awk '$2=="Terminating"{print $1}'); do
  kubectl get ns "$ns" -o json \
    | jq '.spec.finalizers = []' \
    | kubectl replace --raw "/api/v1/namespaces/$ns/finalize" -f -
done
```

### Let's Encrypt rate limit hit

Restore the backed-up TLS secrets (Step 0) instead of re-requesting certs:

```bash
kubectl apply -f /tmp/registry-tls-backup.yaml
kubectl apply -f /tmp/platform-tls-backup.yaml
```

Check current usage at <https://crt.sh/?q=mcpruntime.org>.
