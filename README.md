# MCP Runtime Platform

[![CI](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml)
[![Gosec Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-gosec.yaml?branch=main&label=Gosec%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-gosec.yaml)
[![Trivy FS Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20FS%20Scan&job=Trivy%20FS%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20FS%20Scan)
[![Trivy Image Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20Image%20Scan&job=Trivy%20Operator%20Image%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20Operator%20Image%20Scan)
[![Coverage](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main)
[![Go Report Card](https://goreportcard.com/badge/github.com/Agent-Hellboy/mcp-runtime)](https://goreportcard.com/report/github.com/Agent-Hellboy/mcp-runtime)

Website: https://mcpruntime.org/  
Docs: https://mcpruntime.org/docs/  
API Reference: https://mcpruntime.org/docs/api

MCP Runtime is a self-hosted control plane for internal MCP servers on Kubernetes. The repo now covers metadata-driven deployment, operator reconciliation, optional gateway enforcement, dedicated access/session resources, registry workflows, and bundled audit/analytics through `mcp-sentinel`.

> Alpha status: APIs, commands, and behavior are still evolving. Use the docs and `v1alpha1` types as the source of truth. Not recommended for production yet.

## What ships now

- `mcp-runtime` CLI with `setup`, `status`, `registry`, `server`, `pipeline`, and `cluster`
- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` CRDs
- Operator-managed `Deployment`, `Service`, and `Ingress` resources
- Optional gateway sidecar for header-based identity, tool policy, trust, and audit emission
- Internal or provisioned registry workflows
- Bundled `mcp-sentinel` services for ingest, processing, API, UI, and observability

## Requirements

- Go `1.24+`
- `kubectl`
- Docker
- Make

## Architecture

```text
Developer / CI
      |
      v
+----------------------+
|   mcp-runtime CLI    |
+----------+-----------+
           |
           v
+----------------------+
|   v1alpha1 surface   |
|   MCPServer          |
|   MCPAccessGrant     |
|   MCPAgentSession    |
+----------+-----------+
           |
           v
+----------------------+        +----------------------+
| Operator + Registry  |------->| Deployments /        |
| + Ingress            |        | Services / Ingress   |
+----------+-----------+        +----------+-----------+
           |                               |
           | gateway enabled               | direct or gateway path
           v                               v
      +----------------+            /{server-name}/mcp
      | MCP proxy      |---------------------> MCP server
      | sidecar        |
      +--------+-------+
               |
               v
+------------------------------+
| mcp-sentinel                 |
| ingest | processor | API     |
| UI | gateway | metrics       |
+------------------------------+
```

## Quick start

```bash
make deps && make build-runtime

./bin/mcp-runtime setup
./bin/mcp-runtime status

# Optional:
# ./bin/mcp-runtime setup --with-tls
# ./bin/mcp-runtime setup --without-sentinel

./bin/mcp-runtime registry push --image my-server:latest
./bin/mcp-runtime pipeline generate --dir .mcp --output manifests/
./bin/mcp-runtime pipeline deploy --dir manifests/
```

Servers are exposed at `/{server-name}/mcp`.

## Key commands

```bash
./bin/mcp-runtime setup
./bin/mcp-runtime status
./bin/mcp-runtime registry
./bin/mcp-runtime server
./bin/mcp-runtime pipeline
./bin/mcp-runtime cluster
```

## Current scope

- Deployment, routing, grants, sessions, gateway policy, and audit/event flow are implemented in the current repo.
- `mcp-sentinel` services support bearer-token validation, but this is not a full OAuth 2.1 authorization server or Dynamic Client Registration implementation.

## Development

```bash
./hack/dev-setup.sh
make test
make operator-manifests operator-generate
```

Tested on macOS, Kind, and Minikube.

## License

MIT
