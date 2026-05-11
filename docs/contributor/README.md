# Contributor Guide

This section is the practical contributor runbook for MCP Runtime. Use it when
you need to set up a disposable cluster, change code, rebuild one service,
exercise tenant isolation, or debug the platform UI and MCP request path.

For product concepts, start with [Architecture](../architecture.md). For code
package boundaries, use [Internals](../internals/README.md). This guide focuses
on the day-to-day loop for contributors.

## Contribution Loop

1. Sync the repo and check the worktree before editing.

   ```bash
   git status --short
   ```

2. Build the CLI and run the narrow tests for the area you are touching.

   ```bash
   make deps
   make build
   go test ./internal/cli/... ./internal/operator/... -count=1
   ```

3. Use a disposable Kind cluster for platform, UI, operator, registry, gateway,
   and Sentinel changes.

   Start with [Local Kind and Test Mode](local-kind.md).

4. Rebuild only the changed service while iterating.

   Use [Service Iteration](service-iteration.md) for API, UI, operator, ingest,
   processor, and MCP proxy rebuilds.

5. Verify with a real MCP route and, when relevant, a tenant-isolation matrix.

   Use [Runtime MCP Testing](runtime-mcp-testing.md).

6. Update docs in the same PR when behavior, commands, defaults, setup flow, UI
   behavior, API shapes, or troubleshooting steps change.

## Where to Work

| Change | Start here | Usual checks |
|---|---|---|
| CLI commands | `cmd/mcp-runtime/`, `internal/cli/` | `go test ./internal/cli/... -count=1`, golden help tests when help text changes |
| Operator reconciliation | `cmd/operator/`, `internal/operator/`, `api/v1alpha1/` | `go test ./internal/operator/... -count=1`, integration tests for CRD behavior |
| Platform API | `services/api` | `(cd services/api && go test ./... -count=1)` |
| Platform UI | `services/ui` | `(cd services/ui && go test ./... -count=1)`, `node --check services/ui/static/app.js` |
| MCP proxy/gateway | `services/mcp-proxy`, `pkg/policy`, `pkg/access` | service tests plus `E2E_SCENARIOS=governance` or `smoke-auth` when request behavior changes |
| Agent adapters | `cmd/mcp-runtime-agent-proxy/`, `cmd/mcp-runtime-mcp-shim/`, `internal/agentadapter/` | `go test ./internal/agentadapter -count=1` |
| Docs only | `docs/`, `README.md`, `AGENTS.md` | docs build if available, `rg` for stale terms, `git diff --check` |

## Local Cluster Contract

The contributor Kind flow installs a full local stack:

- CRDs and the operator in `mcp-runtime`
- Sentinel API/UI/ingest/processor/proxy services in `mcp-sentinel`
- Traefik ingress on the local gateway path
- A bundled registry for runtime images
- `mcp-servers` as the org-scoped MCP catalog namespace
- Optional `mcp-team-<slug>` namespaces for tenant isolation

The MCP server catalog is authenticated. Anonymous users should not see MCP
servers. Logged-in users see org-scoped MCPs from `mcp-servers` plus MCPs from
their own team or user namespaces.

## Before Opening a PR

Run the smallest checks that prove your change, then widen only when the blast
radius needs it.

```bash
gofmt -s -l .
go vet ./...
go test ./... -count=1
git diff --check
```

For changes that affect Kind setup, ingress, registry pushes, gateway policy,
sessions, grants, analytics, or tenant isolation, run the relevant e2e scenario:

```bash
E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh
E2E_CACHE_MODE=1 E2E_SCENARIOS=governance bash test/e2e/kind.sh
E2E_CACHE_MODE=1 E2E_SCENARIOS=multitenancy bash test/e2e/kind.sh
```

Use `E2E_CACHE_MODE=1` only for repeated local debugging. Omit it when you want
a CI-equivalent fresh cluster.

