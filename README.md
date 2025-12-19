# MCP Runtime Platform

[![Pre-Check](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/pre-check.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/pre-check.yaml)
[![Post-Merge Checks](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/post-merge.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/post-merge.yaml)
[![Coverage](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/Agent-Hellboy/mcp-runtime)
[![Go Report Card](https://goreportcard.com/badge/github.com/Agent-Hellboy/mcp-runtime)](https://goreportcard.com/report/github.com/Agent-Hellboy/mcp-runtime)

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

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Developer     │    │   CI/CD Runner  │    │   Kubernetes    │
│   Workstations  │    │                 │    │   Cluster       │
│                 │    │  1. Build Image │    │                 │
│  • VS Code      │────│  2. Push to     │────│  ┌─────────────┐│
│  • Terminal     │    │     Registry    │    │  │  Registry   ││
│                 │    │  3. Generate    │    │  │  (Docker)   ││
└─────────────────┘    │     CRDs        │    │  └─────────────┘│
                       │  4. Deploy      │    │                 │
                       └─────────────────┘    │  ┌─────────────┐│
                                              │  │  Operator   ││
                                              │  │  Controller ││
                                              │  └─────────────┘│
                                              │        │        │
                                              │        ▼        │
                                              │  ┌─────────────┐│
                                              │  │ MCPServer   ││
                                              │  │ Resources   ││
                                              │  │ • Deployment││
                                              │  │ • Service   ││
                                              │  │ • Ingress   ││
                                              │  └─────────────┘│
                                              └─────────────────┘
```


## Prerequisites

### Required

- Go 1.21+
- Make
- kubectl (configured for your cluster)
- Docker
- Kubernetes cluster (1.21+) with default StorageClass


### Registry

- **Default**: Platform deploys an internal registry automatically
- **External**: Use `mcp-runtime registry provision --url <registry>` before setup

```bash
# Push images to registry
mcp-runtime registry push --image my-app:latest
```

### Ingress

- **Default**: Traefik is installed automatically (HTTP mode)
- **TLS**: Use `mcp-runtime setup --with-tls` for HTTPS (see TLS section below)
- **Custom**: Use `--ingress none` if you have your own ingress controller

All MCP servers get routes at `/{server-name}/mcp` automatically.

### TLS Setup

To enable HTTPS, you need cert-manager and a CA secret:

```bash
# 1. Install cert-manager
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set crds.enabled=true

# 2. Create CA secret (use your own CA cert/key)
kubectl create secret tls mcp-runtime-ca \
  --cert=ca.crt --key=ca.key -n cert-manager

# 3. Run setup with TLS
mcp-runtime setup --with-tls
```

The `--with-tls` flag automatically:
- Applies ClusterIssuer and Certificate resources
- Configures Traefik with HTTPS
- Configures registry with TLS ingress

### Defaults

The platform sets sensible defaults:
- Health checks (readiness/liveness probes)
- Resource limits (CPU/memory)
- Ingress routes

Override any defaults in your server metadata if needed.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_DEPLOYMENT_TIMEOUT` | `5m` | Timeout for deployment readiness checks |
| `MCP_CERT_TIMEOUT` | `60s` | Timeout for TLS certificate issuance |
| `MCP_REGISTRY_PORT` | `5000` | Registry port for internal registry |
| `MCP_SKOPEO_IMAGE` | `quay.io/skopeo/stable:v1.14` | Skopeo image for in-cluster image transfers (useful for air-gapped environments) |
| `MCP_OPERATOR_IMAGE` | (auto) | Override operator image (bypasses build/push) |
| `MCP_DEFAULT_SERVER_PORT` | `8088` | Default container port for MCP servers |

Examples:
```bash
# Slow cluster - increase timeouts
MCP_DEPLOYMENT_TIMEOUT=10m mcp-runtime setup

# Air-gapped environment - use local skopeo image
MCP_SKOPEO_IMAGE=my-registry.local/skopeo:v1.14 mcp-runtime registry push myimage

# Use pre-built operator image
MCP_OPERATOR_IMAGE=ghcr.io/myorg/mcp-operator:v1.0 mcp-runtime setup
```


## Quick Start

```bash
# 1. Clone and build
git clone https://github.com/Agent-Hellboy/mcp-runtime.git
cd mcp-runtime
make install && make build-runtime

# 2. Setup platform
./bin/mcp-runtime setup

# 3. Check status
./bin/mcp-runtime status

# 4. Deploy your first server
cat > .mcp/metadata.yaml << 'EOF'
version: v1
servers:
  - name: my-server
    route: /my-server/mcp
    port: 8088
EOF

docker build -t my-server:latest .
./bin/mcp-runtime registry push --image my-server:latest
./bin/mcp-runtime pipeline generate --dir .mcp --output manifests/
./bin/mcp-runtime pipeline deploy --dir manifests/
```

Your server will be available at: `http://<ingress-host>/my-server/mcp`

For HTTPS, see the [TLS Setup](#tls-setup) section.

## Examples

See the `examples/` directory for complete working examples:
- `example-app/` - Simple HTTP server with environment variables
- `metadata.yaml` - Multi-server configuration
- `mcpserver-example.yaml` - Direct CRD definition

## CLI Reference

Run `mcp-runtime --help` for all available commands:

```bash
mcp-runtime setup      # Setup complete platform
mcp-runtime status     # Check platform health
mcp-runtime registry   # Registry management
mcp-runtime server     # Server management  
mcp-runtime pipeline   # Build/deploy pipelines
mcp-runtime cluster    # Cluster operations
```


## Development

### Code Structure

```
├── cmd/                 # CLI and operator entry points
├── internal/            # CLI and operator implementations
├── api/                 # Kubernetes CRD definitions
├── config/              # Kubernetes manifests
├── examples/            # Working examples
└── test/                # Tests
```

### Building

```bash
make test              # Run tests
make fmt               # Format code
make lint              # Lint code

# Operator development
make -f Makefile.operator manifests generate  # Regenerate CRDs
make -f Makefile.operator docker-build-operator IMG=<image>
```

### Testing

For e2e testing with pre-loaded images (kind/minikube):

```bash
# Build and load operator image into kind
docker build -t mcp-runtime-operator:latest -f Dockerfile.operator .
kind load docker-image docker.io/library/mcp-runtime-operator:latest --name <cluster>

# Run setup in test mode (skips operator build, uses pre-loaded image)
./bin/mcp-runtime setup --test-mode
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Run `make fmt && make lint && make test`
4. Submit a pull request

## Troubleshooting

```bash
# Check platform health
mcp-runtime status

# View logs
kubectl logs -n mcp-runtime deployment/mcp-runtime-operator-controller-manager
kubectl logs -n registry deployment/registry

# Check events
kubectl get events -n mcp-runtime --sort-by='.lastTimestamp'
```

## Status

### Completed
- Kubernetes operator, CLI, CRD, registry deployment
- Metadata-driven workflow with unified URL routing
- CI/CD integration

### Planned
- Multi-cluster support
- Advanced monitoring
- Webhook validation

## License

MIT License - see LICENSE file.

## Platform Support

**Tested on:** macOS (M1/M4), Minikube, Kind

**Known limitations:** HostPath storage and some external registry auth methods need more testing.
