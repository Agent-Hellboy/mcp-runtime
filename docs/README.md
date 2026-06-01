# MCP Runtime

**Deploy, broker, and govern MCP servers on Kubernetes. Per-call policy enforcement, multi-team isolation, full audit trail. No YAML.**

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
    <a class="docs-button docs-button-primary" href="quickstart/">Try in 10 min</a>
    <a class="docs-button" href="concepts/">Concepts</a>
    <a class="docs-button" href="architecture/">Architecture</a>
    <a class="docs-button" href="getting-started/">Self-host</a>
    <a class="docs-button" href="api/">API reference</a>
  </div>
  </div>
</section>
</div>

## Deploy a governed MCP server in 5 commands

```bash
mcp-runtime auth login --api-url https://platform.mcpruntime.org
mcp-runtime server init my-server --from-server http://localhost:8088
mcp-runtime server build image my-server --tag v1
# ^ prints the exact image ref, e.g. registry.mcpruntime.org/myteam/my-server:v1
mcp-runtime registry push --image registry.mcpruntime.org/myteam/my-server:v1 --scope tenant
mcp-runtime server deploy my-server --scope tenant --metadata-dir .mcp
```

The gateway enforces grants and sessions on every tool call. No Kubernetes manifests. No ingress config.
Point any MCP client at the adapter proxy — governance headers injected transparently.

---

## Who is this for?

| You are | MCP Runtime gives you |
|---|---|
| **Platform engineer** | Kubernetes operator + registry + ingress wiring without writing a single manifest |
| **Security team** | Per-tool audit trail, trust levels, session revocation, deny rules, compliance evidence |
| **Team lead** | Isolated namespace per team, grants scoped to teams, cross-team access without sharing credentials |
| **Developer** | One CLI to deploy, one adapter to connect — no Kubernetes knowledge needed |

---

## Why I built this

I got introduced to MCP while building a Superset MCP server at work. While implementing it I started reading the spec, which led me to a similar platform-level project I was building internally — it never got approved. Along the way I noticed a real infrastructure problem: there is no good way for small teams to deploy and govern MCP servers without either buying an expensive gateway or wiring everything up manually. Everyone ends up running redundant copies of the same server — payments team has one, infra team has one, data team has one. Wasteful and impossible to govern. I thought everyone should have this, so here I am building it in the open.

I have been reading the MCP SEPs for gateway and identity management patterns. There are active proposals for exactly these problems. The gateway policy enforcement is a work in progress — I am following the spec and iterating. MCP still has a long way to go here and so do I.

---

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
[Deployment Targets](deployment-targets.md) to choose the right install shape,
then [Cluster readiness](cluster-readiness.md) for distribution-specific
preparation.

## Where to go next

<div class="docs-grid docs-grid-2">
<a class="docs-card" href="getting-started/">
  <span class="docs-card-kicker">Start here</span>
  <strong>Get started</strong>
  <span>Build the CLI, install the stack, deploy your first server, and observe live traffic.</span>
</a>

<a class="docs-card" href="architecture/">
  <span class="docs-card-kicker">Understand</span>
  <strong>Architecture</strong>
  <span>How the control plane, registry, broker, operator, and Sentinel services fit together.</span>
</a>
</div>

**Developer guide** — publish and govern MCP servers

<div class="docs-grid docs-grid-3">
<a class="docs-card" href="publish-mcp-server/">
  <span class="docs-card-kicker">Build</span>
  <strong>Publish an MCP server</strong>
  <span>Write metadata, build and push an image, deploy it, and verify what the platform creates.</span>
</a>

<a class="docs-card" href="agent-adapters/">
  <span class="docs-card-kicker">Connect</span>
  <strong>Agent adapters</strong>
  <span>HTTP and stdio adapters that inject issued identity and session headers into agent requests.</span>
</a>

<a class="docs-card" href="multi-team/">
  <span class="docs-card-kicker">Govern</span>
  <strong>Multi-team isolation</strong>
  <span>Namespace-per-team isolation, RBAC, and cross-team server access.</span>
</a>
</div>

**Operator guide** — deploy and operate the platform

<div class="docs-grid docs-grid-3">
<a class="docs-card" href="deployment-targets/">
  <span class="docs-card-kicker">Plan</span>
  <strong>Deployment targets</strong>
  <span>Choose the right install shape for k3s, EKS, GKE, AKS, and other distributions.</span>
</a>

<a class="docs-card" href="runtime/">
  <span class="docs-card-kicker">Operate</span>
  <strong>Runtime</strong>
  <span>CRDs, reconciliation outputs, image resolution, ingress wiring, and rollout flow.</span>
</a>

<a class="docs-card" href="sentinel/">
  <span class="docs-card-kicker">Observe</span>
  <strong>Sentinel</strong>
  <span>Gateway policy evaluation, analytics, audit events, and observability services.</span>
</a>
</div>

**Reference**

<div class="docs-grid docs-grid-2">
<a class="docs-card" href="cli/">
  <span class="docs-card-kicker">CLI</span>
  <strong>Command reference</strong>
  <span>Every command with flags, examples, and a full end-to-end walkthrough.</span>
</a>

<a class="docs-card" href="api/">
  <span class="docs-card-kicker">API</span>
  <strong>API and CRDs</strong>
  <span>MCPServer, MCPAccessGrant, MCPAgentSession fields and HTTP endpoints.</span>
</a>
</div>

## Which setup should I use?

| Setup | Use it when | Time to first server |
|---|---|---|
| **Live platform** (`platform.mcpruntime.org`) | Evaluating, no infrastructure, just want to try it | 10 min |
| **Local Kind cluster** (`--test-mode`) | Contributing to the repo, CI, quick local demo | 30 min |
| **k3s on-prem** | Production on your own hardware | 2–4 hours |
| **EKS / GKE / AKS** | Production in cloud | 1–2 hours |

---

## Project status

MCP Runtime is **alpha**. The architecture is stable enough to evaluate as governed MCP infrastructure, but API and UX details are still evolving. Treat the `v1alpha1` types as the source of truth. A security audit is planned but has not been completed — do not use this in production without your own review.

---

## Community

- [GitHub Issues](https://github.com/Agent-Hellboy/mcp-runtime/issues) — bug reports and feature requests
- [GitHub Discussions](https://github.com/Agent-Hellboy/mcp-runtime/discussions) — questions, ideas, and general discussion
- [Releases](https://github.com/Agent-Hellboy/mcp-runtime/releases) — changelog and binary downloads
