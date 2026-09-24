# Comparison with other projects

This page compares MCP Runtime with MCP directories and with other MCP gateway
and platform projects. Features and deployment models change; check each
project's current documentation before choosing.

## MCP directories and catalogs

The [Official MCP Registry](https://registry.modelcontextprotocol.io/),
[Glama](https://glama.ai/mcp), [Smithery](https://smithery.ai/),
[Docker MCP Catalog](https://hub.docker.com/mcp),
[PulseMCP](https://www.pulsemcp.com/), [mcp.so](https://mcp.so/), and
client-specific catalogs help people find and install public MCP servers.
MCP Runtime runs MCP servers inside your own cluster and governs calls to them.
The two are complementary: a server you find in a directory can be deployed and
governed with MCP Runtime.

| | Directories and catalogs | MCP Runtime |
|---|---|---|
| Purpose | Find and install public servers | Host, deploy, govern, and audit your own servers |
| Data | Discovery metadata, popularity, install snippets | `MCPServer` resources, grants, sessions, policy decisions, audit events |
| Where it runs | Third-party hosted service or client feature | Your Kubernetes cluster |
| Request path | Ends once the client is configured | Every tool call passes through the gateway |

## MCP gateways and platforms

Several projects run MCP servers on Kubernetes or define Kubernetes APIs for MCP
traffic. MCP Runtime's focus is expressing server workloads **and** access
policy (trust ceilings, allowed side effects, per-tool rules, user consent,
expiry, revocation) as validated Kubernetes resources that the operator
reconciles and the gateway enforces.

| Project | Focus | Compared with MCP Runtime |
|---|---|---|
| [Archestra](https://github.com/archestra-ai/archestra) | MCP platform with Kubernetes server orchestration, gateway, registry, and agent/chat features | Overlaps on Kubernetes-hosted servers; broader agent platform. MCP Runtime centers access on grant and session resources. |
| [Obot](https://github.com/obot-platform/obot) | MCP hosting, registry, gateway, and an organization-facing AI experience | Also hosts MCP servers. MCP Runtime keeps workload and access policy as Kubernetes state. |
| [Microsoft MCP Gateway](https://github.com/microsoft/mcp-gateway) | Kubernetes-oriented MCP gateway and management APIs, adapter/tool lifecycle, identity integrations | Overlaps on Kubernetes. MCP Runtime models deployment, grants, and consented sessions as resources. |
| [Agent Router](https://github.com/theagentrouter/agent-router) | Kubernetes Gateway API routing for MCP and AI traffic | Overlaps on Kubernetes APIs, focused on routing. MCP Runtime also manages server workloads and session consent. |
| [agentgateway](https://github.com/agentgateway/agentgateway) | High-performance data-plane gateway for MCP, agents, and AI traffic | Focused on routing and policy in the data plane. MCP Runtime owns workload lifecycle and access state. |
| [IBM ContextForge](https://github.com/IBM/mcp-context-forge) | Federation and gateway for MCP, A2A, REST, and gRPC | Broader protocol federation. MCP Runtime focuses on Kubernetes workload and access governance. |
| [MCPJungle](https://github.com/mcpjungle/MCPJungle) | Self-hosted team gateway, unified endpoint, discovery, tool grouping | Centers aggregation. MCP Runtime ties access decisions to reconciled cluster resources. |
| [Unla](https://github.com/AmoyLab/Unla) | MCP/API gateway with API-to-MCP conversion | Focused on API conversion. MCP Runtime focuses on deployed workloads and governed sessions. |
| [OpenZiti MCP Gateway](https://github.com/openziti/mcp-gateway) | Zero-trust networking and remote access for MCP tools | Focused on networking. MCP Runtime governs workloads and agent access inside Kubernetes. |
| [Docker MCP Gateway](https://github.com/docker/mcp-gateway) | Local Docker-based MCP server lifecycle, catalog, and configuration | Focused on local developer workflow. MCP Runtime targets platform-managed Kubernetes. |
| [LiteLLM](https://github.com/BerriAI/litellm) | Model gateway with MCP access, provider routing, keys, and spend controls | Focused on model routing and spend. MCP Runtime focuses on MCP server lifecycle and consented access. |
| [Kong](https://github.com/Kong/kong) | General API gateway with MCP proxy and AI gateway features | General-purpose gateway. MCP Runtime is an MCP-specific control plane for workloads, grants, and sessions. |
| [Portkey](https://github.com/Portkey-AI/gateway) | AI gateway and managed MCP gateway | Focused on AI traffic and hosted gateways. MCP Runtime is self-managed and reconciled from Kubernetes resources. |
| [Composio](https://github.com/ComposioHQ/composio) | SaaS integrations, toolkits, and per-user OAuth for agents | Focused on integration breadth. MCP Runtime focuses on operating MCP workloads and access policy. |
| [Preloop](https://github.com/preloop/preloop) | Agent control plane with MCP firewall, model gateway, approvals, and budgets | Focused on model controls and approvals. MCP Runtime expresses grants and sessions as Kubernetes state. |

## When to choose MCP Runtime

Choose MCP Runtime when you want one system to deploy MCP servers into your
cluster and enforce who may call which tool, with what trust and consent.

Another project may fit better if your main need is broad SaaS integrations,
model routing and spend controls, API-to-MCP conversion, remote zero-trust
networking, or a full agent/chat product.
