# Tests

This page maps the main test suites to the code they protect. Prefer the
narrowest suite while iterating, then broaden before pushing when the change
touches shared contracts.

## Fast Package Tests

| Surface | Tests | Use when |
|---|---|---|
| API types | `go test ./api/v1alpha1/... -count=1` | CRD structs, validation, deepcopy, scheme registration |
| CLI | `go test ./internal/cli/... -count=1` | command routing, command behavior, setup planning, registry helpers, doctor checks |
| Operator | `go test ./internal/operator/... -race -count=1` | reconciliation defaults, owned resources, status, registry/image behavior |
| Metadata | `go test ./pkg/metadata/... -count=1` | `.mcp` loading, host resolution, manifest generation |
| Sentinel services | `go test -race -count=1 ./...` in each service module | API, UI, ingest, processor, proxy service logic |

## Golden CLI Tests

CLI help and stable output snapshots live under `test/golden/cli`.

Run:

```bash
go test ./test/golden/... -count=1
```

Update golden files only when the CLI output intentionally changes. For help
text, verify the actual command output from `./bin/mcp-runtime <command> --help`
before editing snapshots.

## Integration Tests

Integration tests live under `test/integration` and use envtest assets. They are
the right place for Kubernetes API behavior that fake clients cannot represent.

Run:

```bash
go test ./test/integration/... -count=1
```

If envtest assets are missing, follow the setup in `Makefile.operator` or the CI
workflow.

## Kind E2E

`test/e2e/kind.sh` creates a Kind cluster, configures a local registry mirror,
builds and publishes runtime images, runs `setup --test-mode`, deploys example
servers, exercises MCP requests, and verifies governance/observability paths.

Useful local runs:

```bash
E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh
E2E_SCENARIOS=all MCP_DEPLOYMENT_TIMEOUT=900s bash test/e2e/kind.sh
E2E_KEEP_CLUSTER=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh
E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth bash test/e2e/kind.sh
```

`E2E_CACHE_MODE=1` is for repeated local debugging. It implies
`E2E_KEEP_CLUSTER=1`, reuses the existing Kind cluster and local registry when
present, skips platform setup if the core platform is already ready, and reuses
image tags already published to the local registry.

E2E output uses ANSI color only for an interactive terminal by default. Set
`E2E_COLOR=always` or `E2E_COLOR=never` to override auto-detection; `NO_COLOR`
disables color.

Optional real-client `mcp-smoke-agent` prompts default to the OpenAI provider
and let the agent choose its provider-specific default model. Set
`OPENAI_API_KEY` in the environment or `.env` to run them; override
`MCP_SMOKE_AGENT_PROVIDER` or `MCP_SMOKE_AGENT_MODEL` only when a different
provider or model is required.

CI validates that `OPENAI_API_KEY` is accepted by OpenAI before full Kind e2e
on normal PRs, `main`, and manual runs. If validation fails, CI skips Kind e2e
with a warning instead of entering the real-client path with a bad provider key.
Dependabot PRs run `smoke-auth,governance` only, so dependency bumps still
exercise MCP ingress/auth and grant/session behavior without needing provider
API secrets.

The script writes artifacts when `E2E_ARTIFACT_DIR` is set. In CI, those
artifacts are uploaded from `.e2e-artifacts/kind`.

## CI Coverage

The main CI workflow runs:

- formatting check
- `go vet`
- `staticcheck`
- unit and integration tests
- golden CLI tests
- service module tests
- generated file drift
- repository SBOM generation
- Kind e2e for code changes

Security workflows add pinned gosec, Trivy repository/image scans with SARIF
upload, operator-image SBOM artifacts, and pull-request dependency review.

## Pre-commit Hooks

`.pre-commit-config.yaml` mirrors the local contributor gates: Go formatting,
`staticcheck`, `go vet`, targeted Go tests, generated-file drift checks, and a
pinned Gitleaks hook for staged secret detection. Install with
`pre-commit install`; run `pre-commit run --all-files` before pushing when you
want the full local hook suite.

## Choosing Coverage

| Change | Minimum local check |
|---|---|
| Cobra help or flags | `go test ./internal/cli/... ./test/golden/... -count=1` |
| setup, registry, or cluster doctor | targeted `internal/cli` tests plus a local Kind or k3s smoke when behavior affects pulls |
| CRD schema | `make -f Makefile.operator generate manifests`, API tests, operator tests |
| reconciliation | operator tests plus integration or e2e when resource ownership changes |
| gateway policy | service tests plus the governance e2e scenario |
| docs-only | `git diff --check`; MkDocs build when available |
