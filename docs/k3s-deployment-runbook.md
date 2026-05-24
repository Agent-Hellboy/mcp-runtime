# MCP Runtime — k3s Deployment Runbook

Operational guide for deploying, re-deploying, and testing MCP Runtime on the
four-node k3s cluster with public DNS and Let's Encrypt TLS. Complements the
cluster-creation guide in [k3s-on-prem-cluster.md](k3s-on-prem-cluster.md).

## Reference cluster

| Node | Role | Public IP |
|------|------|-----------|
| `mcp-platform-large` | k3s control-plane | 103.181.176.28 |
| `mcp-platform-live` | worker (DNS target) | 103.181.177.16 |
| `mcp-worker-small-1` | worker | 103.181.176.46 |
| `mcp-docs-site` | worker (scheduling disabled) | 103.182.102.123 |

DNS wildcard: `*.mcpruntime.org → 103.181.177.16`

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

```bash
export KUBECONFIG=/private/tmp/mcpruntime-k3s.yaml
export MCP_PLATFORM_DOMAIN=mcpruntime.org
export MCP_IMAGE_PLATFORM=linux/amd64   # workstation is arm64; nodes are amd64
```

`MCP_IMAGE_PLATFORM` is critical — omitting it causes setup to build arm64
images that k3s nodes (amd64) cannot run.

## Step 0: Back up TLS secrets before any wipe

Let's Encrypt enforces a **5 duplicate-certificate / 7 days per domain** rate
limit. Always save the existing TLS secrets before wiping the cluster so you
can restore them instead of requesting new ones.

```bash
# Run BEFORE any cluster wipe
kubectl get secret registry-tls -n registry -o yaml \
  > /tmp/registry-tls-backup.yaml 2>/dev/null || true
kubectl get secret mcp-sentinel-platform-tls -n mcp-sentinel -o yaml \
  > /tmp/platform-tls-backup.yaml 2>/dev/null || true
```

Restore after setup completes:

```bash
kubectl apply -f /tmp/registry-tls-backup.yaml 2>/dev/null || true
kubectl apply -f /tmp/platform-tls-backup.yaml 2>/dev/null || true
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

```bash
export MCP_PLATFORM_ADMIN_EMAIL=princekrroshan01@gmail.com
export MCP_REGISTRY_HOST=registry.mcpruntime.org

MCP_SETUP_WAIT_TIMEOUT=900 MCP_CERT_TIMEOUT=15m \
./bin/mcp-runtime setup \
  --with-tls \
  --acme-email princekrroshan01@gmail.com \
  --kubeconfig /private/tmp/mcpruntime-k3s.yaml
```

**Why no `--test-mode`:** CI does not publish pre-built container images, so
every deployment builds operator/gateway/Sentinel images from the source tree
regardless. Without `--test-mode`, setup requires `MCP_PLATFORM_ADMIN_EMAIL`
and `MCP_REGISTRY_HOST` to be explicitly set, but is otherwise identical. The
only run-time effect of `--test-mode` is setting `MCP_RUNTIME_TEST_MODE=1`
inside deployed pods. For a clean production deployment that avoids that flag,
provide the two extra env vars above.

**Flag notes:**
- `MCP_PLATFORM_ADMIN_EMAIL` — required by non-test-mode setup validation;
  seeds the platform admin account in the `mcp-sentinel-secrets` Secret.
- `MCP_REGISTRY_HOST=registry.mcpruntime.org` — tells setup (and k3s nodes)
  to pull platform images from the public registry hostname once TLS and the
  ingress are up. Ubuntu 24.04 nodes trust Let's Encrypt natively.
- `--with-tls` — installs cert-manager and provisions Let's Encrypt certs for
  `registry.mcpruntime.org` and `mcp.mcpruntime.org`.
- `MCP_CERT_TIMEOUT=15m` — extends the default 5-minute certificate-issuance
  wait. Useful on a fresh cluster where Traefik just started.
- `--kubeconfig` — must be passed explicitly when multiple kubeconfig files
  exist on the workstation. The `KUBECONFIG` env var alone is not sufficient
  because TLS and cert-manager operations use a package-level client that
  requires the explicit path (see code fix in `fix(setup)` PR).

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

## Multi-tenancy end-to-end test

```bash
hack/multitenancytest.sh
```

Default assumptions:
- `PLATFORM_URL=https://platform.mcpruntime.org`
- `MCP_URL=https://mcp.mcpruntime.org`
- Builds and deploys `acme-tools` and `globex-tools` example servers
- Creates Acme and Globex teams, applies a cross-tenant grant
- Verifies adapter success, dashboard events, and `cluster doctor`

To skip the build/deploy and only verify an existing setup:

```bash
SKIP_SETUP=1 hack/multitenancytest.sh
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

You omitted `--test-mode`. Without it, setup enforces a pre-configured registry
endpoint. For source-built deployments on k3s use `--test-mode`.

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
