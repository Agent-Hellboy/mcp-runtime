# Architecture

MCP Runtime is a Kubernetes-native manager, registry, broker, and infrastructure layer for internal MCP servers. It gives companies one platform surface for deploying servers, publishing images, brokering agent access, and keeping policy, consent, audit, and observability on the request path.

```mermaid
flowchart LR
    Dev[Developer / CI] --> CLI[mcp-runtime CLI]
    CLI --> CRD[MCPServer CRD]
    CRD --> Operator[Runtime operator]
    Operator --> Workload[Deployment + Service]
    Operator --> Ingress[Ingress route]
    Operator --> Policy[Policy ConfigMap]
    Client[MCP client] --> Ingress
    Ingress --> Proxy[mcp-proxy]
    Policy --> Proxy
    Proxy --> Server[MCP server]
    Proxy --> Ingest[Sentinel ingest]
    Ingest --> UI[API + dashboard]
```

## Platform Shape

| Piece | What it gives the company |
|---|---|
| Manager | Cluster install, declarative `MCPServer` resources, operator reconciliation, rollout status. |
| Registry | Controlled image publishing and pull-address resolution for deployed MCP servers. |
| Broker | A governed gateway that evaluates access before tool calls reach the server. |
| Sentinel | Audit, analytics, API, UI, and operational visibility for governed MCP traffic. |
| Infrastructure | Kubernetes-native routing, TLS/DNS integration, namespaces, services, and ingress ownership. |

## Control Plane

The CLI owns workstation and cluster workflows: dependency checks, bootstrap preflights, setup, registry operations, manifest generation, and access-management commands. It writes Kubernetes resources rather than running the data path itself.

The operator watches `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession` resources. For each server, it reconciles the workload Deployment, Service, Ingress, gateway sidecar configuration, policy materialization, and status conditions.

The CRDs are the contract between user intent and cluster state. The `api/v1alpha1` Go types and generated CRD YAML are the source of truth for supported fields and validation.

## Brokered Request Path

Public traffic enters through the configured ingress controller. The default public shape is path based: `/<server-name>/mcp`, or `/<publicPathPrefix>/mcp` when `spec.publicPathPrefix` is set.

When the gateway is enabled, requests flow through `mcp-proxy` before they reach the MCP server. The proxy acts as the broker: it reads identity and session headers, evaluates grants and sessions from the rendered policy ConfigMap, forwards allowed MCP calls, rejects denied calls, and emits audit events.

Sentinel services receive those events, process them for analytics, and expose the dashboard/API used to inspect servers, grants, sessions, and recent decisions.

## Boundaries

| Layer | Responsibility |
|---|---|
| CLI | Local build/setup workflows, generated manifests, status, and access commands. |
| Operator | Kubernetes reconciliation for servers, routes, gateway config, policy, and status. |
| Registry | Image storage and pull-address resolution for deployed MCP servers. |
| Gateway | Per-request policy enforcement and audit emission. |
| Sentinel API/UI | Governance CRUD, dashboard state, analytics views, and operator-facing inspection. |
| Cluster infrastructure | Ingress controller, DNS, TLS, storage classes, and node image-pull behavior. |

## Operational Shape

`setup` installs the runtime namespaces, CRDs, registry, operator, ingress wiring, and the Sentinel stack unless explicitly disabled. In development, Kind and path-based localhost ingress are enough. In production, `MCP_PLATFORM_DOMAIN` can derive `registry.<domain>`, `mcp.<domain>`, and `platform.<domain>` so registry pulls, MCP traffic, and the dashboard each have stable hostnames.

The intended company workflow is straightforward: platform teams install the stack, application teams publish MCP servers, security teams define grants and sessions, and agents call tools through the governed broker path.

For routing details and field semantics, see [Runtime](runtime.md) and [API reference](api.md).
