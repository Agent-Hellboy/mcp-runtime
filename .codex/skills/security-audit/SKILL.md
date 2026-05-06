---
name: security-audit
description: Audit MCP Runtime changes for security risks and choose the relevant local or CI security checks. Use when Codex reviews or changes authentication, authorization, secrets, TLS, ingress, registry/image handling, Kubernetes RBAC or pod security, gateway policy, audit logging, dependencies, GitHub Actions, container builds, or any code path that accepts untrusted input.
---

# Security Audit

## Overview

Use this skill to perform a focused security review for MCP Runtime changes. Combine manual threat review with the repository's existing scanners, and keep findings grounded in concrete file and line references.

## Workflow

1. Define the security boundary.
   - Inspect `git status --short`, `git diff --stat`, and diffs for touched files.
   - Identify whether the change affects trust boundaries: user input, JSON/YAML parsing, HTTP handlers, auth cookies or headers, Kubernetes API calls, shell commands, image references, registry endpoints, ingress hosts, TLS material, audit events, or CI workflows.
   - Read nearby tests and existing implementations before judging a pattern unsafe.

2. Review MCP Runtime security invariants.
   - Secrets must come from Kubernetes Secrets, environment references, local test fixtures, or documented dev-only setup; do not hardcode real credentials or log token values.
   - Admin and platform APIs must enforce the intended role, namespace, grant, session, and trust checks before mutating resources or returning sensitive data.
   - Gateway policy should deny by default, scope decisions to the rendered grant/session policy, and emit audit events for allow and deny paths without leaking credentials.
   - Public ingress and TLS changes must preserve the documented `registry.<domain>`, `mcp.<domain>`, and `platform.<domain>` split; do not expose Grafana or Prometheus on the public platform host without an auth-aware design.
   - Registry and image changes must respect dev HTTP versus production TLS behavior, image-pull-secret handling, and the single supported `registry/registry-cert` owner for `registry/registry-tls`.
   - Kubernetes manifests should keep RBAC narrow, avoid privileged pods unless explicitly justified, preserve restricted pod-security labels where the repo owns namespaces, and avoid broad secret access.
   - HTML, JavaScript, logs, and API responses must escape untrusted content and avoid reflecting secrets, auth headers, session cookies, JWTs, OIDC tokens, or API keys.
   - GitHub Actions and dependency changes should keep actions pinned, minimize token permissions, and avoid exposing repository or deploy secrets to untrusted PR code.

3. Choose checks that match the risk.
   - Baseline secret scan: `pre-commit run gitleaks --all-files` when pre-commit is available, or `gitleaks detect --source . --config .gitleaks.toml --redact` when the CLI is installed.
   - Go security scan: install the pinned CI version if needed with `go install github.com/securego/gosec/v2/cmd/gosec@v2.26.1`, then run `$(go env GOPATH)/bin/gosec ./...`.
   - Trivy repository scan: when Trivy is installed, mirror CI with `trivy fs --scanners vuln,secret,license,misconfig --severity CRITICAL,HIGH --ignore-unfixed --skip-dirs inspirations/mcp-gateway-registry .`.
   - Dependency or workflow change: review `.github/workflows/dependency-review.yaml`, Go module diffs, and any new action permissions or secret usage.
   - Auth, policy, or gateway change: run the nearest unit tests plus the governance or smoke-auth E2E path when the live request path changed.

4. Report like a security review.
   - Lead with findings ordered by severity. Include file and line, exploit path, impact, and a specific fix.
   - Separate scanner failures from manual findings.
   - If no issues are found, say that directly and list the checks run plus any checks skipped because tooling or cluster prerequisites were unavailable.
