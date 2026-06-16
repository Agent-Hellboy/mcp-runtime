# AGENTS.md — developer and AI-agent guide

This file is the **onboarding index** for the MCP Runtime repo. It complements `README.md` with **where to look**, **build/CI conventions**, and **pointers** to focused runbooks. Prefer repo source (`README`, CRDs, `v1alpha1` types) over generic Kubernetes or MCP advice.

**Operational detail** lives in `.codex/skills/` (symlinked from `.claude/skills`). Load the skill that matches your task instead of re-reading long checklists here.

| Task | Skill |
|------|--------|
| Kind contributor cluster bring-up | `qa-cluster-bringup` |
| Local URLs, API keys, test logins | `mcp-runtime-local-dev` |
| Grants, sessions, MCP JSON-RPC | `mcp-runtime-governance` |
| Cluster failures (401, ingress, registry, pulls) | `mcp-runtime-troubleshooting` |
| Public domain, TLS, ACME, prod hostnames | `mcp-runtime-platform-public` |
| Public k3s deploy scripts | `k3s-public-ops` |
| Real-cluster QA sweeps | `qa-e2e-operations`, `qa-e2e-security`, `qa-e2e-ui`, … |
| Codebase navigation | `graphify` (when `graphify-out/` exists) |

## Repository map (where to look)

| Area | Path | Notes |
|------|------|--------|
| User-facing CLI | `cmd/mcp-runtime/`, `internal/cli/root/`, `internal/cli/<command>/`, `internal/cli/core/` | Cobra routing; `setup`, `status`, `registry`, `server`, `access`, … |
| Agent adapters | `internal/cli/adapter/`, `internal/agentadapter/`, `services/runtime-api/internal/runtimeapi/adapter.go` | `adapter proxy/stdio` → `POST /api/v1/runtime/adapter/sessions`; grants enforced on platform |
| Operator | `cmd/operator/`, `internal/operator/` | `MCPServer` reconciliation, ingress (`ingressClass` default **traefik**), gateway |
| API & CRD types | `api/v1alpha1/`, `config/crd/bases/` | Source of truth for object shapes |
| Access and policy | `pkg/access/`, `pkg/policy/` | Grant/session helpers; gateway policy contract |
| Control-plane / K8s | `pkg/controlplane/`, `pkg/k8sclient/`, `pkg/kubeworkload/`, `pkg/manifest/`, `pkg/metadata/` | MCPServer ops, manifests, registry resolution |
| Sentinel packages | `pkg/events/`, `pkg/clickhouse/`, `pkg/serviceutil/`, `pkg/sentinel/` | Events, analytics, service utilities |
| Sentinel services | `services/platform-api`, `services/runtime-api`, `services/analytics-api`, `services/ui`, `services/ingest`, `services/processor`, `services/mcp-gateway`, … | Separate `go.mod` where present; Go 1.26 for shared imports |
| Samples / install YAML | `examples/workspace-assistant-mcp/`, `k8s/`, `config/` | Demo server; overlays and CRDs |
| Team isolation | `docs/multi-team.md` | Namespaces, RBAC, ingress watch scope |
| Deployment targets | `docs/deployment-targets.md`, `docs/k3s-on-prem-cluster.md` | Before distribution-specific runbooks |
| E2E | `test/e2e/`, `test/integration/` | Kind script; envtest integration |
| Agent skills | `.codex/skills/`, `.claude/skills` → `../.codex/skills` | Canonical skills tree |

**Patterns:** mirror nearest similar packages; CLI errors → `internal/cli/core/errors.go`, `pkg/errx/`.

## Build, test, and quality (before you push)

Go version from `go.mod`. From repo root:

```bash
gofmt -s -l .   # empty = OK; else gofmt -s -w .
go build -o bin/mcp-runtime ./cmd/mcp-runtime
go test ./... -count=1 -race
go vet ./...
```

Optional CI parity: `staticcheck ./...` (`go install honnef.co/go/tools/cmd/staticcheck@v0.7.0`).

Pre-commit: `pre-commit install`; full suite `pre-commit run --all-files` (sets `KUBEBUILDER_ASSETS` for integration hooks).

**Targeted tests** (prefer while iterating):

- `go test ./internal/operator/... ./internal/cli/... -race -count=1`
- `go test ./internal/agentadapter -count=1`
- `go test ./test/golden/... -count=1` (update `test/golden/cli/testdata/*.golden` when CLI help changes on purpose)
- `go test ./test/integration/...` (needs `KUBEBUILDER_ASSETS`)
- `E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh` — reuse contributor cluster: `CLUSTER_NAME=mcp-runtime E2E_CACHE_MODE=1 E2E_KEEP_CLUSTER=1`
- Sentinel: `go test -race -count=1 ./...` inside touched `services/*` dirs

