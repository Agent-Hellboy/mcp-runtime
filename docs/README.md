# MCP Runtime

MCP Runtime is an open-source, Kubernetes-native control plane for deploying, governing, and brokering MCP servers. It packages server deployment, registry workflows, gateway routing, access policy, audit evidence, and observability into one operating surface for platform, security, and compliance teams.

Unlike public MCP directories or client-specific catalogs, MCP Runtime is not
just a place to discover servers, and it is not a marketplace for MCP listings.
The platform control surface is the front door to a deployable runtime:
Kubernetes reconciliation, registry workflow, brokered tool calls, access
grants, consented sessions, audit, compliance evidence, and operational
visibility. The hosted platform shows what that experience looks like; companies
can run the same model inside their own clusters for agents, IDEs, and direct
human workflows.

<div class="docs-home">
<section class="docs-hero">
  <div class="docs-hero-copy">
  <p class="docs-eyebrow">Vendor-neutral MCP infrastructure for platform teams</p>

  <p class="docs-lead">Build and publish MCP server images, reconcile them with Kubernetes CRDs, expose them through governed gateway routes, and keep policy decisions, consented sessions, audit trails, and telemetry attached to every agent call.</p>

  <div class="docs-actions">
    <a class="docs-button docs-button-primary" href="getting-started/">Get started</a>
    <a class="docs-button" href="contributor/">Contribute</a>
    <a class="docs-button" href="architecture/">Architecture</a>
    <a class="docs-button" href="api/">API reference</a>
  </div>
  </div>

  <div class="docs-snapshot">
  <strong>Core surfaces</strong>

  <ul>
    <li>Operator and <code>MCPServer</code>, <code>MCPAccessGrant</code>, and <code>MCPAgentSession</code> CRDs</li>
    <li>Registry-backed image build, push, and deploy flow</li>
    <li>Sentinel gateway policy, grants, consented sessions, audit, and analytics</li>
    <li>Governance controls for tool access, trust levels, session revocation, and policy versioning</li>
    <li>Optional HTTP and stdio agent adapters for governed framework integrations</li>
    <li>Compliance-oriented event records for who called what, when, against which server, and whether it was allowed or denied</li>
    <li>Ingress routing for path-based MCP endpoints</li>
    <li>CLI for setup, status, registry, access, Sentinel, and servers</li>
  </ul>
  </div>
</section>
</div>

## What MCP Runtime installs

`mcp-runtime setup` installs the CRDs, runtime namespaces, an operator, registry
integration, ingress wiring, and the bundled Sentinel stack. Sentinel includes
the gateway request path, grant/session policy materialization, analytics
ingest and processing, dashboard/API services, and observability components.

## Compared with MCP directories

Top MCP directories and catalogs such as Glama, Smithery, Docker MCP Catalog,
PulseMCP, mcp.so, and client-specific catalogs are useful for public discovery,
metadata, install snippets, or client onboarding. MCP Runtime is different: it
is an open-source control plane for operating governed MCP servers inside a
company environment.

| Others usually provide | MCP Runtime provides |
|---|---|
| Public discovery and categories | Deployable runtime plus an internal server view when teams need one |
| Install snippets and connection docs | Kubernetes `MCPServer` reconciliation and routes |
| Popularity or metadata signals | Trust, grants, sessions, policy decisions, audit, and compliance evidence |
| Hosted directory or client-specific UX | Self-hosted, vendor-neutral Kubernetes control plane |

As of April 2026, we have not found another open-source MCP product that
combines a deployable Kubernetes operator, registry workflow, brokered request
path, access/session model, audit pipeline, and operational control surface in
one system.

## Governance, audit, and compliance

MCP Runtime keeps governance on the live request path instead of leaving it as
out-of-band documentation. The gateway evaluates `MCPAccessGrant` and
`MCPAgentSession` policy before tool calls reach a server, including tool-level
allow/deny rules, side-effect allowances, trust requirements, consented trust,
expiry, and revocation.

Each decision can emit audit and analytics events with the server, namespace,
team ID, human ID, agent ID, session ID, tool name, policy version, decision,
reason, and trust and side-effect context. That gives platform and security teams a
queryable record for reviewing access, investigating denied calls, and preparing
compliance evidence for governed agent workflows.

## Before setup

