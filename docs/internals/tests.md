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

Image mirroring and local runtime/Sentinel image builds run with bounded
parallelism. `E2E_IMAGE_PREP_PARALLELISM=<n>` tunes the shared prep default,
`E2E_IMAGE_MIRROR_PARALLELISM=<n>` tunes pull/push mirroring, and
`E2E_IMAGE_BUILD_PARALLELISM=<n>` tunes local Docker builds. CI sets mirroring
to one worker to avoid Docker/local-registry push contention on shared runners
and builds to two workers because builds are heavier on runner CPU, memory, and
Docker.
The script also deploys the independent workspace assistant, data utility, and
text analysis sample servers concurrently; scenario assertions remain ordered
because they share policy, session, and analytics state.
Parallel worker output is buffered under `stage-logs/` in the e2e workdir and
copied into `E2E_ARTIFACT_DIR` when artifacts are enabled. Live output prints
colored `START`, `RUNNING`, `DONE`, and `FAILED` lifecycle lines plus short
stdout/stderr previews; tune those previews with `E2E_LOG_PREVIEW_LINES` and
`E2E_LOG_FAILURE_LINES`. Major sequential stages such as local registry startup,
Kind cluster creation, CLI rebuilds, setup, cluster doctor, and MCP server
deploys are also mirrored to `stage-logs/` in the same artifact.

E2E output uses ANSI color for interactive terminals and GitHub Actions by
default. Set `E2E_COLOR=always` or `E2E_COLOR=never` to override
auto-detection; `NO_COLOR` disables color.

Kind e2e traffic uses deterministic curl-based MCP requests. The previous
real-client agent prompts are disabled so CI and local runs do not consume
OpenAI or Anthropic tokens while validating gateway policy, auth, audit, and
observability paths.

The `observability` scenario validates the trace backend through both direct
Tempo and Grafana's Tempo datasource. It must find a single request trace that
contains the gateway service, `mcp-sentinel-ingest`, `mcp-sentinel-processor`,
and the `kafka.produce`, `kafka.consume`, `clickhouse.insert_event`, and
`clickhouse.insert_batch` spans.

Normal PRs, `main`, and manual CI runs execute full Kind e2e with
`E2E_SCENARIOS=all`. Dependabot PRs run `smoke-auth,governance` only, so
dependency bumps still exercise MCP ingress/auth and grant/session behavior
while keeping runtime low.

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
upload, pinned Gitleaks secret scanning, operator-image SBOM artifacts, and
pull-request dependency review.

## Pre-commit Hooks

`.pre-commit-config.yaml` mirrors the local contributor gates: Go formatting,
`staticcheck`, `go vet`, targeted Go tests, generated-file drift checks, and a
pinned Gitleaks hook for staged secret detection. Install with
`pre-commit install`; run `pre-commit run --all-files` before pushing when you
want the full local hook suite. The full suite includes integration hooks that
install envtest assets and set `KUBEBUILDER_ASSETS`.

## Choosing Coverage

| Change | Minimum local check |
|---|---|
| Cobra help or flags | `go test ./internal/cli/... ./test/golden/... -count=1` |
| setup, registry, or cluster doctor | targeted `internal/cli` tests plus a local Kind or k3s smoke when behavior affects pulls |
| CRD schema | `make -f Makefile.operator generate manifests`, API tests, operator tests |
| reconciliation | operator tests plus integration or e2e when resource ownership changes |
| gateway policy | service tests plus the governance e2e scenario |
| docs-only | `git diff --check`; MkDocs build when available |
