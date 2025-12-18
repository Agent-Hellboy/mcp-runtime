# MCP Runtime Platform

A complete platform for deploying and managing MCP (Model Context Protocol) servers. 

When working with large language models, context window limitations often require breaking monolithic services into multiple specialized MCP servers. Rather than paying for third-party gateway services that only provide basic routing, this platform offers a self-hosted solution that gives you full control.

The platform targets organizations that need to ship many MCP servers internally, maintaining a centralized registry where any team can discover and use available MCP servers across the company.

> ⚠️ **Caution**: This platform is currently under active development. APIs, commands, and behavior may change. Some features are "vibe-coded" and need thorough testing. Not recommended for production use yet. Contributions and feedback welcome!

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

### Code Structure

```
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
├── examples/            # Complete working examples
└── test/                # Test utilities and e2e tests
```

### Platform Overview

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Developer     │    │   CI/CD Runner  │    │   Kubernetes    │
│   Workstations  │    │                 │    │   Cluster       │
│                 │    │  1. Build Image │    │                 │
│  • VS Code      │────│  2. Push to     │────│  ┌─────────────┐ │
│  • Terminal     │    │     Registry    │    │  │  Registry   │ │
│                 │    │  3. Generate    │    │  │  (Docker)   │ │
└─────────────────┘    │     CRDs        │    │  └─────────────┘ │
                       │  4. Deploy      │    │                 │
                       └─────────────────┘    │  ┌─────────────┐ │
                                               │  │  Operator  │ │
                                               │  │  Controller│ │
                                               │  └─────────────┘ │
                                               │        │          │
                                               │        ▼          │
                                               │  ┌─────────────┐ │
                                               │  │ MCPServer   │ │
                                               │  │ Resources   │ │
                                               │  │ • Deployment│ │
                                               │  │ • Service   │ │
                                               │  │ • Ingress   │ │
                                               │  └─────────────┘ │
                                               └─────────────────┘
```

### Data Flow

1. **Metadata Definition** → Developer defines MCP servers in YAML
2. **Image Building** → CLI builds Docker images from Dockerfiles
3. **Registry Push** → Images pushed to internal/external registry
4. **CRD Generation** → CLI converts metadata to Kubernetes CRDs
5. **Operator Deployment** → Kubernetes operator watches for CRDs
6. **Resource Creation** → Operator creates Deployment, Service, Ingress
7. **Access** → Servers accessible via unified `/{name}/mcp` URLs

This README is the primary source of truth; no additional external docs are published.

## Prerequisites

### Required Tools

- **Go**: 1.21+ (for building CLI and operator)
  - Download: https://golang.org/dl/
  - Verify: `go version`

- **Make**: Build system (usually pre-installed on macOS/Linux)
  - macOS: `xcode-select --install`
  - Ubuntu: `sudo apt install build-essential`

- **kubectl**: Kubernetes CLI (must be configured for your cluster)
  - Install: https://kubernetes.io/docs/tasks/tools/
  - Verify: `kubectl cluster-info`

- **Docker**: Container runtime (for building/pushing images)
  - Alternatives: podman, nerdctl
  - Verify: `docker version`

### Cluster Requirements

- **Kubernetes**: 1.21+ with default StorageClass
  - Local: Minikube (`minikube start`), Kind (`kind create cluster`)
  - Cloud: GKE, EKS, AKS (configure kubectl access)

- **Ingress Controller**: Traefik (installed automatically) or alternatives
  - nginx-ingress, istio, etc.


### Registry

- **Default**: Platform deploys an internal registry automatically
- **External**: Use `mcp-runtime registry provision --url <registry>` before setup

```bash
# Push images to registry
mcp-runtime registry push --image my-app:latest
```

### Ingress

- **Default**: Traefik is installed automatically (HTTP mode)
- **TLS**: Use `mcp-runtime setup --with-tls` for HTTPS
- **Custom**: Use `--ingress none` if you have your own ingress controller

All MCP servers get routes at `/{server-name}/mcp` automatically.

### Defaults

The platform sets sensible defaults:
- Health checks (readiness/liveness probes)
- Resource limits (CPU/memory)
- Ingress routes

Override any defaults in your server metadata if needed.

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

### TLS (optional, internal CA)
- Install cert-manager (Helm or `kubectl apply`).
- Create an internal CA secret in the cert-manager namespace, e.g.:
  ```bash
  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager
  ```
- Apply the ClusterIssuer and a Certificate for the registry (examples in `config/cert-manager/`):
  ```bash
  kubectl apply -f config/cert-manager/cluster-issuer.yaml
  kubectl apply -f config/cert-manager/example-registry-certificate.yaml
  ```
- Run `mcp-runtime setup --with-tls` to install Traefik with HTTPS and the registry TLS ingress. Ensure DNS/hosts map your chosen hostnames to the Traefik Service.
- Distribute the CA to any clients/runners (Docker/skopeo/kaniko/kubectl) so pushes/pulls verify the registry and MCP servers.

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

## Examples

The `examples/` directory contains complete working examples:

### Basic MCP Server Example

```bash
cd examples/example-app

