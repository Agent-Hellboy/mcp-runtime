# MCP Runtime Platform

A complete platform for deploying and managing MCP (Model Context Protocol) servers. 

When working with large language models, context window limitations often require breaking monolithic services into multiple specialized MCP servers. Rather than paying for third-party gateway services that only provide basic routing, this platform offers a self-hosted solution that gives you full control.

The platform targets organizations that need to ship many MCP servers internally, maintaining a centralized registry where any team can discover and use available MCP servers across the company.

## Overview

MCP Runtime Platform provides a streamlined workflow for teams to deploy a suite of MCP servers:
- **Define** server metadata in simple YAML files
- **Build** Docker images automatically from Dockerfiles
- **Deploy** via CLI or CI/CD - Kubernetes operator handles everything
- **Access** via unified URLs: `/{server-name}/mcp`

## Features

- **Complete Platform** - Internal registry deployment plus cluster setup helpers
- **CLI Tool** - Manage platform, registry, cluster, and servers
- **Automated Setup** - One-command platform deployment
- **CI/CD Integration** - Automated build and deployment pipeline
- **Kubernetes Operator** - Automatically creates Deployment, Service, and Ingress
- **Metadata-Driven** - Simple YAML files, no Kubernetes knowledge needed
- **Unified URLs** - All servers get consistent `/{server-name}/mcp` routes
- **Auto Image Building** - Builds from Dockerfiles and updates metadata automatically

## Architecture

```text
├── cmd/                 # Application entry points
│   ├── mcp-runtime/     # Platform management CLI
│   └── operator/        # Kubernetes operator
├── internal/            # Private application code
│   ├── operator/        # Kubernetes operator controller
│   └── cli/             # CLI command implementations
├── api/                 # Kubernetes API definitions (CRDs)
├── config/              # Kubernetes manifests
│   ├── crd/             # Custom Resource Definitions
│   ├── registry/        # Registry deployment
│   ├── rbac/            # RBAC configurations
│   └── manager/         # Operator deployment
├── docs/                # Internal notes (README is primary; no separate docs published)
└── examples/            # Example configurations
```

This README is the primary source of truth; no additional external docs are published.

## Prerequisites

- Go 1.21 or higher (needed to build the CLI and operator tools)
- Make (used by the provided Makefiles)
- kubectl configured for your cluster
- Docker (to build/push images and interact with the registry)
- Kubernetes cluster (1.21+) with a default StorageClass (registry PVC)
- Ingress controller: setup installs Traefik by default; install/configure another controller first if you prefer a different one
- Operator image handling: setup builds the operator image and auto-loads it into minikube/kind/k3d; for other clusters, ensure the image is in a pullable registry (use `OPERATOR_IMG` to override)
- Network access to fetch Go modules (for controller-gen/kustomize downloads)

### Registry options
- **Default (internal)**: `mcp-runtime setup` deploys an in-cluster registry in namespace `registry` (PVC-backed) and loads the operator image into the cluster.
- **Bring your own registry**: run `mcp-runtime registry provision --url <registry> [--username ... --password ...] [--operator-image <registry>/mcp-runtime-operator:latest]` before `mcp-runtime setup`. When a provisioned registry is configured, setup **does not create the internal registry**; it uses your registry instead and tags/pushes the operator image there. Make sure your cluster nodes can pull from it (configure imagePullSecrets if needed).

### Registry usage (quick guidance)
- **Pushing images**: use `mcp-runtime registry push --image <src> [--mode in-cluster|direct] [--registry ...] [--name ...]`. Default mode (`in-cluster`) spins up a helper pod with skopeo to push from inside the cluster; requires kubectl access to create pods in `registry` namespace. `direct` mode uses `docker push` from the runner/host and needs a reachable, trusted registry endpoint (Ingress/TLS or NodePort plus insecure-registry config).
- **Pulling images (pods)**: nodes must reach the registry. Prefer ClusterIP or a trusted ingress/TLS hostname. Service DNS (`*.svc`) is not used by the node runtime for image pulls. NodePort without TLS requires marking the registry as insecure on the node runtime.
- **MCPServer images**: set `spec.image` to the address nodes can reach (ClusterIP or trusted ingress). `spec.useProvisionedRegistry`/`spec.registryOverride` can rewrite to a provisioned registry, and the controller can auto-create a pull secret if provisioned creds are set and `imagePullSecrets` is empty.
- **Registry selection decisions**:
  - Per-server override (`spec.registryOverride`) is kept to let teams pull public images, migrate gradually, or target partner registries without mirroring everything.
  - Global/provisioned registry (`spec.useProvisionedRegistry` + operator env `PROVISIONED_REGISTRY_*`) is the “platform default” path; enables auto pull-secret creation.
  - If you want tighter control, layer policy (allowlist/denylist/webhook) rather than removing per-server override.
