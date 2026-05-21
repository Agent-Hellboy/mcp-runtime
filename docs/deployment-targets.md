# Deployment Targets

Use this guide when you know you want to deploy MCP Runtime, but still need to
choose the right Kubernetes target and install shape. It sits between
[Getting Started](getting-started.md) and
[Cluster Readiness](cluster-readiness.md):

- [Getting Started](getting-started.md) is the step-by-step install flow.
- This page explains which path to use for common self-managed and managed
  Kubernetes distributions.
- [Cluster Readiness](cluster-readiness.md) has the detailed registry,
  container runtime, DNS, ingress, TLS, and failure-mode checks.

`mcp-runtime setup` installs into an existing Kubernetes cluster. It does not
create EKS, GKE, AKS, k3s, or kubeadm clusters for you, and it does not modify
node container runtime trust unless a documented provider path says so.

## Common Deployment Model

Every distribution needs the same high-level shape:

1. Create or choose a Kubernetes cluster.
2. Configure `kubectl` for that cluster.
3. Make sure nodes can pull the registry host that MCP Runtime will use.
4. Decide ingress, DNS, TLS, storage, and image credential ownership.
5. Run `./bin/mcp-runtime bootstrap`.
6. Run `./bin/mcp-runtime setup` with the registry and TLS mode that matches
   the cluster.
7. Run `./bin/mcp-runtime cluster doctor`.
8. Deploy the first MCP server and verify the dashboard/API.

For production-like installs, prefer:

```bash
./bin/mcp-runtime setup --with-tls --strict-prod
```

Then make registry mode explicit:

```bash
# Existing managed or enterprise registry.
./bin/mcp-runtime setup \
  --registry-mode external \
  --external-registry-url registry.example.com \
  --with-tls \
  --strict-prod

# Bundled registry with internal HTTPS. Every node must trust the internal CA.
./bin/mcp-runtime setup \
  --registry-mode bundled-https \
  --with-tls \
  --strict-prod
```

## Choose a Target

