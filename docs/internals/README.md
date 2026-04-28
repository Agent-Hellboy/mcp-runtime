# Internals

This section teaches the MCP Runtime codebase from the inside out. Use it when you want to modify the CLI, operator, Kubernetes API types, Sentinel services, manifests, or tests without reverse-engineering the repository from scratch.

For platform usage, start with the [user docs](../README.md). For guided contributor onboarding, use the Tessl skill [codebase-onboarding-check](https://tessl.io/registry/skills/github/Agent-Hellboy/codebase-onboarding-check/codebase-onboarding-check) with this repository; it can turn these docs into comprehension checks and a contribution readiness plan.

## Mental model

MCP Runtime is a Kubernetes-native control plane for MCP servers. Most changes touch one of four surfaces:

1. The CLI turns user intent into Kubernetes manifests, registry actions, cluster checks, and API calls.
2. The CRDs define the durable contract: `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession`.
3. The operator reconciles those CRDs into Deployments, Services, Ingress routes, status, and policy materialization.
4. Sentinel services provide the runtime gateway, governance APIs, analytics ingest, processing, and UI.

```mermaid
flowchart LR
    User[User or automation] --> CLI[mcp-runtime CLI]
    CLI --> K8s[Kubernetes API]
    CLI --> Registry[Container registry]
    K8s --> CRDs[MCP Runtime CRDs]
    CRDs --> Operator[Operator controller]
    Operator --> Workloads[MCP server Deployments and Services]
    Operator --> Ingress[Ingress and gateway routes]
    Operator --> Policy[Grant/session policy state]
    Client[MCP client] --> Gateway[Sentinel gateway]
    Gateway --> Policy
    Gateway --> Workloads
    Gateway --> Ingest[Analytics ingest]
    Ingest --> Processor[Processor]
    Processor --> Store[(ClickHouse/Postgres)]
    UI[Sentinel UI] --> API[Sentinel API]
    API --> K8s
    API --> Store
```

## Repository map

| Area | Start here | Why it matters |
|---|---|---|
| CLI entrypoint | [`cmd-mcp-runtime.md`](cmd-mcp-runtime.md) | Shows how the binary starts, wires Cobra commands, and reports errors. |
| CLI implementation | [`internal-cli.md`](internal-cli.md) | Covers setup, bootstrap, registry, server, access, status, sentinel, auth, and pipeline commands. |
| Kubernetes API types | [`api-types.md`](api-types.md) | Defines the public CRD shapes consumed by users, tests, and the operator. |
| Operator | [`cmd-operator.md`](cmd-operator.md) | Explains manager startup and reconciliation from desired state to Kubernetes resources. |
| Metadata helpers | [`pkg-metadata.md`](pkg-metadata.md) | Covers `.mcp` metadata loading, host resolution, and CRD generation helpers. |
| Manifests and examples | [`config-and-examples.md`](config-and-examples.md) | Explains Kustomize overlays, registry/ingress config, and example MCP servers. |
| Tests | [`tests.md`](tests.md) | Maps unit, golden, integration, and Kind e2e coverage. |

## Control-plane flow

A normal deployment starts in the CLI, passes through the Kubernetes API, and is completed by reconciliation.

```mermaid
sequenceDiagram
    participant U as User
    participant CLI as mcp-runtime CLI
    participant K as Kubernetes API
    participant O as Operator
    participant R as Registry
    participant W as Workloads

    U->>CLI: setup / server apply / pipeline deploy
    CLI->>K: apply CRDs, manifests, MCPServer
    CLI->>R: build or push images when requested
    K-->>O: watch MCPServer changes
    O->>K: create or update Deployment, Service, Ingress
    O->>K: update MCPServer status
    K-->>CLI: status and resource queries
    O-->>W: desired state becomes running pods
```

When changing this path, check the relevant CLI command, the `api/v1alpha1` contract, the operator reconciliation code, and at least one test that proves the generated or reconciled resource shape.

## Runtime request flow

At request time, clients do not call server pods directly. Traffic flows through the gateway, which checks grants and sessions before forwarding MCP JSON-RPC calls.

```mermaid
flowchart TD
    Client[MCP client] --> Route[Ingress route]
    Route --> Gateway[Sentinel gateway or proxy]
    Gateway --> Authz[Grant/session evaluation]
    Authz -->|allow| Server[MCP server pod]
    Authz -->|deny| Deny[JSON-RPC error]
    Gateway --> Events[Audit and analytics event]
    Events --> Ingest[services/ingest]
    Ingest --> Processor[services/processor]
    Processor --> Analytics[(analytics store)]
    API[services/api] --> Grants[MCPAccessGrant]
    API --> Sessions[MCPAgentSession]
    Grants --> Authz
    Sessions --> Authz
```

Governance-related changes usually span `api/v1alpha1/access_types.go`, `pkg/access/`, `services/api`, `services/mcp-proxy`, `services/ingest`, and the e2e policy scenarios.

## Package dependency guide

```mermaid
flowchart TB
    Cmd[cmd/mcp-runtime] --> InternalCLI[internal/cli]
    InternalCLI --> Metadata[pkg/metadata]
    InternalCLI --> Manifest[pkg/manifest]
    InternalCLI --> K8sClient[pkg/k8sclient]
    InternalCLI --> Access[pkg/access]
    CmdOp[cmd/operator] --> Operator[internal/operator]
    Operator --> API[api/v1alpha1]
    Operator --> K8sClient
    Operator --> Manifest
    Operator --> Access
    Services[services/*] --> ServiceUtil[pkg/serviceutil]
    Services --> Access
    Services --> ClickHouse[pkg/clickhouse]
    Metadata --> API
```

Keep shared behavior in `pkg/` only when multiple binaries or services need it. CLI-only behavior belongs in `internal/cli`; reconciliation behavior belongs in `internal/operator`; HTTP service glue belongs near the service that owns the endpoint.

## Learning path

1. Read [API types](api-types.md) first. The CRDs are the contract that every other subsystem follows.
2. Read [CLI internals](internal-cli.md) and [cmd/mcp-runtime](cmd-mcp-runtime.md) to see how users create, inspect, and deploy resources.
3. Read [operator internals](cmd-operator.md) to understand how `MCPServer` state becomes Kubernetes workloads and ingress.
4. Read [config and examples](config-and-examples.md), then run or inspect the example server manifests.
5. Read [tests](tests.md) before making changes; it shows the fastest feedback loop and the broader CI safety net.
6. Use the Tessl [codebase-onboarding-check skill](https://tessl.io/registry/skills/github/Agent-Hellboy/codebase-onboarding-check/codebase-onboarding-check) to quiz yourself on the architecture, identify weak spots, and produce a focused contribution plan.

## Change playbooks

| Change | Read first | Verify with |
|---|---|---|
| Add or change a CLI flag | `internal/cli`, `cmd/mcp-runtime`, golden CLI tests | `go test ./internal/cli/... ./test/golden/... -count=1` |
| Change a CRD field | `api/v1alpha1`, CRD YAML, operator reconciliation, docs/API reference | `go test ./api/v1alpha1/... ./internal/operator/... -count=1` |
| Change generated manifests | `pkg/metadata`, `pkg/manifest`, `config/`, examples | targeted package tests plus manifest diff review |
| Change reconciliation behavior | `internal/operator`, API types, k8s helpers | `go test ./internal/operator/... -race -count=1` |
| Change governance policy | `pkg/access`, `services/api`, `services/mcp-proxy`, access CRDs | targeted service tests plus e2e policy scenario |
| Change docs site behavior | `docs/mkdocs.yml`, `docs/nginx.conf`, Markdown pages | MkDocs build or docs container build |

## Contributor checklist

Before opening a change, confirm:

- The code follows the closest existing package pattern.
- Public behavior is reflected in `docs/`, `README.md`, or `AGENTS.md` when relevant.
- CRD changes update both Go types and generated YAML.
- CLI help changes update golden snapshots intentionally.
- Narrow tests pass for touched packages.
- Full `go test ./... -count=1 -race` is run before merge when the change touches shared behavior.

> Line references in the linked internals pages match the repository at the time of writing; they are 1-based and may drift after refactors.
