# MCP Runtime

MCP Runtime is an open source control plane for operating company MCP servers on Kubernetes. It packages server deployment, registry workflows, gateway routing, access policy, audit, and observability into one infrastructure layer.

<div class="docs-home">
<section class="docs-hero">
  <div class="docs-hero-copy">
  <p class="docs-eyebrow">MCP infrastructure for platform teams</p>

  <p class="docs-lead">Deploy internal MCP servers, expose them through governed routes, and keep policy and telemetry attached to every agent call.</p>

  <div class="docs-actions">
    <a class="docs-button docs-button-primary" href="getting-started/">Get started</a>
    <a class="docs-button" href="architecture/">Architecture</a>
    <a class="docs-button" href="api/">API reference</a>
  </div>
  </div>

  <div class="docs-snapshot">
  <strong>Core surfaces</strong>

  <ul>
    <li>Operator and <code>MCPServer</code> CRDs</li>
    <li>Registry-backed build and deploy flow</li>
    <li>Sentinel gateway policy and analytics</li>
    <li>CLI for setup, status, grants, sessions, and servers</li>
  </ul>
  </div>
</section>
</div>

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
  <span>Trace how the manager, registry, broker, operator, and Sentinel services fit together.</span>
</a>

<a class="docs-card" href="cluster-readiness/">
  <span class="docs-card-kicker">Prepare</span>
  <strong>Check your cluster</strong>
  <span>Review prerequisites for k3s, kind, minikube, Docker Desktop Kubernetes, kubeadm, and EKS.</span>
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
</div>

## Common workflows

| Workflow | Start here |
|---|---|
| Evaluate MCP Runtime for a company platform | [Getting started](getting-started.md), then [Architecture](architecture.md) |
| Run MCP Runtime on a real cluster | [Cluster readiness](cluster-readiness.md), then [Runtime](runtime.md) |
| Govern tools and sessions | [Sentinel](sentinel.md), then [API reference](api.md) |
| Integrate from automation | [CLI](cli.md), then [API reference](api.md) |
| Work on the codebase | [Internals](internals/README.md) |

## Project status

MCP Runtime is alpha. The architecture is stable enough to evaluate as internal MCP infrastructure, but API and UX details are still evolving. Treat the `v1alpha1` types as the source of truth.