- **Image pull secrets**: Kubernetes stores registry creds in a `kubernetes.io/dockerconfigjson` Secret. The operator will auto-create/update one per namespace (default name `mcp-runtime-registry-creds`) and attach it to Deployments when `PROVISIONED_REGISTRY_URL/USERNAME/PASSWORD` are set and `spec.imagePullSecrets` is empty. If you need custom creds, set `spec.imagePullSecrets` on the MCPServer.

### Ingress behavior (Traefik by default)
- Ingress host is required. Set `spec.ingressHost` per MCPServer or configure a cluster-wide default via operator env `MCP_DEFAULT_INGRESS_HOST`. The operator will error if neither is set to avoid catch-all ingress collisions.
- The operator creates one Ingress per MCPServer with a default path of `/<name>/mcp`; set `spec.ingressPath` to override.
- Set a shared host once via operator env `MCP_DEFAULT_INGRESS_HOST` (e.g., `mcp.example.com`); any MCPServer without `spec.ingressHost` will inherit it. Per-CR values override the default.
- Traefik merges all matching Host/Path rules onto its single listener (80/443), so multiple MCPServers can share the same host with different paths.
- Make your hostname resolve to the ingress entrypoint (LB IP, node IP+NodePort, or your chosen exposure) so `http(s)://<host>/<name>/mcp` works from clients.
- `mcp-runtime setup --ingress traefik` installs the bundled Traefik controller by default using the secure overlay at `config/ingress/overlays/prod` (dashboard/API disabled). It skips install if an IngressClass already exists unless you add `--force-ingress-install`. Use `--ingress none` to skip installing a controller. For a dev-friendly dashboard, point `--ingress-manifest` to `config/ingress/overlays/dev` to enable `--api.insecure=true`.
- On-prem/no-cloud exposure options:
  - Install MetalLB and keep Traefik as `LoadBalancer`; MetalLB assigns a LAN IP—point DNS/hosts to it.
  - Switch Traefik Service to `NodePort` and use `http://<node-ip>:<nodePort>`; update DNS/hosts to the node IP.
- Dev-only: `kubectl port-forward -n traefik svc/traefik 18080:80` and curl `http://127.0.0.1:18080/...` with the correct Host header.

### MCPServer runtime defaults
- Pods get TCP readiness/liveness probes on `spec.port` and baseline resources (requests: `50m`/`64Mi`, limits: `500m`/`256Mi`) when none are set. Override with `spec.resources` if you need different sizing.

## Installation

### Install Platform CLI

```bash
# Clone repository
git clone https://github.com/Agent-Hellboy/mcp-runtime.git
cd mcp-runtime

# Install dependencies
make install

# Build runtime CLI
make build-runtime

# (Optional) Install globally
make install-runtime
```

## Quick Start (CLI flow)

Run these in order:

1) Build the CLI
```bash
make build-runtime
```
Builds the `mcp-runtime` binary into `./bin`.

2) (Optional) Install the CLI globally
```bash
make install-runtime
```
Copies `bin/mcp-runtime` to `/usr/local/bin` so it’s on your PATH.

3) Setup the platform (uses internal registry by default; skips it if you provision an external one)
```bash
mcp-runtime setup
```
Installs the CRD, creates the `mcp-runtime` namespace, deploys the registry, and deploys the operator if its image is present.
The registry uses a PersistentVolumeClaim by default; make sure your cluster has a default storage class or update `config/registry/pvc.yaml` with a specific `storageClassName`.

4) Check status
```bash
mcp-runtime status
```
Verifies cluster, registry, and operator readiness.

### 2. Deploy Your First MCP Server

```bash
# 1. Create metadata file (.mcp/metadata.yaml)
cat > .mcp/metadata.yaml <<EOF
version: v1
servers:
  - name: my-server
    route: /my-server/mcp
    port: 8088
EOF

# 2. Build image locally
docker build -t my-server:latest .

# 3. Push image to the platform/provisioned registry (retags automatically)
mcp-runtime registry push --image my-server:latest

# 4. Generate CRDs and deploy
mcp-runtime pipeline generate --dir .mcp --output manifests/
mcp-runtime pipeline deploy --dir manifests/
```

Your server will be available at: `http://<ingress-host>/my-server/mcp`

Use this README as the primary walkthrough.

## Usage

### Platform Management

```bash
# Setup complete platform
mcp-runtime setup

# Check platform status
mcp-runtime status
```

### Cluster Management

```bash
# Initialize cluster
mcp-runtime cluster init

# Check cluster status
mcp-runtime cluster status

# Provision cluster (Kind, GKE, EKS, AKS)
mcp-runtime cluster provision --provider kind --nodes 3
# Cloud providers are not automated yet; use gcloud/eksctl/az to create a cluster and then point kubectl at it
```

### Registry Management

```bash
# Check registry status
mcp-runtime registry status

# Show registry info
mcp-runtime registry info

# (Optional) Configure an external registry
mcp-runtime registry provision --url <registry> [--username ... --password ...]
```

### Build & Deploy

