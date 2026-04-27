# MCP Runtime Documentation

Documentation for using and operating the MCP Runtime platform — a Kubernetes-native control plane for internal Model Context Protocol (MCP) servers.

> Served at **docs.mcpruntime.org** as a generated MkDocs site. Source remains plain Markdown in this directory.

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
- **Hacking on the codebase:** [Internals](internals/README.md) plus [`AGENTS.md`](https://github.com/Agent-Hellboy/mcp-runtime/blob/main/AGENTS.md) at the repo root.

## Status

Alpha. The architecture is stable enough to evaluate. The API and UX are still evolving — treat the `v1alpha1` types as the source of truth.

## Production deploy (GitHub Actions)

The `deploy-docs` job in [`.github/workflows/ci.yaml`](https://github.com/Agent-Hellboy/mcp-runtime/blob/main/.github/workflows/ci.yaml)
syncs `docs/` to your remote host and, by default, builds/runs a Docker
container there.

Docker build context is this `docs/` directory:

- `Dockerfile` builds a static MkDocs site and packages it in `nginx`.
- `nginx.conf` serves the generated site for `docs.mcpruntime.org` with
  MkDocs directory URL handling, static asset caching, gzip, and basic
  hardening headers.
- `mkdocs.yml` defines nav/theme/site settings.
- `requirements.txt` pins MkDocs dependencies.

Required GitHub secrets:

- `DOCS_DEPLOY_HOST`
- `DOCS_DEPLOY_USER`
- `DOCS_DEPLOY_PATH`
- `DOCS_DEPLOY_SSH_KEY`
- `DOCS_DEPLOY_HOST_KEY` — pinned SSH host key line, for example `host ssh-ed25519 AAAA...`

Optional GitHub secrets:

| Secret | Default | Purpose |
|---|---:|---|
| `DOCS_HOST_PORT` | `8081` | Host port published by Docker. |
| `DOCS_CONTAINER_PORT` | `80` | Container port exposed by the docs image. |
| `DOCS_CONTAINER_NAME` | `mcp-runtime-docs` | Remote Docker container name. |
| `DOCS_IMAGE_NAME` | `mcp-runtime-docs:latest` | Remote Docker image tag. |
| `DOCS_DEPLOY_COMMAND` | none | If set, CI runs this remote command instead of the default Docker build/run sequence. |
