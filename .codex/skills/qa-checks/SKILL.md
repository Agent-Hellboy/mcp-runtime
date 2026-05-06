---
name: qa-checks
description: Plan, run, and report the right MCP Runtime validation checks for code, docs, CLI, operator, service, integration, and release-readiness changes. Use when Codex is asked to QA a change, verify a fix before PR, choose targeted tests, investigate validation failures, or prepare a concise test summary for this repository.
---

# QA Checks

## Overview

Use this skill to turn an MCP Runtime change into a defensible validation plan. Prefer narrow checks while iterating, then broaden when the change touches shared contracts, generated output, cluster behavior, or user-facing workflows.

## Workflow

1. Inspect the change surface before choosing commands.
   - Read `git status --short`, `git diff --stat`, and relevant file diffs.
   - Treat `AGENTS.md`, `docs/internals/tests.md`, `.github/workflows/ci.yaml`, and package-local test patterns as source truth.
   - Keep unrelated dirty or untracked files out of the validation story unless they affect the task.

2. Choose the narrowest meaningful checks.
   - Root Go code: `gofmt -s -l <files>`, package tests for touched packages, then `go test ./... -count=1 -race` when shared contracts or broad behavior changed.
   - CLI behavior, flags, or help: `go build -o bin/mcp-runtime ./cmd/mcp-runtime`, `go test ./internal/cli/... ./test/golden/... -count=1`, and live `./bin/mcp-runtime ... --help` output before editing CLI docs or golden files.
   - Operator or CRD behavior: `go test ./internal/operator/... -race -count=1`; add `go test ./test/integration/... -count=1` when envtest-backed Kubernetes API behavior matters.
   - API or generated types: `go test ./api/v1alpha1/... -count=1` and generated-file drift checks when CRD schemas or deepcopy output change.
   - Metadata, manifests, registry, or setup flow: targeted tests in `./pkg/metadata/...`, `./pkg/manifest/...`, `./pkg/k8sclient/...`, and `./internal/cli/...`; use Kind or k3s smoke checks only when image pulls, ingress, registry, or setup behavior changed.
   - Sentinel services: run `go test -race -count=1 ./...` inside each touched service module such as `services/api`, `services/ui`, `services/ingest`, `services/processor`, or `services/mcp-proxy`.
   - Gateway policy or runtime governance: include service tests plus the governance or smoke-auth E2E scenario when request-path behavior, grants, sessions, or audit emission changed.
   - Docs-only or agent-skill changes: run `git diff --check`; run docs build or link checks when the edited doc tree has one.

3. Run checks in a useful order.
   - Start with format and package-level tests that fail fast.
   - Build the CLI before commands that depend on `./bin/mcp-runtime`.
   - Avoid full Kind E2E as a reflex; reserve it for setup, ingress, registry, policy, observability, or cluster lifecycle changes.
   - If a required tool or environment is missing, record the exact blocker and the command that should be run.

4. Report results compactly.
   - Name each command, whether it passed or failed, and the important failure line if it failed.
   - Explain why any high-cost or environment-dependent check was skipped.
   - When tests fail because of pre-existing or unrelated worktree state, say so and separate it from the current change.