MCP Runtime expects an already-running Kubernetes cluster and a workstation with
the CLI prerequisites installed. The setup flow applies the runtime manifests,
installs the operator and Sentinel services, and wires ingress and registry
resources for the selected environment.

For provider-specific prerequisites such as container runtime registry trust,
DNS, ingress, TLS, and k3s configuration, start with
[Cluster readiness](cluster-readiness.md).

## Choose a path

<div class="docs-grid docs-grid-3">
<a class="docs-card" href="getting-started/">
  <span class="docs-card-kicker">Start</span>
  <strong>Install the platform</strong>
  <span>Build the CLI, run preflight checks, install the stack, and deploy the first server.</span>
</a>

<a class="docs-card" href="architecture/">
  <span class="docs-card-kicker">Understand</span>
  <strong>Read the architecture</strong>
  <span>Trace how the control plane, registry, broker, operator, and Sentinel services fit together.</span>
</a>

<a class="docs-card" href="cluster-readiness/">
  <span class="docs-card-kicker">Prepare</span>
  <strong>Check your cluster</strong>
  <span>Review prerequisites for k3s, kind, minikube, Docker Desktop Kubernetes, kubeadm, and EKS.</span>
</a>

<a class="docs-card" href="contributor/">
  <span class="docs-card-kicker">Contribute</span>
  <strong>Use the contributor guide</strong>
  <span>Set up local Kind, iterate on services, verify tenant visibility, and debug the platform.</span>
</a>

<a class="docs-card" href="publish-mcp-server/">
  <span class="docs-card-kicker">Ship</span>
  <strong>Publish an MCP server</strong>
  <span>Write a manifest or `.mcp` metadata, push an image, deploy it, and verify what the platform creates.</span>
</a>

<a class="docs-card" href="agent-adapters/">
  <span class="docs-card-kicker">Connect</span>
  <strong>Wire agent frameworks</strong>
  <span>Use HTTP and stdio adapters to present issued identity/session values without moving enforcement out of the gateway.</span>
</a>
</div>

## Operate MCP Runtime

<div class="docs-grid docs-grid-2">
<a class="docs-card" href="runtime/">
  <span class="docs-card-kicker">Runtime</span>
  <strong>Control plane</strong>
  <span>CRDs, reconciliation outputs, image resolution, ingress wiring, and rollout flow.</span>
</a>

<a class="docs-card" href="sentinel/">
  <span class="docs-card-kicker">Governance</span>
  <strong>Sentinel request path</strong>
  <span>Gateway policy, grant/session evaluation, analytics, audit events, and dashboard services.</span>
</a>

<a class="docs-card" href="multi-team/">
  <span class="docs-card-kicker">Tenancy</span>
  <strong>Multi-team isolation</strong>
  <span>Use namespaces, RBAC, and naming conventions for team boundaries without adding team fields to runtime CRDs.</span>
</a>

<a class="docs-card" href="cli/">
  <span class="docs-card-kicker">CLI</span>
  <strong>Command reference</strong>
  <span>Setup, status, registry, server, access, pipeline, and Sentinel commands.</span>
</a>

<a class="docs-card" href="api/">
  <span class="docs-card-kicker">Reference</span>
  <strong>API and CRDs</strong>
  <span><code>MCPServer</code>, access grants, sessions, gateway headers, and HTTP APIs.</span>
</a>

<a class="docs-card" href="internals/">
  <span class="docs-card-kicker">Codebase</span>
  <strong>Read the internals</strong>
  <span>Use the internal docs for codebase structure, package tours, and implementation details.</span>
</a>
</div>

## Common workflows

| Workflow | Start here |
|---|---|
| Evaluate MCP Runtime for a private MCP platform | [Getting started](getting-started.md), then [Architecture](architecture.md) |
| Run MCP Runtime on a real cluster | [Cluster readiness](cluster-readiness.md), then [Runtime](runtime.md) |
| Host multiple teams on one cluster | [Multi-team isolation](multi-team.md), then [CLI](cli.md) |
| Govern tools and sessions | [Sentinel](sentinel.md), then [API reference](api.md) |
| Integrate from automation | [CLI](cli.md), then [API reference](api.md) |
| Work on the codebase | [Contributor guide](contributor/README.md), then [Internals](internals/README.md) |

## Project status

MCP Runtime is alpha. The architecture is stable enough to evaluate as governed MCP infrastructure, but API and UX details are still evolving. Treat the `v1alpha1` types as the source of truth.
