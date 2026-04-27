# MCP Runtime Documentation

Documentation for using and operating MCP Runtime: an open source manager, registry, broker, and infrastructure layer for company Model Context Protocol (MCP) servers on Kubernetes.

> Served at **docs.mcpruntime.org** as a generated MkDocs site. Source remains plain Markdown in this directory.

MCP Runtime helps platform and security teams turn MCP from scattered experiments into managed infrastructure. Use it to deploy internal MCP servers, publish them through controlled registry workflows, broker agent traffic through governed routes, and keep policy, audit, and observability attached to every endpoint.

## Map

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install the stack, deploy a server, grant access, and observe live traffic. |
| [Architecture](architecture.md) | How the manager, registry, broker, operator, and Sentinel services fit together. |
| [Runtime](runtime.md) | Control-plane responsibilities, core resources, reconciliation outputs, and rollout flow. |
| [CLI](cli.md) | Every `mcp-runtime` command, flag, and operational flow. |
| [Sentinel](sentinel.md) | Governed request path, policy enforcement, audit, and observability. |
| [API reference](api.md) | CRD fields, gateway headers, runtime/governance/analytics HTTP APIs. |
| [Cluster readiness](cluster-readiness.md) | Per-distribution prerequisites (k3s / kind / minikube / kubeadm). |
| [Internals](internals/README.md) | Source-tree walkthroughs for contributors. |

## Where to start

- **Evaluating for a company platform:** [Getting started](getting-started.md) → [Architecture](architecture.md) → [Sentinel](sentinel.md).
- **Operating a cluster:** [Getting started](getting-started.md) → [CLI](cli.md) → [Cluster readiness](cluster-readiness.md).
- **Writing manifests / integrating:** [API reference](api.md).
- **Hacking on the codebase:** [Internals](internals/README.md) plus [`AGENTS.md`](https://github.com/Agent-Hellboy/mcp-runtime/blob/main/AGENTS.md) at the repo root.

## Status

Alpha. The architecture is stable enough to evaluate as internal MCP infrastructure. The API and UX are still evolving — treat the `v1alpha1` types as the source of truth.

## Production deploy (GitHub Actions)

The `deploy-docs` job in [`.github/workflows/ci.yaml`](https://github.com/Agent-Hellboy/mcp-runtime/blob/main/.github/workflows/ci.yaml)
syncs `docs/` to your remote host and, by default, builds/runs a Docker
container there. On `main`, docs-only changes deploy as soon as the path
filter detects changes under `docs/`; the deploy job does not wait for Go unit,
integration, or Kind e2e jobs.

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

Optional GitHub secrets:

| Secret | Default | Purpose |
|---|---:|---|
| `DOCS_DEPLOY_HOST_KEY` | `ssh-keyscan` fallback | Pinned SSH host key for `DOCS_DEPLOY_HOST`; use either a full known-hosts line such as `203.0.113.10 ssh-ed25519 AAAA...` or a bare host key such as `ssh-ed25519 AAAA...`. |
| `DOCS_HOST_PORT` | `8081` | Host port published by Docker. |
| `DOCS_CONTAINER_PORT` | `80` | Container port exposed by the docs image. |
| `DOCS_CONTAINER_NAME` | `mcp-runtime-docs` | Remote Docker container name. |
| `DOCS_IMAGE_NAME` | `mcp-runtime-docs:latest` | Remote Docker image tag. |
| `DOCS_DEPLOY_COMMAND` | none | If set, CI runs this remote command instead of the default Docker build/run sequence. |
