# MCP Runtime Platform

[![CI](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml)
[![Gosec Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-gosec.yaml?branch=main&label=Gosec%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-gosec.yaml)
[![Trivy FS Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20FS%20Scan&job=Trivy%20FS%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20FS%20Scan)
[![Trivy Image Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20Image%20Scan&job=Trivy%20Operator%20Image%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20Operator%20Image%20Scan)
[![Coverage](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main)
[![Go Report Card](https://goreportcard.com/badge/github.com/Agent-Hellboy/mcp-runtime)](https://goreportcard.com/report/github.com/Agent-Hellboy/mcp-runtime)

MCP Runtime is a self-hosted Kubernetes control plane for internal Model Context Protocol servers. It provides declarative MCP server deployment, registry workflows, operator reconciliation, request-path governance, access/session resources, audit, analytics, dashboards, and a marketplace-style platform surface for browsing and operating MCP servers.

The public platform at `platform.mcpruntime.org` shows the hosted experience. Companies can deploy the same model in their own Kubernetes clusters and operate it through both the CLI and the platform UI.

- Website: https://mcpruntime.org/
- Platform: https://platform.mcpruntime.org/ for the public marketplace-style experience; companies can deploy the same platform model in their own clusters
- Docs: https://docs.mcpruntime.org/ and [`docs/`](docs/)
- API reference: https://docs.mcpruntime.org/api and [`docs/api.md`](docs/api.md)

> Alpha status: APIs, commands, and behavior are still evolving. Use the docs, CRDs, and `api/v1alpha1` types as the source of truth.

## What ships

- `mcp-runtime` CLI for `setup`, `status`, `registry`, `server`, `pipeline`, `cluster`, `access`, and `sentinel`
- Platform UI for browsing MCP servers, viewing platform state, and operating the stack through a web interface
- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` CRDs
- Kubernetes operator for `Deployment`, `Service`, `Ingress`, and policy materialization
- Internal or provisioned registry workflows
- Optional gateway enforcement for identity, tool policy, trust, and audit emission
- Bundled Sentinel stack for ingest, processing, API, UI, and observability

## Requirements

Host tools:

- Go `1.25+`
- Make
- Docker or a Docker-compatible client, with the daemon running
- `kubectl` on `PATH`, configured for the target cluster
- `curl`, `jq`, and `python3` for documented dev and traffic-generation flows
- `kind` for local Kind-based clusters

Cluster prerequisites:

- A running Kubernetes cluster: kind, k3s, minikube, Docker Desktop Kubernetes, EKS, or equivalent
- Working DNS, default storage class, ingress, and load-balancing path for your distribution
- See [`docs/cluster-readiness.md`](docs/cluster-readiness.md) before running production-like installs

`mcp-runtime setup` installs the platform stack, including Sentinel services such as ClickHouse and Kafka. You do not install those separately for the default flow.

## Quick start

```bash
make deps-install              # best-effort host install where supported
STRICT_DEPS_CHECK=1 make deps-check
make deps
make build

./bin/mcp-runtime bootstrap
./bin/mcp-runtime setup
./bin/mcp-runtime status
```

Notes:

- `make deps-install` is best-effort. It cannot start Docker Desktop, create cloud credentials, or configure kubeconfig for you.
- `make deps` checks host tools and downloads Go modules. It does not create a Kubernetes cluster.
- `make build` produces `./bin/mcp-runtime`.

## Common commands

```bash
./bin/mcp-runtime bootstrap              # preflight cluster prerequisites
./bin/mcp-runtime setup                  # install platform stack
./bin/mcp-runtime status                 # show platform health
./bin/mcp-runtime registry status        # inspect registry
./bin/mcp-runtime server status          # inspect MCP servers
./bin/mcp-runtime access grant list      # inspect access grants
./bin/mcp-runtime sentinel status        # inspect Sentinel stack
```

## Development checks

```bash
gofmt -s -l .
go build -o bin/mcp-runtime ./cmd/mcp-runtime
go test ./... -count=1 -race
go vet ./...
```

For targeted tests, e2e setup, and debugging runbooks, use [`AGENTS.md`](AGENTS.md) and the docs site.

## Agent tool configuration

The repo keeps Claude-specific local configuration in [`.claude/`](.claude/README.md). Its `skills` entry is expected to be a symlink to `../.codex/skills`, so Claude Desktop and the Codex CLI discover the same repository skills during local development.

## License

MIT
