---
name: qa-checks
description: Plan, run, and report the right MCP Runtime validation checks for code, docs, CLI, operator, service, integration, and release-readiness changes. Use when Codex is asked to QA a change, verify a fix before PR, choose targeted tests, investigate validation failures, run a deep platform-wide QA pass, or prepare a concise test summary for this repository.
---

# QA Checks

## Overview

Use this skill to turn an MCP Runtime change — or the whole platform — into a
defensible validation plan. Prefer narrow checks while iterating, broaden when
the change touches shared contracts, generated output, cluster behavior, or
user-facing workflows, and switch to platform-wide mode when asked to audit
release readiness or do deep QA.

## Step 1 — Choose a mode

State the mode in the report. Do not silently switch.

- **change-scoped** (default). Drive everything from the diff. Read
  `git status --short`, `git diff --stat`, and per-file diffs. Keep unrelated
  dirty or untracked files out of the validation story unless they affect the
  task.
- **platform-wide**. Ignore the diff. Walk every component, run the full test
  matrix, and report on coverage and gaps. Use this when the user asks for
  release-readiness QA, a deep audit, or "QA the whole platform."

In both modes, treat `AGENTS.md`, `CLAUDE.md`, `docs/internals/tests.md`,
`.github/workflows/ci.yaml`, and package-local test patterns as source truth.

## Step 2 — Choose checks that match the surface

Pick the narrowest meaningful set. In platform-wide mode, run them all.

### Root Go module

- Format: `gofmt -s -l .` (must print nothing).
- Vet: `go vet ./...`.
- Static check: `go install honnef.co/go/tools/cmd/staticcheck@v0.7.0 && $(go env GOPATH)/bin/staticcheck ./...` (CI version).
- Unit: `go test ./<touched-pkgs>/... -race -count=1`. Broaden to `./...`
  when shared contracts changed.
- Build: `go build -o bin/mcp-runtime ./cmd/mcp-runtime` before any command
  that depends on `./bin/mcp-runtime`.

### CLI behavior, flags, help text

- `go test ./internal/cli/... ./test/golden/... -count=1`.
- Live help diff before editing CLI docs: capture
  `./bin/mcp-runtime --help`, `./bin/mcp-runtime <group> --help`, and
  `./bin/mcp-runtime <group> <subcommand> --help`. If golden files drift
  intentionally, regenerate per `docs/internals/tests.md`.
- CLI UX matrix (platform-wide, or when error UX changed): for every Cobra
  command, exercise `--help`, missing required arg, unknown flag, and one
  invalid value; assert exit code and that the message goes through
  `internal/cli/core/errors.go` / `pkg/errx/`.

### Operator and CRDs

- `go test ./internal/operator/... -race -count=1`.
- `go test ./test/integration/... -count=1` (needs `KUBEBUILDER_ASSETS`;
  `pre-commit run --all-files` installs envtest assets).
- Generated-file drift: `make generate manifests && git diff --exit-code
  api/ config/crd/bases/` (or the equivalent `controller-gen` invocation in
  `Makefile.operator`). Drift is a failure even if tests pass.
- Upgrade/migration sanity (platform-wide, or when CRD schemas changed):
  apply the previous release's CRDs, upgrade to HEAD, reapply representative
  `MCPServer` / `MCPAccessGrant` / `MCPAgentSession` objects, and confirm
  reconciliation completes without manual edits.

### Sentinel services (separate `go.mod` per service)

For each touched module under `services/<name>/`:

- `cd services/<name> && go test -race -count=1 ./...`.
- When the service ships a Dockerfile and CI builds an image: `docker build
  -f services/<name>/Dockerfile -t <name>:ci services/<name>` to catch
  multi-stage and base-image regressions before CI.

### Gateway policy / runtime governance

- Service tests as above.
- `./bin/mcp-runtime server policy inspect <name> --namespace <ns>` after
  applying or editing a grant/session. Allow ~10s for the proxy poll loop;
  do not conclude `session_not_found` without that wait.
- `E2E_CACHE_MODE=1 E2E_SCENARIOS=governance bash test/e2e/kind.sh` for
  request-path checks against a live cluster.

### Metadata / manifests / registry / setup flow

- `go test ./pkg/metadata/... ./pkg/manifest/... ./pkg/k8sclient/... ./internal/cli/... -count=1`.
- Smoke run only when image pulls, ingress, registry, or setup behavior
  changed: `kind create cluster --name mcp-runtime --config /tmp/mcp-runtime-kind.yaml`
  then `MCP_SETUP_WAIT_TIMEOUT=900 ./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http`.

### E2E (Kind)

- Default smoke: `E2E_SCENARIOS=smoke-auth,governance bash test/e2e/kind.sh`
  matches the Dependabot CI matrix.
- Full pass (platform-wide or release): `E2E_SCENARIOS=all bash test/e2e/kind.sh`.
- Re-debug fast: `E2E_CACHE_MODE=1 E2E_SCENARIOS=<scenario> bash test/e2e/kind.sh`
  reuses cluster and cached images.

