# MCP Runtime Platform

[![CI](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml)
[![Gosec Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-gosec.yaml?branch=main&label=Gosec%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-gosec.yaml)
[![Trivy FS Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20FS%20Scan&job=Trivy%20FS%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20FS%20Scan)
[![Trivy Image Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20Image%20Scan&job=Trivy%20Operator%20Image%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20Operator%20Image%20Scan)
[![Coverage](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main)
[![Go Report Card](https://goreportcard.com/badge/github.com/Agent-Hellboy/mcp-runtime)](https://goreportcard.com/report/github.com/Agent-Hellboy/mcp-runtime)

Website: https://mcpruntime.org/  
Docs: https://docs.mcpruntime.org/ (also browseable in [`docs/`](docs/))  
API Reference: https://docs.mcpruntime.org/api ([`docs/api.md`](docs/api.md))

MCP Runtime is a self-hosted control plane for internal MCP servers on Kubernetes. It provides metadata-driven deployment, operator reconciliation, optional gateway enforcement, dedicated access/session resources, registry workflows, and bundled audit/analytics services.

For local dev/debug quick steps, see `AGENTS.md`.

> Alpha status: APIs, commands, and behavior are still evolving. Use the docs and `v1alpha1` types as the source of truth. Not recommended for production yet.

## What ships now

- `mcp-runtime` CLI with `setup`, `status`, `registry`, `server`, `pipeline`, `cluster`, `access`, and `sentinel` commands
- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` CRDs
- Operator-managed `Deployment`, `Service`, and `Ingress` resources
- Optional gateway sidecar for header-based identity, tool policy, trust, and audit emission
- Internal or provisioned registry workflows
- Bundled services for ingest, processing, API, UI, and observability in `services/`
- Shared libraries for Kubernetes access, ClickHouse queries, and governance in `pkg/`
- Web-based dashboard with governance UI for grants, sessions, and component operations

## Requirements

- Go `1.24+`
- `kubectl`
- Docker
- Make

`mcp-runtime setup` provisions the bundled Sentinel stack, including ClickHouse and Kafka, for the default local and CI flow. You do not need to install those services separately before setup.

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
| Services Stack (services/)   |
| ingest | processor | API     |
| UI | gateway | observability |
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
./bin/mcp-runtime setup              # Install platform stack
./bin/mcp-runtime status            # Show platform health
./bin/mcp-runtime registry          # Manage registry and push images
./bin/mcp-runtime server            # Manage MCP server resources
./bin/mcp-runtime access            # Manage grants and sessions
./bin/mcp-runtime sentinel          # Operate services stack
./bin/mcp-runtime pipeline          # Generate and deploy manifests
./bin/mcp-runtime cluster           # Cluster operations
```

## Dashboard and Governance

The web UI (served by `services/ui`) provides:

- **Dashboard tab**: Overview metrics (total events, active servers, grants, sessions) and event stream
- **Governance tab**: Create/apply MCPAccessGrant and MCPAgentSession resources, then enable/disable grants and revoke/unrevoke sessions
- **Operations tab**: Component health view and safe restart actions

Access the dashboard at `/` after setup, with links to Grafana (`/grafana`) and Prometheus (`/prometheus`).

## API Endpoints

The API service (`services/api`) exposes:

**Analytics APIs:**
- `GET /api/events?limit=100`
- `GET /api/stats`
- `GET /api/sources`
- `GET /api/event-types`

**Dashboard API:**
- `GET /api/dashboard/summary` - Overview statistics

**Governance APIs:**
- `GET /api/runtime/servers` - MCP server deployments
- `GET /api/runtime/grants` - Access grants
- `GET /api/runtime/sessions` - Agent sessions
- `GET /api/runtime/components` - Component health
- `POST /api/runtime/grants` - Create or update an access grant
- `POST /api/runtime/sessions` - Create or update an agent session
- `POST /api/runtime/grants/{ns}/{name}/disable|enable`
- `POST /api/runtime/sessions/{ns}/{name}/revoke|unrevoke`
- `POST /api/runtime/actions/restart` - Safe component restart

## Current scope

- Deployment, routing, grants, sessions, gateway policy, audit/event flow are implemented
- Dashboard with governance UI for creating, applying, and toggling grants and sessions
- Services support bearer-token validation, but this is not a full OAuth 2.1 authorization server
- Shared libraries (`pkg/`) used by both CLI and API services

## Development

```bash
./hack/dev-setup.sh
make test
make operator-manifests operator-generate
```

### Building service images

```bash
# API service
docker build -f services/api/Dockerfile -t mcp-sentinel-api:latest .

# UI service
docker build -f services/ui/Dockerfile -t mcp-sentinel-ui:latest .

# Other services
docker build -f services/ingest/Dockerfile -t mcp-sentinel-ingest:latest .
docker build -f services/processor/Dockerfile -t mcp-sentinel-processor:latest .
```

Tested on macOS, Kind, and Minikube.

## License

MIT