```bash
# Build image from Dockerfile
mcp-runtime server build my-server

# Build and push image (updates metadata)
mcp-runtime server build --push my-server

# Generate CRDs from metadata
mcp-runtime pipeline generate --dir .mcp --output manifests/

# Deploy CRDs to cluster
mcp-runtime pipeline deploy --dir manifests/
```

### Server Management

```bash
# List all servers
mcp-runtime server list

# Get server details
mcp-runtime server get my-server

# View server logs
mcp-runtime server logs my-server --follow

# Delete server
mcp-runtime server delete my-server
```

This README is the main reference; there is no separate published doc set.

## Development

### Code Generation

The operator uses kubebuilder/controller-gen to generate code. Before building, you need to generate:

#### Generate CRD Manifests

```bash
make -f Makefile.operator manifests
```

This generates:
- `config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml` - CRD definition

#### Generate DeepCopy Methods

```bash
make -f Makefile.operator generate
```

This generates:
- `api/v1alpha1/zz_generated.deepcopy.go` - Deep copy methods for Kubernetes runtime.Object

#### Generate Both

```bash
make -f Makefile.operator manifests generate
```

**Note:** Generated files (`api/v1alpha1/zz_generated.deepcopy.go` and CRD manifests) are committed to the repository. Regenerate them after modifying types in `api/v1alpha1/`.

### Building the Operator

For developers working on the operator itself:

```bash
# Build operator image
make -f Makefile.operator docker-build-operator IMG=your-registry/mcp-runtime-operator:latest

# Deploy operator manually (usually handled by mcp-runtime setup)
make -f Makefile.operator deploy IMG=your-registry/mcp-runtime-operator:latest
```

**Note:** End users should use `mcp-runtime setup` which handles operator deployment automatically. These commands are for developers modifying the operator code.

### Development Commands

```bash
# Run tests
make test

# Format code
make fmt

# Lint code (requires golangci-lint)
make lint

# Generate operator code
make -f Makefile.operator generate
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Run `make fmt && make lint && make test`
6. Submit a pull request

## How It Works

### Architecture

```text
┌─────────────────────────────────────────────────────────────┐
│  CI/CD Runner (GitHub Actions, GitLab CI, etc.)            │
│                                                             │
│  1. Checkout code                                          │
│  2. Build Docker image                                      │
│  3. Push to registry                                        │
│  4. Generate CRDs from metadata                            │
│  5. Apply CRDs to cluster via kubectl                      │
│     (uses kubeconfig from secrets)                         │
└──────────────────────┬──────────────────────────────────────┘
                       │ HTTPS API calls
                       │ (kubectl → Kubernetes API Server)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes Cluster (Remote - Cloud/On-Prem)               │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  API Server                                           │ │
│  │  • Receives CRD manifests from CI/CD                 │ │
│  │  • Stores CRDs in etcd                               │ │
│  └──────────────────┬──────────────────────────────────┘ │
│                     │                                      │
│                     │ Operator watches for changes        │
│                     ▼                                      │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  Operator (in cluster)                                │ │
│  │  • Watches MCPServer CRDs                             │ │
│  │  • Creates Deployment, Service, Ingress              │ │
│  └───────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  Resources Created by Operator                        │ │
│  │  ├─ Deployment (runs pods)                           │ │
│  │  ├─ Service (ClusterIP)                              │ │
│  │  └─ Ingress (external routes)                        │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

1. **Teams define metadata** - Simple YAML file with server configuration
2. **CLI builds images** - From Dockerfiles, pushes to registry, updates metadata
3. **CLI generates CRDs** - Converts metadata to Kubernetes Custom Resources
4. **Operator watches CRDs** - Automatically creates Deployment, Service, and Ingress
5. **Servers accessible** - Via unified URLs: `/{server-name}/mcp`

### Kubernetes Operator

The operator automatically manages MCP server deployments:
- Watches for `MCPServer` CRD instances
- Creates Deployment with specified replicas and resources
- Creates ClusterIP Service for internal communication
- Creates Ingress with unified route pattern

## CI/CD Integration

The platform integrates seamlessly with CI/CD pipelines:

```yaml
# Example GitHub Actions workflow
- name: Build and push
  run: mcp-runtime build push my-server --tag ${{ github.sha }}

- name: Deploy
  run: |
    mcp-runtime pipeline generate --dir .mcp --output manifests/
    mcp-runtime pipeline deploy --dir manifests/
```

Use these snippets in your pipeline of choice. A sample pre-check workflow exists at `.github/workflows/pre-check.yaml`.

## Status

### ✅ Completed

- Kubernetes operator for automatic deployment
- CI/CD pipeline integration
- Custom Resource Definition (CRD)
- Platform CLI with full feature set
- Container registry deployment
- Metadata-driven workflow
- Automatic image building and metadata updates
- Unified URL routing pattern

### 🚧 Future Enhancements

- API server for centralized control (optional)
- Multi-cluster support
- Advanced monitoring and observability
- Webhook validation
- Approval workflows

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Platform Support

Officially supported on:
under dev

Tested on:
- macOS Sonoma (M1/M4 chips)
- Ubuntu 20.04+ LTS
