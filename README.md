# MCP Runtime

<p align="center">
  <img src="website/static/brand/mcp-runtime-banner.png" alt="MCP Runtime — Deploy, govern, and broker MCP servers using a Kubernetes-native control plane" />
</p>

[![CI](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/ci.yaml/badge.svg)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/ci.yaml)
[![Kind E2E](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/ci.yaml?branch=main&label=Kind%20E2E&job=Kind%20E2E)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/ci.yaml?query=branch%3Amain+job%3AKind%20E2E)
[![Production E2E](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/production-e2e.yaml?branch=main&label=Production%20E2E&event=workflow_dispatch)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/production-e2e.yaml)
[![Gosec Scan](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/security-gosec.yaml?branch=main&label=Gosec%20Scan)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/security-gosec.yaml)
[![Gitleaks Scan](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/security-gitleaks.yaml?branch=main&label=Gitleaks%20Scan)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/security-gitleaks.yaml)
[![Trivy FS Scan](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20FS%20Scan&job=Trivy%20FS%20Scan)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+job%3ATrivy%20FS%20Scan)
[![Trivy Image Scan](https://img.shields.io/github/actions/workflow/status/mcp-runtime/mcp-runtime/security-trivy.yaml?branch=main&label=Trivy%20Image%20Scan&job=Trivy%20operator%20Image)](https://github.com/mcp-runtime/mcp-runtime/actions/workflows/security-trivy.yaml?query=branch%3Amain+event%3Apush)
[![Coverage](https://codecov.io/gh/mcp-runtime/mcp-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/mcp-runtime/mcp-runtime/branch/main)

MCP Runtime is a Kubernetes control plane for [Model Context Protocol](https://modelcontextprotocol.io/) servers. It deploys MCP servers into your cluster, enforces per-tool access policy on every call, and records each decision for audit.

You describe a server with an `MCPServer` resource and the operator creates its Deployment, Service, Ingress, and policy. Access is granted with `MCPAccessGrant` and time-boxed with `MCPAgentSession`. A gateway sidecar in each server pod checks the caller's identity, session, trust level, and the tool's side effect before a call reaches your code.

A public preview runs at [platform.mcpruntime.org](https://platform.mcpruntime.org/). The same stack installs into your own cluster with `mcp-runtime setup`.

- [Website](https://mcpruntime.org/) · [Docs](https://docs.mcpruntime.org/) ([`docs/`](docs/)) · [API reference](https://docs.mcpruntime.org/api) · [Articles](https://articles.mcpruntime.org/)
- Running or evaluating an internal MCP platform? Open a [GitHub issue](https://github.com/mcp-runtime/mcp-runtime/issues) with your use case, cluster shape, or integration feedback.

> [!CAUTION]
> MCP Runtime is alpha software. APIs, commands, and behavior are still evolving. Use the docs, CRDs, and `api/v1alpha1` types as the source of truth before production use.

## Features

- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` are namespaced CRDs, so servers, access, and sessions are visible and reviewable with `kubectl`.
- The `mcp-gateway` sidecar applies deny-by-default tool rules, trust ceilings, side-effect limits, session expiry, and revocation on every `tools/call`.
- Every allow and deny decision is recorded with the identity, tool, reason, and policy version, and is queryable through the Sentinel API and dashboards.
- `adapter proxy` (HTTP) and `adapter stdio` let IDEs, agent frameworks, and scripts connect with platform-issued identity and automatic session refresh.
- Team namespaces, RBAC, and `teamID` subject matching let several teams publish and govern servers on one cluster, with private, org-wide, or public catalogs.
- Setup, registry and image-pull wiring, ingress, rollout readiness, `cluster doctor`, `cluster diagnostics`, and status commands are included.
- Documented install paths cover Kind, k3s, self-managed clusters, and managed Kubernetes with external registries.

## What ships

- `mcp-runtime` CLI for `auth`, `bootstrap`, `setup`, `status`, `registry`, `server`, `catalog`, `cluster`, `access`, `team`, and `sentinel`
- `mcp-runtime adapter proxy` and `mcp-runtime adapter stdio` subcommands for
  governed HTTP and stdio agent integrations. Both can fetch identity from
  the platform with `--server <name> --agent <id> [--auto-refresh]` once an
  enabled grant exists for that server and agent, or accept explicit
  `MCP_RUNTIME_*` env vars (see [Agent Adapters](docs/agent-adapters.md))
- Platform UI for authenticated MCP catalog browsing, platform state, and web operations
- `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` CRDs
- Kubernetes operator for `Deployment`, `Service`, `Ingress`, and policy materialization
- Internal or provisioned registry workflows
- Optional gateway enforcement for identity, tool policy, trust, and audit emission
- Bundled Sentinel stack for ingest, processing, API, UI, and observability

For how MCP Runtime relates to MCP directories and to other MCP gateways and platforms, see [Comparison](docs/comparison.md).

## Requirements

Host tools:

- Go `1.26+` (matches the repository `go.mod` files)
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
./bin/mcp-runtime auth login --api-url <platform-url>   # save platform credentials
./bin/mcp-runtime team create acme --name "Acme Corp"   # create a team namespace (admin)
./bin/mcp-runtime registry status        # inspect registry
./bin/mcp-runtime server status          # inspect MCP servers
./bin/mcp-runtime catalog tools          # search tools across visible servers
./bin/mcp-runtime access grant list      # inspect access grants
./bin/mcp-runtime adapter proxy --server <name> --agent <id> --auto-refresh   # connect an MCP client
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
