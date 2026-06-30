# MCP Runtime Platform

[![CI](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml/badge.svg)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/ci.yaml)
[![Gosec Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-gosec.yaml?branch=main&label=Gosec%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-gosec.yaml)
[![Gitleaks Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-gitleaks.yaml?branch=main&label=Gitleaks%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-gitleaks.yaml)
[![Trivy FS Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20FS%20Scan&job=Trivy%20FS%20Scan)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20FS%20Scan)
[![Trivy Image Scan](https://img.shields.io/github/actions/workflow/status/Agent-Hellboy/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20Image%20Scan&job=Trivy%20operator%20Image)](https://github.com/Agent-Hellboy/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+event%3Apush)
[![Coverage](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/Agent-Hellboy/mcp-runtime/branch/main)
[![Go Report Card](https://goreportcard.com/badge/github.com/Agent-Hellboy/mcp-runtime)](https://goreportcard.com/report/github.com/Agent-Hellboy/mcp-runtime)

MCP Runtime is a self-hosted Kubernetes control plane for internal [Model Context Protocol](https://modelcontextprotocol.io/) servers. It provides declarative MCP server deployment, registry workflows, operator reconciliation, request-path governance, access/session resources, audit, analytics, dashboards, and a platform control surface for browsing and operating MCP servers.

The public platform at `platform.mcpruntime.org` is a live preview of the deployable platform experience. It runs the public preview catalog mode, where visitors can browse public preview MCP servers and signed-in preview users can publish into the public catalog namespace. It is still not a general-purpose public MCP marketplace. Companies can deploy the same model in their own Kubernetes clusters, then host, manage, govern, and audit MCP servers through both the CLI and the platform control surface for agents, IDEs, and direct human workflows.

- [Website](https://mcpruntime.org/)
- [Platform preview](https://platform.mcpruntime.org/) for the platform control surface; companies can deploy the same model in their own clusters
- [Docs](https://docs.mcpruntime.org/) and [`docs/`](docs/)
- [API reference](https://docs.mcpruntime.org/api) and [`docs/api.md`](docs/api.md)
- [Articles](https://articles.mcpruntime.org/) and [`articles/`](articles/)
- Early adopters: MCP Runtime is looking for teams running or evaluating internal MCP platforms. Open a [GitHub issue](https://github.com/Agent-Hellboy/mcp-runtime/issues) with your use case, cluster shape, or integration feedback.

> [!CAUTION]
> MCP Runtime is alpha software. APIs, commands, and behavior are still evolving. Use the docs, CRDs, and `api/v1alpha1` types as the source of truth before production use.

## Why teams use MCP Runtime

- **Operate MCP servers where company data already lives.** Deploy into an existing Kubernetes cluster instead of sending internal tools, tokens, or traffic through a third-party catalog or hosted proxy.
- **Use Kubernetes as the source of truth.** `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` resources make server delivery, access grants, agent sessions, policy, rollout, and status inspectable with normal Kubernetes workflows.
- **Move beyond "connect an agent to a URL."** The gateway can enforce identity, deny-by-default tool policy, trust ceilings, side-effect limits, session expiry, revocation, and audit emission on the live MCP request path.
- **Give agents a clean integration path.** The stdio and Streamable HTTP adapters let IDEs, agent frameworks, and scripts attach platform-issued governance identity without each client reimplementing grants or session handling.
- **Support internal catalog models.** Run private tenant namespaces, an org-wide catalog, or a public preview-style catalog while keeping the same CLI, CRDs, platform UI, and operator model.
- **Separate teams without separate platforms.** Team namespaces, RBAC, `teamID`, subject matching, and namespace-scoped grants/sessions let multiple teams publish and govern MCP servers on one cluster.
- **Own the day-two path.** Setup, registry workflows, image pull wiring, ingress, rollout readiness, `cluster doctor`, status commands, dashboards, audit, analytics, and Sentinel services are part of the platform rather than afterthoughts.
- **Fit different cluster shapes.** The documented paths cover disposable Kind development, laptop evaluation, k3s labs, self-managed production clusters, and managed Kubernetes with external registries.

## What ships

- `mcp-runtime` CLI for `setup`, `status`, `registry`, `server`, `cluster`, `access`, and `sentinel`
- `mcp-runtime adapter proxy` and `mcp-runtime adapter stdio` subcommands for
  governed HTTP and stdio agent integrations. Both can fetch identity from
  the platform with `--server <name> --agent <id> [--auto-refresh]`, or
  accept explicit `MCP_RUNTIME_*` env vars (see
  [Agent Adapters](docs/agent-adapters.md))
- Platform UI for authenticated MCP catalog browsing, platform state, and web operations
- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` CRDs
- Kubernetes operator for `Deployment`, `Service`, `Ingress`, and policy materialization
- Internal or provisioned registry workflows
- Optional gateway enforcement for identity, tool policy, trust, and audit emission
- Bundled Sentinel stack for ingest, processing, API, UI, and observability

## How it differs from MCP directories

The [Official MCP Registry](https://registry.modelcontextprotocol.io/) and public MCP directories such as [Glama](https://glama.ai/mcp), [Smithery](https://smithery.ai/), [Docker MCP Catalog on Docker Hub](https://hub.docker.com/mcp), [PulseMCP](https://www.pulsemcp.com/), [mcp.so](https://mcp.so/), and client-specific catalogs are useful discovery and installation surfaces. MCP Runtime is different: it is a deployable operating layer for running MCP servers inside a company's own environment. It can provide an internal catalog-like view, but the main product is deployment, governance, brokered access, audit, compliance evidence, and day-two operations.

| Public MCP directory or catalog | MCP Runtime |
|---|---|
| Helps users find or install public MCP servers | Helps companies host, deploy, govern, observe, and audit their own MCP servers |
| Optimizes for discovery metadata, popularity, and install snippets | Optimizes for deployment, runtime governance, Kubernetes reconciliation, policy, sessions, audit, and compliance |
| Usually runs as a third-party hosted directory or client feature | Runs in the company's Kubernetes environment or in a hosted preview shape |
| Stops at configuration or connection | Owns the governed request path through the broker/gateway |

## How it differs from open-source MCP gateways

Open-source MCP gateways such as [agentgateway](https://github.com/agentgateway/agentgateway), [Microsoft MCP Gateway](https://github.com/microsoft/mcp-gateway), [IBM ContextForge](https://github.com/IBM/mcp-context-forge), [MCPJungle](https://github.com/mcpjungle/MCPJungle), [Unla](https://github.com/AmoyLab/Unla), [OpenZiti MCP Gateway](https://github.com/openziti/mcp-gateway), [Docker MCP Gateway](https://github.com/docker/mcp-gateway), [Obot](https://github.com/obot-platform/obot), [Preloop](https://github.com/preloop/preloop), and [Agentic Community MCP Gateway & Registry](https://github.com/agentic-community/mcp-gateway-registry) are useful references, and several are more than thin proxies. The real difference is not "gateway versus platform" in the abstract; it is where the source of truth lives.

MCP Runtime's main distinction is simplicity around Kubernetes-native control-plane management. `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` are managed as first-class Kubernetes state instead of only gateway configuration; the operator reconciles workloads, services, ingress, registry image references, gateway policy ConfigMaps, and readiness status; the gateway enforces the rendered grant/session policy and emits audit/analytics events into the Sentinel stack.

This comparison was reviewed on June 4, 2026 with AI assistance against the referenced open-source repositories and local clones in `inspirations/`.

| Project | What it is strong at | How MCP Runtime is different |
|---|---|---|
| [agentgateway](https://github.com/agentgateway/agentgateway) | High-performance Rust data plane for MCP, A2A, LLM traffic, Gateway API, CEL policy, auth, and observability. | MCP Runtime is not trying to be a general AI traffic gateway. It makes MCP server delivery and governance Kubernetes-native through `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession`, then renders gateway policy from that cluster state. |
| [Microsoft MCP Gateway](https://github.com/microsoft/mcp-gateway) | Kubernetes-oriented MCP reverse proxy and management layer with adapter/tool CRUD APIs, StatefulSet deployment, session-aware routing, and Entra/RBAC integration. | MCP Runtime exposes MCP lifecycle and access as CRDs instead of only management API resources: server workload, route, tools, grants, sessions, policy, rollout, and status are inspectable and reconcilable with normal Kubernetes workflows. |
| [IBM ContextForge](https://github.com/IBM/mcp-context-forge) | Broad registry/proxy/federation platform for MCP, A2A, REST, and gRPC, with plugins, API virtualization, admin UI, and OpenTelemetry integrations. | MCP Runtime is narrower and more Kubernetes-operational: it focuses on deploying and running internal MCP server workloads, wiring ingress/registry/policy, and enforcing grant/session decisions on those workloads rather than virtualizing every API protocol. |
| [MCPJungle](https://github.com/mcpjungle/MCPJungle) | Self-hosted team gateway with one MCP endpoint, server registration, unified discovery, tool groups, access-control hooks, and observability. | MCP Runtime treats each governed server and access decision as durable cluster state. The operator owns workloads, Services, Ingress, gateway policy materialization, and readiness status, while grants and sessions stay as first-class Kubernetes objects. |
| [Unla](https://github.com/AmoyLab/Unla) | Lightweight, configuration-driven MCP/API gateway with API-to-MCP conversion, hot-reloadable config, management UI, multi-tenant support, and Docker/Kubernetes deployment options. | MCP Runtime chooses Kubernetes reconciliation over config hot reload as the core workflow: CRD validation, status, RBAC, namespaces, operator ownership, and platform APIs all point back to the same desired state. |
| [OpenZiti MCP Gateway](https://github.com/openziti/mcp-gateway) | Zero-trust remote access for MCP tools using zrok/OpenZiti/Agora, secure sharing, aggregation, dark services, and per-client backend sessions. | MCP Runtime assumes the Kubernetes cluster is the operating plane. Its priority is team-owned MCP workload lifecycle, policy, identity, session revocation, audit, analytics, and day-two diagnostics inside that environment. |
| [Docker MCP Gateway](https://github.com/docker/mcp-gateway) | Docker-native MCP server orchestration, catalog workflows, secret/config handling, image-oriented developer experience, and local-to-production container workflows. | MCP Runtime targets platform teams standardizing MCP inside Kubernetes: registry/image workflows are connected to CRDs, operator reconciliation, ingress, policy injection, cluster status, and Sentinel audit/analytics. |
| [Obot](https://github.com/obot-platform/obot) | Full MCP platform with hosting, registry, gateway, usage visibility, audit, and an organization-facing chat/client experience. | MCP Runtime stays closer to the infrastructure layer. It does not try to become the primary chat client; it focuses on self-hosted MCP server operations, governed request paths, and Kubernetes-native control-plane objects. |
| [Preloop](https://github.com/preloop/preloop) | Agent control plane with MCP firewall, model gateway, human approvals, runtime observability, policy-as-code, budgets, and audit. | MCP Runtime does not bundle a model gateway or approval workflow product. Its governance primitive is Kubernetes-backed server access: grants, consented sessions, side-effect limits, trust ceilings, expiry, revocation, and gateway audit for MCP servers. |
| [Agentic Community MCP Gateway & Registry](https://github.com/agentic-community/mcp-gateway-registry) | Unified MCP server and A2A agent registry with federation, semantic search, security scanning, OAuth/Keycloak/Entra paths, observability, and registry APIs. | MCP Runtime's registry/catalog story is tied directly to reconciled cluster workloads. The same platform path that lists servers also deploys them, validates their metadata, injects policy, routes traffic, and records runtime decisions. |

## Requirements

Host tools:

- Go `1.25+`
- Make
- Docker or a Docker-compatible client, with the daemon running
- `kubectl` on `PATH`, configured for the target cluster
- `curl`, `jq`, and `python3` for documented dev and traffic-generation flows
- `kind` for local Kind-based clusters

Cluster prerequisites:

- A running Kubernetes cluster: kind, k3s, minikube, Docker Desktop Kubernetes, EKS, GKE, AKS, or equivalent
- Working DNS, default storage class, ingress, and load-balancing path for your distribution
- See [`docs/deployment-targets.md`](docs/deployment-targets.md) to choose the install shape, then [`docs/cluster-readiness.md`](docs/cluster-readiness.md) before running production-like installs

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
- `make build` produces `./bin/mcp-runtime` with version metadata from
  `git describe --tags --match 'v*'`, the current commit, and UTC build time.
  Release binaries use the release tag exactly. Override with
  `VERSION=<tag> make build` when needed.
- Contributors who want a disposable local Kind install should start with the
  maintained [`docs/contributor/`](docs/contributor/README.md) guide. The
  shorter entry summary remains in
  [`docs/getting-started.md`](docs/getting-started.md#3-contributor-test-mode-cluster).
- To exercise agent-side governance against a real MCP route, use the
  [`examples/governed-agent`](examples/governed-agent/) demo.

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

Apache License 2.0. See [LICENSE](LICENSE).
