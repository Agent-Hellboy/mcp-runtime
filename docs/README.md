# MCP Runtime Documentation

Documentation for using and operating the MCP Runtime platform — a Kubernetes-native control plane for internal Model Context Protocol (MCP) servers.

> Eventually served at **docs.mcpruntime.org**. Today these are plain Markdown files; render them on GitHub or with any static site generator.

## Map

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install prerequisites, run `setup`, deploy your first MCP server. |
| [Architecture](architecture.md) | How the platform is built: control plane, operator, request path, services. |
| [Runtime](runtime.md) | Control-plane responsibilities, core resources, reconciliation outputs. |
| [CLI](cli.md) | Every `mcp-runtime` command, flag, and operational flow. |
| [Sentinel](sentinel.md) | Governed request path, policy enforcement, audit, and observability. |
| [API reference](api.md) | CRD fields, gateway headers, runtime/governance/analytics HTTP APIs. |
| [Cluster readiness](cluster-readiness.md) | Per-distribution prerequisites (k3s / kind / minikube / kubeadm). |
| [Internals](internals/README.md) | Source-tree walkthroughs for contributors. |

## Where to start

- **Operating a cluster:** [Getting started](getting-started.md) → [CLI](cli.md) → [Cluster readiness](cluster-readiness.md).
- **Understanding the platform:** [Architecture](architecture.md) → [Runtime](runtime.md) → [Sentinel](sentinel.md).
- **Writing manifests / integrating:** [API reference](api.md).
- **Hacking on the codebase:** [Internals](internals/README.md) plus [`AGENTS.md`](../AGENTS.md) at the repo root.

## Status

Alpha. The architecture is stable enough to evaluate. The API and UX are still evolving — treat the `v1alpha1` types as the source of truth.
