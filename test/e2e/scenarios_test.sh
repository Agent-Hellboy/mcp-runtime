#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
KIND_SCRIPT="${PROJECT_ROOT}/test/e2e/kind.sh"
SELECT_SCRIPT="${PROJECT_ROOT}/test/e2e/select_pr_scenarios.sh"

run_valid() {
  local name="$1"
  local scenarios="$2"
  local expected="$3"
  local output

  if ! output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_SCENARIOS="${scenarios}" bash "${KIND_SCRIPT}" 2>&1)"; then
    echo "[fail] ${name}: expected validation success" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi

  if ! printf '%s\n' "${output}" | grep -F -q -- "[info] E2E scenarios: ${expected}"; then
    echo "[fail] ${name}: missing selected-scenario output" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi

  echo "[pass] ${name}"
}

run_invalid() {
  local name="$1"
  local scenarios="$2"
  local expected_error="$3"
  local output

  if output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_SCENARIOS="${scenarios}" bash "${KIND_SCRIPT}" 2>&1)"; then
    echo "[fail] ${name}: expected validation failure" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi

  if ! printf '%s\n' "${output}" | grep -F -q -- "${expected_error}"; then
    echo "[fail] ${name}: missing expected error" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi

  echo "[pass] ${name}"
}

run_valid "all" "all" "all"
run_valid "smoke-auth" "smoke-auth" "smoke-auth"
run_valid "governance" "governance" "governance"
run_valid "trust" "trust" "trust"
run_valid "oauth" "oauth" "oauth"
run_valid "api-platform" "api-platform" "api-platform"
run_valid "ui-auth" "ui-auth" "ui-auth"
run_valid "adapter-proxy" "adapter-proxy" "adapter-proxy"
run_valid "cli-platform" "cli-platform" "cli-platform"
run_valid "observability-with-deps" "smoke-auth,governance,trust,oauth,observability" "smoke-auth,governance,trust,oauth,observability"
run_valid "whitespace-trimmed" " smoke-auth , governance " "smoke-auth,governance"
run_valid "duplicates-deduped" "smoke-auth,smoke-auth" "smoke-auth"
run_valid "all-overrides-subsets" "all,smoke-auth" "all"

run_invalid "empty" "" "E2E_SCENARIOS must not be empty"
run_invalid "blank-spaces" "   " "E2E_SCENARIOS must not be empty"
run_invalid "unsupported-token" "smoke-auth,bad" "unsupported E2E scenario: bad"
run_invalid "observability-alone" "observability" "observability requires smoke-auth, governance, trust, and oauth scenarios"
run_invalid "observability-missing-oauth" "smoke-auth,governance,trust,observability" "observability requires smoke-auth, governance, trust, and oauth scenarios"

if ! output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_DEEP_REQUEST_FLOWS=1 E2E_SCENARIOS=all bash "${KIND_SCRIPT}" 2>&1)"; then
  echo "[fail] deep-request-flows-all: expected validation success" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
if ! printf '%s\n' "${output}" | grep -F -q -- "Pre-release deep request-flow checks: enabled"; then
  echo "[fail] deep-request-flows-all: missing deep-mode output" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
echo "[pass] deep-request-flows-all"

if output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_DEEP_REQUEST_FLOWS=1 E2E_SCENARIOS=smoke-auth,governance bash "${KIND_SCRIPT}" 2>&1)"; then
  echo "[fail] deep-request-flows-subset: expected validation failure" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
if ! printf '%s\n' "${output}" | grep -F -q -- "E2E_DEEP_REQUEST_FLOWS=1 requires all E2E scenarios"; then
  echo "[fail] deep-request-flows-subset: missing expected error" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
echo "[pass] deep-request-flows-subset"

for mode in tenant org public; do
  if ! output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_SCENARIOS=smoke-auth E2E_PLATFORM_MODE="${mode}" bash "${KIND_SCRIPT}" 2>&1)"; then
    echo "[fail] platform-mode-${mode}: expected validation success" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  if ! printf '%s\n' "${output}" | grep -F -q -- "[info] E2E platform mode: ${mode}"; then
    echo "[fail] platform-mode-${mode}: missing platform-mode output" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  echo "[pass] platform-mode-${mode}"
done

if output="$(E2E_COLOR=never E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_SCENARIOS=smoke-auth E2E_PLATFORM_MODE=bad bash "${KIND_SCRIPT}" 2>&1)"; then
  echo "[fail] platform-mode-invalid: expected validation failure" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
if ! printf '%s\n' "${output}" | grep -F -q -- "unsupported E2E platform mode: bad"; then
  echo "[fail] platform-mode-invalid: missing expected error" >&2
  printf '%s\n' "${output}" >&2
  exit 1
fi
echo "[pass] platform-mode-invalid"

selector_expect() {
  local name="$1"
  local expected="$2"
  shift 2
  local output

  output="$(printf '%s\n' "$@" | bash "${SELECT_SCRIPT}")"
  if [[ "${output}" != "${expected}" ]]; then
    echo "[fail] selector-${name}: expected ${expected}, got ${output}" >&2
    printf 'paths:\n' >&2
    printf '  %s\n' "$@" >&2
    exit 1
  fi
  echo "[pass] selector-${name}"
}

selector_expect "docs-only" "smoke-auth" "docs/internals/tests.md"
selector_expect "ui" "smoke-auth,ui-auth" "services/ui/main.go"
selector_expect "api" "smoke-auth,api-platform" "services/platform-api/auth/login.go"
selector_expect "runtime-tools-api" "smoke-auth,api-platform,cli-platform" "services/runtime-control/internal/runtimeapi/tools.go"
selector_expect "catalog-cli" "smoke-auth,cli-platform" "internal/cli/catalog/catalog.go"
selector_expect "adapter" "smoke-auth,adapter-proxy,governance" "internal/cli/adapter/proxy.go"
selector_expect "gateway" "smoke-auth,governance,trust,oauth,adapter-proxy" "services/mcp-gateway/main.go"
selector_expect "observability" "smoke-auth,governance,trust,oauth,observability" "services/ingest/main.go"
selector_expect "broad" "all" "api/v1alpha1/mcpserver_types.go"

echo "[pass] scenario selector validation"