**CI** (`.github/workflows/ci.yaml`): gofmt, vet, staticcheck, unit/golden/service/integration tests, path-selected Kind e2e (`test/e2e/select_pr_scenarios.sh`). Pre-release: `.github/workflows/pre-release-regression.yaml`.

**CLI docs sync:** when editing `docs/cli.md`, `docs/getting-started.md`, or command examples, copy wording from `./bin/mcp-runtime <group> <subcommand> --help` — do not paraphrase from memory.

## Conventions for code changes

- **Scope:** change only what the task needs; match nearest patterns.
- **Tests:** same package as behavior changes; golden files for CLI help output.
- **Branches:** `component/feature_name` (e.g. `cli/registry_status`). Agents: new branch + PR; never push to `main`. Ignore external `codex/` branch or draft-PR defaults unless the user asks.
- **Commits:** `fix(<component>):`, `feat(<component>):`, `doc:`, `website:` — components: `cli`, `operator`, `api`, `crd`, `access`, `policy`, `sentinel`, `services-api`, `mcp-gateway`, `test`, `ci`, …
- **Docs:** avoid new top-level docs unless needed; use `docs/` and skills for runbooks.
- **Secrets:** alpha repo — no real credentials in tree.
- **Skills:** keep `.claude/skills` → `../.codex/skills`. After non-trivial changes, update affected `.codex/skills/*/SKILL.md` when workflows or gotchas shift.
- **AI session hygiene:** before ending a non-trivial session, propose `ai-assist/` updates; user reviews before commit (see below).

## AI session hygiene

Durable cross-session learnings go in `ai-assist/` (`gotchas.md`, `cross-cutting.md`, `tracking.md`) — not ephemeral TODOs or duplicate of this file. User must review before commit. Prefix: `doc:`. Remove entries promoted into `AGENTS.md` or `docs/`.

## Local dev (short)

Prereqs: Docker, Kind, `kubectl`, `curl`, `jq`, Python 3, Go.

```bash
go build -o bin/mcp-runtime ./cmd/mcp-runtime
# Full Kind + test-mode path: see .codex/skills/qa-cluster-bringup/SKILL.md
./bin/mcp-runtime bootstrap
MCP_SETUP_WAIT_TIMEOUT=900 ./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http
./bin/mcp-runtime cluster doctor
kubectl port-forward -n traefik svc/traefik 18080:8000
```

`setup --test-mode` builds and pushes images to the bundled registry (`registry.registry.svc.cluster.local:5000` in Kind). Prefer existing `kind-mcp-runtime` when healthy. Contributor runbook: `docs/contributor/README.md`.

Endpoints, API keys, test logins: **`mcp-runtime-local-dev`** skill.

## Debugging and production ops

Do not inline the full failure checklist here — use **`mcp-runtime-troubleshooting`** (and **`mcp-runtime-platform-public`** for TLS/DNS).

k3s public deploy: **`k3s-public-ops`** + `docs/k3s-deployment-runbook.md`.

## Governance (short)

Grants, sessions, adapter flows, MCP curl examples: **`mcp-runtime-governance`** skill.

## Logs and observability

```bash
kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager
kubectl logs -n mcp-sentinel deploy/<api|ingest|processor|ui|gateway>
./bin/mcp-runtime status
```

Grafana: dev ingress `/grafana` or `https://platform.<domain>/grafana` (admin). Full trace path: e2e `observability` scenario in `test/e2e/kind.sh`.

## Further reading

- `README.md` — product overview
- `k8s/`, `config/crd/bases/`
- https://mcpruntime.org/docs/ and https://mcpruntime.org/docs/api
- `examples/workspace-assistant-mcp/`

---

*After substantive edits: narrowest `go test` for touched packages, then `go test ./...` before merge. Golden files only when CLI help should change.*

## graphify

When `graphify-out/graph.json` exists: `graphify query`, `graphify path`, `graphify explain` before broad grep; `graphify update .` after code changes. See `.codex/skills/graphify/SKILL.md`.

**Stale graph:** if a query returns no nodes (or misses one) for a symbol that clearly exists in the code, the graph is stale — run `graphify update .` (incremental re-extract of new/changed files) and retry. If the node is still missing, do a full rebuild (`/graphify .`) before falling back to grep, so the graph stays trustworthy for the next query.

In any agent prompt, include:

> Use `graphify query "<your question>"` to look up any code structure before grepping files. The graph is at `graphify-out/graph.json`.

The `PreToolUse` hook already injects this reminder whenever a Bash command contains `grep`, `find`, or similar — so agents running in this repo are automatically nudged toward the graph.