# Build the example app
docker build -t example-app:latest .

# Push to platform registry
mcp-runtime registry push --image example-app:latest

# Generate and deploy CRD
mcp-runtime pipeline generate --dir . --output manifests/
mcp-runtime pipeline deploy --dir manifests/

# Access your server
curl http://<ingress-host>/example-app/mcp
```

**What it does:**
- Simple HTTP server that responds with JSON
- Shows environment variable passing
- Demonstrates the complete MCP server deployment flow

### Metadata Configuration

The `examples/metadata.yaml` shows how to define multiple servers:

```yaml
version: v1
servers:
  - name: my-mcp-server
    route: /my-mcp-server/mcp
    port: 8088
    resources:
      limits:
        cpu: "500m"
        memory: "512Mi"
      requests:
        cpu: "100m"
        memory: "128Mi"
    envVars:
      - name: ENV_VAR_1
        value: "value1"
```

### Advanced Examples

- **`mcpserver-example.yaml`**: Direct CRD definition
- **`mcpservers-shared-host.yaml`**: Multiple servers sharing one ingress host
- **`example-app/`**: Complete MCP server implementation

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

## Troubleshooting

### Common Issues

-- Exploring it and will write it up in docs

### Getting Help

1. Check `mcp-runtime status` for overall platform health
2. View detailed logs: `kubectl logs -n <namespace> deployment/<name>`
3. Use verbose flags if available: `mcp-runtime --help`
4. Check GitHub issues for similar problems

### Debug Commands

```bash
# Platform status
mcp-runtime status

# Check all resources
kubectl get all -n mcp-runtime
kubectl get all -n registry
kubectl get all -n traefik

# View logs
kubectl logs -n registry deployment/registry --tail=50
kubectl logs -n mcp-runtime deployment/mcp-runtime-operator-controller-manager --tail=50

# Check events
kubectl get events -n mcp-runtime --sort-by='.lastTimestamp'
```

## Contributing

### For Kubernetes Newcomers

If you're new to Kubernetes like the maintainer, here are some helpful resources:
- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [Kubebuilder Book](https://book.kubebuilder.io/) - For understanding operators
- [kubectl Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/)
- [Kubernetes API Reference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/)

### Development Workflow

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Test locally: `make build && make test`
5. Format code: `make fmt`
6. Run linter: `make lint`
7. Submit a pull request with a clear description

### Code Style

- Use `go fmt` for formatting
- Follow standard Go naming conventions
- Add comments for exported functions
- Write tests for new functionality
- Keep PRs focused on single features

## How It Works

### Workflow

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

### Will support
- **Kubernetes**: 1.21+ (tested on 1.21-1.34)
- **Container Runtimes**: Docker, containerd
- **Architectures**: AMD64, ARM64

### Tested On
- **macOS**: Sonoma (M1/M4 chips) with Docker Desktop
- **Kubernetes Distributions**:
  - Minikube (Docker driver)
  - Kind
  - GKE, EKS, AKS (not tested)

### Known Limitations
- **HostPath Storage**: Not tested on managed kubernetes distribution , also s3 or other blob storage type for registry volume is not tested  
- **External Registries**: Some authentication methods not fully tested
- **Multi-cluster**: Not yet supported