| Target | Best use | Registry recommendation | Notes |
|---|---|---|---|
| kind | Contributor development, CI-like smoke tests, disposable clusters | Bundled HTTP registry with the documented kind mirror | Use [Contributor Local Kind](contributor/local-kind.md) and `setup --test-mode`. |
| Docker Desktop Kubernetes | Laptop demos and local evaluation | Bundled HTTP registry or Docker Desktop image loading | Good for local UI/API exploration, not production. |
| minikube | Laptop or VM evaluation | Insecure registry flag at cluster start, or `minikube image load` | Recreate minikube when changing insecure registry settings. |
| k3s | Single-node lab, edge, small self-managed clusters | Bundled registry for labs; external or bundled HTTPS for production | See the k3s examples below, the [k3s On-Prem Cluster](k3s-on-prem-cluster.md) runbook, and [Cluster Readiness - k3s](cluster-readiness.md#k3s). |
| kubeadm / vanilla Kubernetes | Self-managed production or staging | External registry, or bundled HTTPS with node CA trust | Configure containerd, DNS, ingress, storage, and TLS on every node. |
| RKE2 | Self-managed production or staging | External registry, or bundled HTTPS with node CA trust | Treat it like a hardened self-managed cluster; use provider tooling for runtime config. |
| EKS | AWS managed Kubernetes | ECR | Use AWS-managed node registry auth, a real ingress/load balancer, Route 53 or equivalent DNS, and cert-manager or enterprise TLS. |
| GKE | Google managed Kubernetes | Artifact Registry | Use node/workload identity registry access, Cloud DNS or equivalent DNS, and a Kubernetes ingress controller compatible with this platform. |
| AKS | Azure managed Kubernetes | ACR | Use AKS/ACR integration or pull secrets, Azure DNS or equivalent DNS, and a supported ingress/TLS path. |

OpenShift and other Kubernetes distributions are not a first-class documented
target yet. They can work only if the cluster can satisfy the same Kubernetes
contracts: CRDs, Deployments, Services, Ingress, storage, image pulls, TLS
secrets, and pod security requirements. Review the generated manifests and
platform security policy before using those clusters.

## Self-Managed Clusters

Self-managed clusters give you direct control over node runtime configuration.
That is useful for labs and edge clusters, but it also means you own every node
pull path.

### k3s Lab Example

Use this for a single-node k3s lab or internal evaluation with the bundled
plain HTTP registry. Do not copy the insecure registry settings into
production.

1. Install k3s and point your shell at its kubeconfig:

   ```bash
   export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
   kubectl get nodes
   ```

2. Preconfigure k3s containerd for the bundled registry NodePort:

   ```bash
   sudo tee /etc/rancher/k3s/registries.yaml >/dev/null <<'EOF'
   mirrors:
     registry.local:
       endpoint:
         - "http://127.0.0.1:32000"
   configs:
     "127.0.0.1:32000":
       tls:
         insecure_skip_verify: true
   EOF

   echo "127.0.0.1 registry.local" | sudo tee -a /etc/hosts
   sudo systemctl restart k3s
   ```

   Multi-node k3s needs equivalent registry mirror and host resolution on every
   node that can schedule MCP Runtime pods.

3. Build the CLI and run the k3s bootstrap apply path:

   ```bash
   make deps
   make build

   ./bin/mcp-runtime bootstrap --apply --provider k3s
   ```

   `bootstrap --apply --provider k3s` is the only automated prerequisite apply
   path today. It installs the bundled CoreDNS and local-path manifests when
   they are missing.

4. Install the platform:

   ```bash
   MCP_SETUP_WAIT_TIMEOUT=900 \
     MCP_REGISTRY_ENDPOINT=registry.local:32000 \
     ./bin/mcp-runtime setup
   ```

5. Validate the rollout:

   ```bash
   ./bin/mcp-runtime status
   ./bin/mcp-runtime cluster doctor
   kubectl get pods -n mcp-sentinel
   ```

If setup prints a different registry internal URL, copy that exact `host:port`
into `/etc/rancher/k3s/registries.yaml`, restart k3s, and rerun setup. k3s
containerd registry matching is exact.

### k3s Production-Style Shape

For a public or persistent k3s cluster, make it production-like:

```bash
export MCP_PLATFORM_DOMAIN=example.com
export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com
export GOOGLE_CLIENT_ID=<google-oauth-client-id>

./bin/mcp-runtime bootstrap --provider k3s
./bin/mcp-runtime setup \
  --registry-mode external \
  --external-registry-url registry.example.com \
  --with-tls \
  --acme-email ops@example.com \
  --strict-prod
```

Use `--tls-cluster-issuer <issuer-name>` instead of `--acme-email` when your
cluster already has an enterprise `ClusterIssuer`.

For a complete four-node reference topology, worker join commands, ServiceLB
pinning, public DNS, TLS, registry, validation, and a five-node extension, use
[k3s On-Prem Cluster](k3s-on-prem-cluster.md).

### kubeadm, RKE2, and Other Self-Managed Clusters

For self-managed production clusters:

- Use a stable registry endpoint that every node can resolve and trust.
- Configure containerd or your node runtime on every node pool.
- Use an ingress controller that creates Kubernetes `Ingress` routes, or run
  `setup --ingress none` only when equivalent ingress and registry auth are
  managed outside this repo.
- Install or verify a default `StorageClass`.
- Decide whether cert-manager, an enterprise issuer, or pre-created TLS secrets
  own platform certificates.

Then use the same production-style setup command:

```bash
./bin/mcp-runtime bootstrap
./bin/mcp-runtime setup --with-tls --strict-prod
./bin/mcp-runtime cluster doctor
```

## Managed Kubernetes

Managed clusters reduce node lifecycle work, but they do not remove registry,
DNS, TLS, or ingress decisions. In most managed environments, use an external
registry instead of the bundled registry.

### EKS

Recommended shape:

- Registry: ECR.
- Node pull auth: EKS node role, IRSA, or explicit image pull secrets.
- DNS: Route 53 or your enterprise DNS.
- Ingress: existing platform ingress controller, AWS Load Balancer Controller,
  ingress-nginx, or Traefik, as long as it supports the required Kubernetes
  `Ingress` routes and registry auth guard.
- TLS: cert-manager with Let's Encrypt or an enterprise issuer.

Setup shape:

```bash
export MCP_PLATFORM_DOMAIN=example.com
export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com
export GOOGLE_CLIENT_ID=<google-oauth-client-id>

./bin/mcp-runtime bootstrap
./bin/mcp-runtime setup \
  --registry-mode external \
  --external-registry-url <account>.dkr.ecr.<region>.amazonaws.com/mcp-runtime \
  --with-tls \
  --strict-prod
```

### GKE

Recommended shape:

- Registry: Artifact Registry.
- Node pull auth: Google-managed node identity or workload identity where
  appropriate.
- DNS: Cloud DNS or enterprise DNS.
- Ingress: GKE ingress, ingress-nginx, or Traefik, with equivalent registry
  auth if you do not use the repo-managed Traefik dynamic config.
- TLS: cert-manager with Let's Encrypt or an enterprise issuer.

Setup shape:

```bash
export MCP_PLATFORM_DOMAIN=example.com
export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com
export GOOGLE_CLIENT_ID=<google-oauth-client-id>

./bin/mcp-runtime bootstrap
./bin/mcp-runtime setup \
  --registry-mode external \
  --external-registry-url <region>-docker.pkg.dev/<project>/<repo> \
  --with-tls \
  --strict-prod
```

### AKS

Recommended shape:

- Registry: ACR.
- Node pull auth: AKS to ACR attachment, managed identity, or pull secrets.
- DNS: Azure DNS or enterprise DNS.
- Ingress: Application Gateway Ingress Controller, ingress-nginx, or Traefik,
  with equivalent registry auth if you replace repo-managed Traefik.
- TLS: cert-manager with Let's Encrypt or an enterprise issuer.

Setup shape:

```bash
export MCP_PLATFORM_DOMAIN=example.com
export MCP_PLATFORM_ADMIN_EMAIL=admin@example.com
export GOOGLE_CLIENT_ID=<google-oauth-client-id>

./bin/mcp-runtime bootstrap
./bin/mcp-runtime setup \
  --registry-mode external \
  --external-registry-url <registry>.azurecr.io/mcp-runtime \
  --with-tls \
  --strict-prod
```

## Ingress and Registry Ownership

MCP Runtime can install repo-managed Traefik, or it can reuse an existing
ingress controller. Avoid running two ingress stacks for the same public
surface.

If you bring your own ingress controller:

- Make sure it watches the namespaces MCP Runtime uses.
- Make sure it can serve `platform.<domain>`, `mcp.<domain>`, and
  `registry.<domain>` when `MCP_PLATFORM_DOMAIN` is set.
- Provide an equivalent registry auth guard before exposing
  `registry.<domain>` publicly. The repo-managed Traefik stack uses
  `registry-admin-auth@file` backed by `/api/registry/authz`.
- Use `./bin/mcp-runtime setup --ingress none ...` only when that external
  ingress path is already prepared.

## After Setup

Run the same checks on every distribution:

```bash
./bin/mcp-runtime status
./bin/mcp-runtime cluster doctor

kubectl get pods -n mcp-runtime
kubectl get pods -n mcp-sentinel
kubectl get ingress -A
```

For host-based public installs, also verify:

```bash
getent hosts registry.<domain>
getent hosts mcp.<domain>
getent hosts platform.<domain>

curl -k -I https://platform.<domain>/
curl -k -I -H "x-api-key: $ADMIN_API_KEY" https://registry.<domain>/v2/
```

Then continue with [Getting Started - Connect your first MCP
server](getting-started.md#8-connect-your-first-mcp-server).