### Docs and agent skills

- `git diff --check` for whitespace.
- For pages that show CLI commands (`docs/cli.md`, `docs/getting-started.md`,
  `docs/publish-mcp-server.md`), compare prose against live `--help` output,
  not memory.
- For `website/`: build the site if the doc tree has one configured.

## Step 3 — Deep-QA techniques (use in platform mode, or when a change warrants)

These are not part of routine PR validation; pull them in when the user asks
for deep QA, or when the change touches a parser, controller, request path,
or release artifact.

### Fuzzing

Go has native fuzz support. Trust-boundary parsers are the prime targets:

- JSON-RPC parsing in the gateway/proxy.
- Manifest/YAML loaders in `pkg/manifest/`.
- Host resolver in `pkg/metadata/host_resolve.go` (split between dev HTTP and
  prod TLS).
- Grant/session policy renderer in `pkg/access/`.

Enumerate `Fuzz*` functions:
`grep -rIn '^func Fuzz' --include='*.go' .`

Run each for a bounded duration:
`go test -run='^$' -fuzz='Fuzz<Name>' -fuzztime=60s ./<pkg>`

Flag packages at trust boundaries that ship no fuzz targets — that is itself a
QA finding.

### Flake detection

Race conditions hide under "passed once."

- `go test -count=20 -race ./<touched-pkgs>/...`, or
- `gotestsum --rerun-fails=3 --packages='./...'` if installed.
- For controller-runtime reconcilers and gateway session maps, run the
  package-local test under `-race -count=50` overnight when contention is
  suspected.

### Coverage gates

- `go test -coverprofile=/tmp/cov.out -covermode=atomic ./<pkg>/...`
- `go tool cover -func=/tmp/cov.out | sort -k3 -n` and inspect the bottom.
- For changed files: filter the `func` output to lines whose path is in
  `git diff --name-only`. Functions at `0.0%` covering an error branch is a
  finding, not a number to reach.

### Concurrency-specific tests

Beyond `-race`:

- Parallel `tools/call` against the gateway with a shared session:
  bulk-traffic Python loop in `CLAUDE.md` adapted to N goroutines.
- Reconciler under contention: queue `MCPServer` updates faster than the
  default work-queue rate-limiter and assert eventual convergence.

### Cross-version matrix (release readiness)

- Kind k8s versions: matrix via `kind create cluster --image kindest/node:v1.29.x`,
  `v1.30.x`, `v1.31.x`. Re-run `E2E_SCENARIOS=smoke-auth,governance` per node
  image.
- k3s parity: install the documented `registries.yaml` from `CLAUDE.md` and
  run setup in `--test-mode`.

### Chaos / failure injection

Apply only when the change touches lifecycle, ingress, or storage:

- Kill the operator pod mid-reconcile:
  `kubectl -n mcp-runtime delete pod -l control-plane=controller-manager`.
- Restart the registry during an image push:
  `kubectl -n registry rollout restart sts/registry`.
- Drop the API mid-grant-apply:
  `kubectl -n mcp-sentinel scale deploy/mcp-sentinel-api --replicas=0`,
  apply, scale back, assert convergence.

### Golden-file staleness

Lists `testdata/*.golden` whose generator output drifted but the file was not
regenerated:

```sh
for f in $(git ls-files '**/testdata/*.golden'); do
  pkg="$(dirname "$f" | sed 's|/testdata$||')"
  go test -run='Golden|TestHelp|TestCLI' -count=1 -update "./$pkg" 2>/dev/null
done
git diff --stat -- '**/testdata/*.golden'
```

A non-empty diff is a stale golden. Land the regeneration in the same PR as
the behavior change.

## Step 4 — Run order

- Format and `go vet` first; they fail fast and cheap.
- Package tests for touched packages before broadening.
- Build the CLI before any command that depends on `./bin/mcp-runtime`.
- Service tests (`services/*/`) only after their module compiles cleanly.
- Integration tests need `KUBEBUILDER_ASSETS`; if missing, record the
  command (`pre-commit run --all-files`) and continue.
- E2E last, and only when warranted; reserve full Kind for setup, ingress,
  registry, policy, observability, or cluster lifecycle changes.
- If a required tool or environment is missing, record the exact blocker
  and the install command.

## Step 5 — Report

- Lead with mode (change-scoped or platform-wide), commit SHA, and a one-line
  verdict.
- One row per command: name, pass/fail, duration when relevant, and the
  important failure line if it failed. Do not paste full logs.
- Separate scanner-style failures from behavioral test failures.
- Call out tests that pass but cover near-zero of the changed branches.
- Explain why any high-cost or environment-dependent check was skipped.
- When tests fail because of pre-existing or unrelated worktree state, say so
  and separate it from the current change.
- In platform-wide mode, include a coverage-gap section: what the matrix did
  not exercise, and the next step that would close the gap.
