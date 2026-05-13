#!/usr/bin/env bash
set -euo pipefail

# End-to-end check on a fresh kind cluster:
# - build the CLI and publish runtime/sentinel images to a local docker mirror registry
# - run `mcp-runtime setup --test-mode`
# - deploy a policy-enabled MCP server through the CLI pipeline flow
# - exercise the deployed server through curl-based smoke checks and targeted MCP requests
# - verify audit events plus trace/log backends
#
# Set E2E_SCENARIOS to a comma-separated subset for local debugging.
# Supported values: all, smoke-auth, governance, trust, oauth, observability, multitenancy.
# observability requires the full traffic suite: smoke-auth, governance, trust, oauth.
#
# For repeated local debugging, set E2E_CACHE_MODE=1. Cache mode implies
# E2E_KEEP_CLUSTER=1, reuses an existing kind cluster and local registry, skips
# platform setup when the core platform is already ready, and reuses cached
# image tags from the local registry instead of pulling/building them again.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

E2E_COLOR="${E2E_COLOR:-auto}"
export E2E_COLOR

e2e_color_enabled() {
  if [[ -n "${NO_COLOR:-}" ]]; then
    return 1
  fi

  case "${E2E_COLOR}" in
    always|1|true|yes|on)
      return 0
      ;;
    never|0|false|no|off)
      return 1
      ;;
    auto|"")
      [[ -t 1 || "${GITHUB_ACTIONS:-}" == "true" ]]
      ;;
    *)
      [[ -t 1 ]]
      ;;
  esac
}

E2E_COLOR_RESET=""
E2E_COLOR_INFO=""
E2E_COLOR_MCP=""
E2E_COLOR_POLICY=""
E2E_COLOR_WARN=""
E2E_COLOR_DEBUG=""
E2E_COLOR_SUCCESS=""
if e2e_color_enabled; then
  E2E_COLOR_RESET=$'\033[0m'
  E2E_COLOR_INFO=$'\033[36m'
  E2E_COLOR_MCP=$'\033[35m'
  E2E_COLOR_POLICY=$'\033[33m'
  E2E_COLOR_WARN=$'\033[31m'
  E2E_COLOR_DEBUG=$'\033[2m'
  E2E_COLOR_SUCCESS=$'\033[32m'
fi

log_line() {
  local tag="$1"
  shift
  local color="${E2E_COLOR_INFO}"
  case "${tag}" in
    mcp)
      color="${E2E_COLOR_MCP}"
      ;;
    policy)
      color="${E2E_COLOR_POLICY}"
      ;;
    error|fail|retry)
      color="${E2E_COLOR_WARN}"
      ;;
    debug)
      color="${E2E_COLOR_DEBUG}"
      ;;
  esac

  printf '%b[%s]%b %s\n' "${color}" "${tag}" "${E2E_COLOR_RESET}" "$*"
}

log_line info "Running from: ${PROJECT_ROOT}"
export E2E_HELPERS="${PROJECT_ROOT}/test/e2e/e2e_helpers.py"

SENTINEL_ROOT="${PROJECT_ROOT}"
if [[ ! -d "${SENTINEL_ROOT}/services" || ! -d "${SENTINEL_ROOT}/k8s" ]]; then
  echo "expected flattened services/ and k8s/ layout under ${SENTINEL_ROOT}" >&2
  exit 1
fi
log_line info "Sentinel root: ${SENTINEL_ROOT}"

CLUSTER_NAME="${CLUSTER_NAME:-mcp-e2e}"
PLATFORM_HOST="${PLATFORM_HOST:-localhost}"
SERVER_NAME="${SERVER_NAME:-policy-mcp-server}"
SERVER_HOST="${SERVER_HOST:-${PLATFORM_HOST}}"
OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME:-oauth-mcp-server}"
OAUTH_SERVER_HOST="${OAUTH_SERVER_HOST:-${PLATFORM_HOST}}"
PYTHON_EXAMPLE_SERVER_NAME="${PYTHON_EXAMPLE_SERVER_NAME:-python-example-mcp}"
PYTHON_EXAMPLE_SERVER_HOST="${PYTHON_EXAMPLE_SERVER_HOST:-${PLATFORM_HOST}}"
PYTHON_EXAMPLE_SERVER_ROUTE="${PYTHON_EXAMPLE_SERVER_ROUTE:-/${PYTHON_EXAMPLE_SERVER_NAME}/mcp}"
RUST_EXAMPLE_SERVER_NAME="${RUST_EXAMPLE_SERVER_NAME:-rust-example-mcp}"
RUST_EXAMPLE_SERVER_HOST="${RUST_EXAMPLE_SERVER_HOST:-${PLATFORM_HOST}}"
RUST_EXAMPLE_SERVER_ROUTE="${RUST_EXAMPLE_SERVER_ROUTE:-/${RUST_EXAMPLE_SERVER_NAME}/mcp}"
GO_EXAMPLE_SERVER_NAME="${GO_EXAMPLE_SERVER_NAME:-go-example-mcp}"
GO_EXAMPLE_SERVER_HOST="${GO_EXAMPLE_SERVER_HOST:-${PLATFORM_HOST}}"
GO_EXAMPLE_SERVER_ROUTE="${GO_EXAMPLE_SERVER_ROUTE:-/${GO_EXAMPLE_SERVER_NAME}/mcp}"
MT_TENANT_A="${MT_TENANT_A:-mt-tenant-a}"
MT_TENANT_B="${MT_TENANT_B:-mt-tenant-b}"
MT_HUMAN_A="${MT_HUMAN_A:-alice}"
MT_AGENT_A="${MT_AGENT_A:-alice-agent}"
MT_SESSION_A="${MT_SESSION_A:-alice-session}"
MT_HUMAN_B="${MT_HUMAN_B:-bob}"
MT_AGENT_B="${MT_AGENT_B:-bob-agent}"
MT_SESSION_B="${MT_SESSION_B:-bob-session}"
HUMAN_ID="${HUMAN_ID:-user-123}"
AGENT_ID="${AGENT_ID:-ops-agent}"
SESSION_ID="${SESSION_ID:-sess-ops-agent}"
OAUTH_HUMAN_ID="${OAUTH_HUMAN_ID:-oauth-user-123}"
OAUTH_AGENT_ID="${OAUTH_AGENT_ID:-oauth-client}"
OAUTH_SESSION_ID="${OAUTH_SESSION_ID:-oauth-session-1}"
OAUTH_AUDIENCE="${OAUTH_AUDIENCE:-mcp-runtime-e2e}"
OAUTH_ISSUER_NAME="${OAUTH_ISSUER_NAME:-oauth-issuer}"
OAUTH_ISSUER_URL="http://${OAUTH_ISSUER_NAME}.mcp-servers.svc.cluster.local:8080"
TRAEFIK_PORT="${TRAEFIK_PORT:-18080}"
SENTINEL_PORT="${SENTINEL_PORT:-18083}"
TEMPO_PORT="${TEMPO_PORT:-13200}"
LOKI_PORT="${LOKI_PORT:-13100}"
API_SERVICE_PORT="${API_SERVICE_PORT:-18091}"
UI_SERVICE_PORT="${UI_SERVICE_PORT:-18092}"
INGEST_SERVICE_PORT="${INGEST_SERVICE_PORT:-18093}"
SERVER_PROXY_PORT="${SERVER_PROXY_PORT:-18094}"
SERVER_UPSTREAM_PORT="${SERVER_UPSTREAM_PORT:-18095}"
OAUTH_PROXY_PORT="${OAUTH_PROXY_PORT:-18096}"
OAUTH_UPSTREAM_PORT="${OAUTH_UPSTREAM_PORT:-18097}"
PYTHON_EXAMPLE_PROXY_PORT="${PYTHON_EXAMPLE_PROXY_PORT:-18098}"
RUST_EXAMPLE_PROXY_PORT="${RUST_EXAMPLE_PROXY_PORT:-18099}"
GO_EXAMPLE_PROXY_PORT="${GO_EXAMPLE_PROXY_PORT:-18102}"
CLI_SENTINEL_API_PORT="${CLI_SENTINEL_API_PORT:-18103}"
API_METRICS_PORT="${API_METRICS_PORT:-19090}"
INGEST_METRICS_PORT="${INGEST_METRICS_PORT:-19091}"
PROCESSOR_METRICS_PORT="${PROCESSOR_METRICS_PORT:-19092}"
PLATFORM_ADMIN_EMAIL="${PLATFORM_ADMIN_EMAIL:-admin@mcpruntime.org}"
PLATFORM_ADMIN_PASSWORD="${PLATFORM_ADMIN_PASSWORD:-admin@123}"
MCP_CURL_TIMEOUT="${MCP_CURL_TIMEOUT:-${MCP_SMOKE_TIMEOUT:-20}}"
# Keep the old MCP_SMOKE_* environment variables as aliases for local scripts
# that override these proxy ports or timeout.
MCP_CURL_ANON_PORT="${MCP_CURL_ANON_PORT:-${MCP_SMOKE_ANON_PORT:-18084}}"
MCP_CURL_IDENTITY_PORT="${MCP_CURL_IDENTITY_PORT:-${MCP_SMOKE_IDENTITY_PORT:-18085}}"
MCP_CURL_SESSION_PORT="${MCP_CURL_SESSION_PORT:-${MCP_SMOKE_SESSION_PORT:-18086}}"
MCP_CURL_BAD_SESSION_PORT="${MCP_CURL_BAD_SESSION_PORT:-${MCP_SMOKE_BAD_SESSION_PORT:-18087}}"
MCP_CURL_OAUTH_ANON_PORT="${MCP_CURL_OAUTH_ANON_PORT:-${MCP_SMOKE_OAUTH_ANON_PORT:-18088}}"
MCP_CURL_OAUTH_INVALID_PORT="${MCP_CURL_OAUTH_INVALID_PORT:-${MCP_SMOKE_OAUTH_INVALID_PORT:-18089}}"
MCP_CURL_OAUTH_VALID_PORT="${MCP_CURL_OAUTH_VALID_PORT:-${MCP_SMOKE_OAUTH_VALID_PORT:-18090}}"
MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION:-2025-06-18}"
MCP_POLICY_WAIT_TRIES="${MCP_POLICY_WAIT_TRIES:-90}"
RAW_REQUEST_TRIES="${RAW_REQUEST_TRIES:-10}"
UNKNOWN_SESSION_ID="${UNKNOWN_SESSION_ID:-sess-does-not-exist}"
TEST_MODE_REGISTRY_IMAGE="${TEST_MODE_REGISTRY_IMAGE:-docker.io/library/mcp-runtime-registry:latest}"
LOCAL_REGISTRY_NAME="${LOCAL_REGISTRY_NAME:-${CLUSTER_NAME}-dockerhub-mirror}"
LOCAL_REGISTRY_PORT="${LOCAL_REGISTRY_PORT:-5001}"
LOCAL_REGISTRY_PUSH_HOST="${LOCAL_REGISTRY_PUSH_HOST:-127.0.0.1:${LOCAL_REGISTRY_PORT}}"
LOCAL_REGISTRY_MIRROR_ENDPOINT="${LOCAL_REGISTRY_NAME}:5000"
LOCAL_REGISTRY_RETRY_TRIES="${LOCAL_REGISTRY_RETRY_TRIES:-5}"
LOCAL_REGISTRY_RETRY_DELAY="${LOCAL_REGISTRY_RETRY_DELAY:-5}"
E2E_WORKLOAD_TAG="${E2E_WORKLOAD_TAG:-e2e}"
E2E_ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-}"
E2E_SCENARIOS="${E2E_SCENARIOS-all}"
E2E_SCENARIOS="${E2E_SCENARIOS//[[:space:]]/}"
E2E_PLATFORM_MODE="${E2E_PLATFORM_MODE:-tenant}"
E2E_PLATFORM_MODE="${E2E_PLATFORM_MODE//[[:space:]]/}"
E2E_VALIDATE_SCENARIOS_ONLY="${E2E_VALIDATE_SCENARIOS_ONLY:-0}"
E2E_KEEP_CLUSTER="${E2E_KEEP_CLUSTER:-0}"
E2E_CACHE_MODE="${E2E_CACHE_MODE:-0}"
E2E_IMAGE_PREP_PARALLELISM="${E2E_IMAGE_PREP_PARALLELISM:-3}"
E2E_IMAGE_MIRROR_PARALLELISM="${E2E_IMAGE_MIRROR_PARALLELISM:-${E2E_IMAGE_PREP_PARALLELISM}}"
E2E_IMAGE_BUILD_PARALLELISM="${E2E_IMAGE_BUILD_PARALLELISM:-}"
E2E_LOG_PREVIEW_LINES="${E2E_LOG_PREVIEW_LINES:-4}"
E2E_LOG_FAILURE_LINES="${E2E_LOG_FAILURE_LINES:-40}"
if [[ "${E2E_CACHE_MODE}" == "1" ]]; then
  E2E_KEEP_CLUSTER=1
fi

if ! [[ "${E2E_IMAGE_PREP_PARALLELISM}" =~ ^[0-9]+$ ]] || [[ "${E2E_IMAGE_PREP_PARALLELISM}" -lt 1 ]]; then
  echo "E2E_IMAGE_PREP_PARALLELISM must be a positive integer" >&2
  exit 1
fi
if [[ -z "${E2E_IMAGE_BUILD_PARALLELISM}" ]]; then
  if [[ "${E2E_IMAGE_PREP_PARALLELISM}" -lt 2 ]]; then
    E2E_IMAGE_BUILD_PARALLELISM="${E2E_IMAGE_PREP_PARALLELISM}"
  else
    E2E_IMAGE_BUILD_PARALLELISM=2
  fi
fi
if ! [[ "${E2E_IMAGE_MIRROR_PARALLELISM}" =~ ^[0-9]+$ ]] || [[ "${E2E_IMAGE_MIRROR_PARALLELISM}" -lt 1 ]]; then
  echo "E2E_IMAGE_MIRROR_PARALLELISM must be a positive integer" >&2
  exit 1
fi
if ! [[ "${E2E_IMAGE_BUILD_PARALLELISM}" =~ ^[0-9]+$ ]] || [[ "${E2E_IMAGE_BUILD_PARALLELISM}" -lt 1 ]]; then
  echo "E2E_IMAGE_BUILD_PARALLELISM must be a positive integer" >&2
  exit 1
fi
if ! [[ "${E2E_LOG_PREVIEW_LINES}" =~ ^[0-9]+$ ]]; then
  echo "E2E_LOG_PREVIEW_LINES must be zero or a positive integer" >&2
  exit 1
fi
if ! [[ "${E2E_LOG_FAILURE_LINES}" =~ ^[0-9]+$ ]]; then
  echo "E2E_LOG_FAILURE_LINES must be zero or a positive integer" >&2
  exit 1
fi

IFS=',' read -r -a E2E_SCENARIO_LIST <<< "${E2E_SCENARIOS}"
if [[ ${#E2E_SCENARIO_LIST[@]} -eq 0 || -z "${E2E_SCENARIO_LIST[0]}" ]]; then
  echo "E2E_SCENARIOS must not be empty" >&2
  exit 1
fi

# Deduplicate while preserving order. Keep this Bash 3 compatible for macOS.
declare -a _e2e_deduped=()
for _e2e_s in "${E2E_SCENARIO_LIST[@]}"; do
  _e2e_seen=0
  if [[ ${#_e2e_deduped[@]} -gt 0 ]]; then
    for _e2e_existing in "${_e2e_deduped[@]}"; do
      if [[ "${_e2e_existing}" == "${_e2e_s}" ]]; then
        _e2e_seen=1
        break
      fi
    done
  fi
  if [[ "${_e2e_seen}" -eq 0 ]]; then
    _e2e_deduped+=("${_e2e_s}")
  fi
done
E2E_SCENARIO_LIST=("${_e2e_deduped[@]}")
unset _e2e_deduped _e2e_existing _e2e_seen _e2e_s

scenario_requested() {
  local wanted="$1"
  local scenario
  for scenario in "${E2E_SCENARIO_LIST[@]}"; do
    if [[ "${scenario}" == "${wanted}" ]]; then
      return 0
    fi
  done
  return 1
}

scenario_selected() {
  local wanted="$1"
  if scenario_requested "all"; then
    return 0
  fi
  scenario_requested "${wanted}"
}

validate_scenarios() {
  case "${E2E_PLATFORM_MODE}" in
    tenant|org|public)
      ;;
    *)
      echo "unsupported E2E platform mode: ${E2E_PLATFORM_MODE}" >&2
      echo "supported values: tenant, org, public" >&2
      exit 1
      ;;
  esac

  local scenario
  for scenario in "${E2E_SCENARIO_LIST[@]}"; do
    case "${scenario}" in
      all|smoke-auth|governance|trust|oauth|observability|multitenancy)
        ;;
      *)
        echo "unsupported E2E scenario: ${scenario}" >&2
        echo "supported values: all, smoke-auth, governance, trust, oauth, observability, multitenancy" >&2
        exit 1
        ;;
    esac
  done

  if scenario_selected "observability"; then
    local dependency
    for dependency in smoke-auth governance trust oauth; do
      if ! scenario_selected "${dependency}"; then
        echo "observability requires smoke-auth, governance, trust, and oauth scenarios" >&2
        exit 1
      fi
    done
  fi
}

describe_selected_scenarios() {
  if scenario_requested "all"; then
    echo "all"
    return
  fi

  local IFS=','
  echo "${E2E_SCENARIO_LIST[*]}"
}

validate_scenarios
log_line info "E2E scenarios: $(describe_selected_scenarios)"
log_line info "E2E platform mode: ${E2E_PLATFORM_MODE}"
log_line info "Local smoke/governance checks use direct curl-based MCP HTTP; OpenAI/Anthropic real-client agent checks are disabled"
if [[ "${E2E_VALIDATE_SCENARIOS_ONLY}" == "1" ]]; then
  exit 0
fi

git config --global --add safe.directory "${PROJECT_ROOT}" >/dev/null 2>&1 || true

WORKDIR="$(mktemp -d)"
STAGE_LOG_DIR="${WORKDIR}/stage-logs"
KIND_CONFIG="$(mktemp)"
ORIG_CONTEXT="$(kubectl config current-context 2>/dev/null || true)"
PIDS=()
PARALLEL_PIDS=()
PARALLEL_LABELS=()
PARALLEL_LOGS=()
PARALLEL_STDOUT_LOGS=()
PARALLEL_STDERR_LOGS=()
PARALLEL_STARTED_AT=()
PARALLEL_FAILED=0
PARALLEL_SEQ=0
STAGE_SEQ=0

cleanup() {
  if [[ -n "${E2E_ARTIFACT_DIR}" ]]; then
    mkdir -p "${E2E_ARTIFACT_DIR}"
    if [[ -d "${WORKDIR}" ]]; then
      cp -R "${WORKDIR}/." "${E2E_ARTIFACT_DIR}/" 2>/dev/null || true
    fi
    if [[ -f "${KIND_CONFIG}" ]]; then
      cp "${KIND_CONFIG}" "${E2E_ARTIFACT_DIR}/kind-config.yaml" 2>/dev/null || true
    fi
  fi
  for pid in "${PIDS[@]:-}"; do
    kill "${pid}" >/dev/null 2>&1 || true
    wait "${pid}" 2>/dev/null || true
  done
  kubectl config use-context "${ORIG_CONTEXT}" >/dev/null 2>&1 || true
  if [[ "${E2E_KEEP_CLUSTER}" == "1" ]]; then
    echo "[info] leaving cluster ${CLUSTER_NAME}, registry ${LOCAL_REGISTRY_NAME}, and workdir ${WORKDIR} because E2E_KEEP_CLUSTER=1" >&2
    echo "[info] kind config preserved at ${KIND_CONFIG}" >&2
    return
  fi
  kind delete cluster --name "${CLUSTER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1 || true
  rm -rf "${WORKDIR}"
  rm -f "${KIND_CONFIG}"
}
trap cleanup EXIT

wait_port() {
  local port="$1"
  local tries="${2:-60}"
  local i
  for i in $(seq 1 "${tries}"); do
    if port_is_listening "${port}"; then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for localhost:${port}" >&2
  return 1
}

port_is_listening() {
  local port="$1"
  (echo >/dev/tcp/127.0.0.1/"${port}") >/dev/null 2>&1
}

require_port_available() {
  local port="$1"
  local label="$2"

  if port_is_listening "${port}"; then
    echo "[error] localhost:${port} is already in use before starting ${label}" >&2
    echo "[error] stop the existing listener or override the matching E2E port environment variable" >&2
    return 1
  fi
}

wait_managed_port() {
  local port="$1"
  local pid="$2"
  local log_file="$3"
  local label="$4"
  local tries="${5:-60}"
  local i

  for i in $(seq 1 "${tries}"); do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      echo "[error] ${label} exited before localhost:${port} became available" >&2
      if [[ -s "${log_file}" ]]; then
        echo "[debug] ${label} log:" >&2
        cat "${log_file}" >&2 || true
      fi
      return 1
    fi
    if port_is_listening "${port}"; then
      return 0
    fi
    sleep 1
  done

  echo "timed out waiting for ${label} on localhost:${port}" >&2
  if [[ -s "${log_file}" ]]; then
    echo "[debug] ${label} log:" >&2
    cat "${log_file}" >&2 || true
  fi
  return 1
}

wait_http() {
  local url="$1"
  local header="${2:-}"
  local tries="${3:-60}"
  local i
  for i in $(seq 1 "${tries}"); do
    local curl_args=(-fsS "${url}")
    if [[ -n "${header}" ]]; then
      curl_args=(-fsS -H "${header}" "${url}")
    fi
    if curl "${curl_args[@]}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${url}" >&2
  return 1
}

# Run rollout status with diagnostics on failure.
rollout_status_with_logs() {
  local namespace="$1"
  local kind="$2"
  local name="$3"
  local timeout="$4"

  set +e
  kubectl rollout status "${kind}/${name}" -n "${namespace}" --timeout="${timeout}"
  local status=$?
  set -e

  if [[ ${status} -ne 0 ]]; then
    echo "[debug] rollout failed for ${kind}/${name}; collecting diagnostics" >&2
    kubectl describe "${kind}/${name}" -n "${namespace}" || true
    kubectl get pods -n "${namespace}" -l "app=${name}" -o wide || true
    kubectl describe pods -n "${namespace}" -l "app=${name}" || true
    kubectl logs -n "${namespace}" -l "app=${name}" --all-containers=true --tail=200 || true
    kubectl logs -n "${namespace}" "deploy/${name}" --all-containers=true --tail=200 || true
  fi

  return ${status}
}

assert_file_contains() {
  local needle="$1"
  local file="$2"

  if command -v rg >/dev/null 2>&1; then
    rg -F -q -- "${needle}" "${file}"
    return
  fi

  grep -F -q -- "${needle}" "${file}"
}

run_cli_allowing_cert_prereq_failure() {
  local name="$1"
  shift
  local log_file="${WORKDIR}/${name}.log"

  if "$@" >"${log_file}" 2>&1; then
    echo "[cli][pass] ${name}"
    return 0
  fi

  if grep -E -q "cert-manager|Certificate not found|Certificate not ready|certificate not ready|ClusterIssuer .* not found|CA secret .* not found" "${log_file}"; then
    echo "[cli][pass] ${name} reached expected missing TLS prerequisite path"
    return 0
  fi

  echo "[cli][fail] ${name}" >&2
  cat "${log_file}" >&2
  exit 1
}

decode_base64() {
  if base64 --help 2>/dev/null | grep -q -- "--decode"; then
    base64 --decode
  else
    base64 -D
  fi
}

port_forward_bg() {
  local namespace="$1"
  local service="$2"
  local local_port="$3"
  local remote_port="$4"
  local log_file="$5"
  local label="port-forward ${namespace}/svc/${service}"

  require_port_available "${local_port}" "${label}"
  kubectl port-forward -n "${namespace}" "svc/${service}" "${local_port}:${remote_port}" >"${log_file}" 2>&1 &
  PIDS+=("$!")
  wait_managed_port "${local_port}" "$!" "${log_file}" "${label}"
}

port_forward_resource_bg() {
  local namespace="$1"
  local resource="$2"
  local local_port="$3"
  local remote_port="$4"
  local log_file="$5"
  local label="port-forward ${namespace}/${resource}"

  require_port_available "${local_port}" "${label}"
  kubectl port-forward -n "${namespace}" "${resource}" "${local_port}:${remote_port}" >"${log_file}" 2>&1 &
  PIDS+=("$!")
  wait_managed_port "${local_port}" "$!" "${log_file}" "${label}"
}

start_header_proxy_bg() {
  local local_port="$1"
  local upstream_origin="$2"
  local log_file="$3"
  local label="header proxy ${local_port} -> ${upstream_origin}"
  shift 3

  require_port_available "${local_port}" "${label}"
  python3 "${PROJECT_ROOT}/test/e2e/mcp_header_proxy.py" \
    --listen-host 127.0.0.1 \
    --listen-port "${local_port}" \
    --upstream-origin "${upstream_origin}" \
    "$@" >"${log_file}" 2>&1 &
  PIDS+=("$!")
  wait_managed_port "${local_port}" "$!" "${log_file}" "${label}"
}

build_headers_json() {
  # Usage: build_headers_json "Name=value" "Name2=value2" ...
  # Safely encodes header key=value pairs into a JSON object via Python so
  # that values containing quotes or backslashes never corrupt the JSON.
  python3 -c "
import json, sys
d = {}
for arg in sys.argv[1:]:
    k, _, v = arg.partition('=')
    d[k] = v
print(json.dumps(d))
" "$@"
}

generate_oauth_fixtures() {
  local out_dir="$1"
  local generator="${out_dir}/oauth-fixtures.go"

  mkdir -p "${out_dir}"
  cat >"${generator}" <<'EOF'
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func mustWrite(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		panic(err)
	}
}

func encodeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func signToken(privateKey *rsa.PrivateKey, claims map[string]any) string {
	header := map[string]any{
		"alg": "RS256",
		"kid": "e2e-test-key",
		"typ": "JWT",
	}
	signingInput := encodeJSON(header) + "." + encodeJSON(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func main() {
	outDir := os.Getenv("OAUTH_FIXTURE_DIR")
	issuerURL := os.Getenv("OAUTH_ISSUER_URL")
	audience := os.Getenv("OAUTH_AUDIENCE")
	humanID := os.Getenv("OAUTH_HUMAN_ID")
	agentID := os.Getenv("OAUTH_AGENT_ID")
	sessionID := os.Getenv("OAUTH_SESSION_ID")
	if outDir == "" || issuerURL == "" || audience == "" || humanID == "" || agentID == "" || sessionID == "" {
		panic("missing required OAuth fixture environment")
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	exponent := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()

	now := time.Now().UTC()
	validClaims := map[string]any{
		"iss": issuerURL,
		"sub": humanID,
		"azp": agentID,
		"sid": sessionID,
		"aud": []string{audience},
		"iat": now.Add(-1 * time.Minute).Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}
	invalidAudienceClaims := map[string]any{
		"iss": issuerURL,
		"sub": humanID,
		"azp": agentID,
		"sid": sessionID,
		"aud": []string{"wrong-audience"},
		"iat": now.Add(-1 * time.Minute).Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}

	jwks := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": "e2e-test-key",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(exponent),
			},
		},
	}
	metadata := map[string]any{
		"issuer":         issuerURL,
		"jwks_uri":       issuerURL + "/keys",
		"token_endpoint": issuerURL + "/token",
	}

	jwksJSON, err := json.MarshalIndent(jwks, "", "  ")
	if err != nil {
		panic(err)
	}
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		panic(err)
	}

	mustWrite(filepath.Join(outDir, "oauth-authorization-server"), append(metadataJSON, '\n'))
	mustWrite(filepath.Join(outDir, "keys"), append(jwksJSON, '\n'))
	mustWrite(filepath.Join(outDir, "valid-token.txt"), []byte(signToken(privateKey, validClaims)))
	mustWrite(filepath.Join(outDir, "invalid-token.txt"), []byte(signToken(privateKey, invalidAudienceClaims)))

	fmt.Println("generated oauth fixtures in", outDir)
}
EOF

  OAUTH_FIXTURE_DIR="${out_dir}" \
  OAUTH_ISSUER_URL="${OAUTH_ISSUER_URL}" \
  OAUTH_AUDIENCE="${OAUTH_AUDIENCE}" \
  OAUTH_HUMAN_ID="${OAUTH_HUMAN_ID}" \
  OAUTH_AGENT_ID="${OAUTH_AGENT_ID}" \
  OAUTH_SESSION_ID="${OAUTH_SESSION_ID}" \
  go run "${generator}"
}

run_mcp_curl_expect() {
  local name="$1"
  local url="$2"
  local expected_ok="$3"
  local expected_tool_error="${4:-}"
  local output_file="${WORKDIR}/${name}.json"
  local curl_exit_code=0

  if [[ "${expected_ok}" == "true" ]]; then
    log_line mcp "${name}: curl MCP flow should complete initialize/list/call/read successfully"
  else
    log_line mcp "${name}: curl MCP flow should be rejected with ${expected_tool_error}"
  fi

  if MCP_CURL_NAME="${name}" \
    MCP_CURL_URL="${url}" \
    MCP_CURL_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
    MCP_CURL_TIMEOUT="${MCP_CURL_TIMEOUT}" \
    MCP_CURL_OUTPUT="${output_file}" \
    python3 <<'PY'; then
import json
import os
import shutil
import subprocess
import sys
import tempfile


name = os.environ["MCP_CURL_NAME"]
url = os.environ["MCP_CURL_URL"]
protocol = os.environ["MCP_CURL_PROTOCOL_VERSION"]
output_file = os.environ["MCP_CURL_OUTPUT"]
curl_bin = os.environ.get("MCP_CURL_BIN", "curl")
timeout = os.environ.get("MCP_CURL_TIMEOUT", "20").strip()
if timeout.endswith("s"):
    timeout = timeout[:-1]
if not timeout:
    timeout = "20"

required_steps = [
    "tools/list",
    "prompts/list",
    "resources/list",
    "tools/call",
    "prompts/get",
    "resources/read",
]


def result_doc():
    return {"transport": "http", "ok": False, "steps": []}


doc = result_doc()


def write_doc():
    doc["ok"] = all(step.get("ok") for step in doc["steps"])
    with open(output_file, "w", encoding="utf-8") as fh:
        json.dump(doc, fh, indent=2)
        fh.write("\n")


def parse_session_id(headers):
    session_id = ""
    for line in headers.splitlines():
        key, sep, value = line.partition(":")
        if sep and key.lower() == "mcp-session-id":
            session_id = value.strip()
    return session_id


def json_candidates(body):
    candidates = []
    try:
        candidates.append(json.loads(body))
    except json.JSONDecodeError:
        pass

    for line in body.splitlines():
        if not line.startswith("data:"):
            continue
        data = line[len("data:"):].strip()
        if not data or data == "[DONE]":
            continue
        try:
            candidates.append(json.loads(data))
        except json.JSONDecodeError:
            continue
    return candidates


def rpc_error_text(body):
    for candidate in json_candidates(body):
        if not isinstance(candidate, dict):
            continue
        err = candidate.get("error")
        if not err:
            continue
        if isinstance(err, dict):
            pieces = [str(err.get("code", "")).strip(), str(err.get("message", "")).strip()]
            data = err.get("data")
            if data:
                pieces.append(json.dumps(data, sort_keys=True))
            return " ".join(piece for piece in pieces if piece)
        return str(err)
    return ""


def summarize_error(status, body, stderr):
    rpc_error = rpc_error_text(body)
    if rpc_error:
        return rpc_error
    snippet = " ".join(body.split())
    if len(snippet) > 500:
        snippet = snippet[:500] + "..."
    if snippet:
        return f"http_status_{status}: {snippet}"
    stderr = " ".join(stderr.split())
    if stderr:
        return f"http_status_{status}: {stderr}"
    return f"http_status_{status}"


def curl_post(step_name, payload, session_id="", allowed_statuses=(200,), require_session=False):
    with tempfile.TemporaryDirectory(prefix="mcp-curl-") as temp_dir:
        payload_file = os.path.join(temp_dir, "payload.json")
        headers_file = os.path.join(temp_dir, "headers.txt")
        body_file = os.path.join(temp_dir, "body.txt")
        with open(payload_file, "w", encoding="utf-8") as fh:
            json.dump(payload, fh)

        cmd = [
            curl_bin,
            "-sS",
            "--max-time",
            timeout,
            "-D",
            headers_file,
            "-o",
            body_file,
            "-w",
            "%{http_code}",
            "-X",
            "POST",
            "-H",
            "content-type: application/json",
            "-H",
            "accept: application/json, text/event-stream",
            "-H",
            f"Mcp-Protocol-Version: {protocol}",
        ]
        if session_id:
            cmd.extend(["-H", f"Mcp-Session-Id: {session_id}"])
        cmd.extend(["--data-binary", f"@{payload_file}", url])

        proc = subprocess.run(cmd, text=True, capture_output=True, check=False)
        status_text = proc.stdout.strip().splitlines()[-1] if proc.stdout.strip() else "0"
        try:
            status = int(status_text)
        except ValueError:
            status = 0

        try:
            with open(headers_file, "r", encoding="utf-8") as fh:
                headers = fh.read()
        except FileNotFoundError:
            headers = ""
        try:
            with open(body_file, "r", encoding="utf-8") as fh:
                body = fh.read()
        except FileNotFoundError:
            body = ""

    next_session_id = parse_session_id(headers) or session_id
    rpc_error = rpc_error_text(body)
    ok = proc.returncode == 0 and status in allowed_statuses and not rpc_error
    if require_session and not next_session_id:
        ok = False

    step = {"name": step_name, "ok": ok, "status": status, "body": body}
    if proc.returncode != 0:
        step["error"] = proc.stderr.strip() or f"curl exited {proc.returncode}"
    elif status not in allowed_statuses:
        step["error"] = summarize_error(status, body, proc.stderr)
    elif rpc_error:
        step["error"] = rpc_error
    elif require_session and not next_session_id:
        step["error"] = "missing Mcp-Session-Id response header"
    return step, next_session_id


def append_skipped(step_names, reason):
    for step_name in step_names:
        doc["steps"].append({"name": step_name, "ok": False, "skipped": True, "error": reason})


if not shutil.which(curl_bin):
    doc["steps"].append({"name": "initialize", "ok": False, "status": 0, "error": f"{curl_bin} not found"})
    append_skipped(required_steps, "curl unavailable")
    write_doc()
    sys.exit(1)

initialize_payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": protocol,
        "capabilities": {},
        "clientInfo": {"name": "mcp-runtime-e2e-curl", "version": "1.0.0"},
    },
}

step, session_id = curl_post("initialize", initialize_payload, require_session=True)
doc["steps"].append(step)
if not step["ok"]:
    append_skipped(required_steps, "initialize failed")
    write_doc()
    sys.exit(1)

step, session_id = curl_post(
    "notifications/initialized",
    {"jsonrpc": "2.0", "method": "notifications/initialized"},
    session_id=session_id,
    allowed_statuses=(200, 202, 204),
)
doc["steps"].append(step)
if not step["ok"]:
    append_skipped(required_steps, "notifications/initialized failed")
    write_doc()
    sys.exit(1)

requests = [
    ("tools/list", {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}),
    ("prompts/list", {"jsonrpc": "2.0", "id": 3, "method": "prompts/list", "params": {}}),
    ("resources/list", {"jsonrpc": "2.0", "id": 4, "method": "resources/list", "params": {}}),
    (
        "tools/call",
        {
            "jsonrpc": "2.0",
            "id": 5,
            "method": "tools/call",
            "params": {"name": "aaa-ping", "arguments": {}},
        },
    ),
    (
        "prompts/get",
        {
            "jsonrpc": "2.0",
            "id": 6,
            "method": "prompts/get",
            "params": {"name": "hello", "arguments": {}},
        },
    ),
    (
        "resources/read",
        {
            "jsonrpc": "2.0",
            "id": 7,
            "method": "resources/read",
            "params": {"uri": "embedded:readme"},
        },
    ),
]

remaining = list(required_steps)
for step_name, payload in requests:
    remaining.pop(0)
    step, session_id = curl_post(step_name, payload, session_id=session_id)
    doc["steps"].append(step)
    if not step["ok"]:
        append_skipped(remaining, f"{step_name} failed")
        write_doc()
        sys.exit(1)

write_doc()
sys.exit(0 if doc["ok"] else 1)
PY
    curl_exit_code=0
  else
    curl_exit_code=$?
  fi

  SMOKE_NAME="${name}" \
  SMOKE_OUTPUT="${output_file}" \
  EXPECTED_OK="${expected_ok}" \
  EXPECTED_TOOL_ERROR="${expected_tool_error}" \
  SMOKE_EXIT_CODE="${curl_exit_code}" \
  python3 <<'PY'
import json
import os

name = os.environ["SMOKE_NAME"]
expected_ok = os.environ["EXPECTED_OK"].lower() == "true"
expected_tool_error = os.environ.get("EXPECTED_TOOL_ERROR", "")
curl_exit_code = int(os.environ.get("SMOKE_EXIT_CODE", "0"))


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())

with open(os.environ["SMOKE_OUTPUT"], "r", encoding="utf-8") as fh:
    doc = json.load(fh)

check(
    doc.get("transport") == "http",
    f"{name}: transport=http",
    f"{name}: expected transport=http, got {doc.get('transport')!r}",
)

steps = {step["name"]: step for step in doc.get("steps", [])}
required_steps = [
    "initialize",
    "tools/list",
    "prompts/list",
    "resources/list",
    "tools/call",
    "prompts/get",
    "resources/read",
]
for step_name in required_steps:
    check(
        step_name in steps,
        f"{name}: step present {step_name}",
        f"{name}: missing step {step_name}",
    )

check(
    bool(doc.get("ok")) == expected_ok,
    f"{name}: ok={expected_ok}",
    f"{name}: expected ok={expected_ok}, got {doc.get('ok')}: {json.dumps(doc, indent=2)}",
)

if expected_ok:
    check(
        curl_exit_code == 0,
        f"{name}: exit code 0",
        f"{name}: expected exit code 0, got {curl_exit_code}",
    )
    for step_name in ("tools/call", "prompts/get", "resources/read"):
        step = steps[step_name]
        check(
            bool(step.get("ok")),
            f"{name}: {step_name} succeeded",
            f"{name}: expected {step_name} to succeed: {json.dumps(step, indent=2)}",
        )
else:
    check(
        curl_exit_code != 0,
        f"{name}: non-zero exit code for expected failure",
        f"{name}: expected non-zero exit code for failed curl MCP run",
    )
    failed_steps = {
        step_name: step
        for step_name, step in steps.items()
        if not step.get("ok") and not step.get("skipped")
    }
    check(
        bool(failed_steps),
        f"{name}: observed failed step(s)",
        f"{name}: expected at least one failed step: {json.dumps(doc, indent=2)}",
    )
    if expected_tool_error:
        matching_steps = {
            step_name: step
            for step_name, step in failed_steps.items()
            if expected_tool_error in step.get("error", "")
        }
        rendered = json.dumps(failed_steps, indent=2)
        check(
            bool(matching_steps),
            f"{name}: failed step contains {expected_tool_error!r}",
            f"{name}: expected a failed step error to contain {expected_tool_error!r}, got {rendered}",
        )
    for step_name in ("tools/call", "prompts/get", "resources/read"):
        step = steps[step_name]
        allowed = (
            step.get("ok")
            or step.get("skipped")
            or (expected_tool_error and expected_tool_error in step.get("error", ""))
        )
        check(
            allowed,
            f"{name}: {step_name} outcome allowed",
            f"{name}: expected {step_name} to succeed, skip, or fail with {expected_tool_error!r}: "
            f"{json.dumps(step, indent=2)}",
        )

rows = []
for step_name in required_steps:
    step = steps[step_name]
    if (
        not expected_ok
        and not step.get("ok")
        and not step.get("skipped")
        and (not expected_tool_error or expected_tool_error in step.get("error", ""))
    ):
        status = "expected_fail"
    else:
        status = "ok" if step.get("ok") else "skip" if step.get("skipped") else "fail"
    error = step.get("error", "")
    if error:
        status = f"{status} ({error})"
    rows.append((step_name, status))

width = max(len(step_name) for step_name, _ in rows)
print(f"{name}:")
exit_code = str(curl_exit_code)
if not expected_ok and curl_exit_code != 0:
    exit_code = f"{curl_exit_code} (expected non-zero)"
print(f"  exit code{' ' * (width - len('exit code'))}  {exit_code}")
for step_name, status in rows:
    print(f"  {step_name:{width}}  {status}")
PY
}

wait_for_gateway_rpc_methods() {
  local server_name="$1"
  local label="$2"

  API_BASE="http://127.0.0.1:${SENTINEL_PORT}/api" \
  API_KEY="${API_KEY}" \
  SERVER_NAME="${server_name}" \
  AUDIT_LABEL="${label}" \
  python3 <<'PY'
import json
import os
import time
import urllib.parse
import urllib.request


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


api_base = os.environ["API_BASE"]
api_key = os.environ["API_KEY"]
server_name = os.environ["SERVER_NAME"]
label = os.environ["AUDIT_LABEL"]
expected = {
    "initialize",
    "notifications/initialized",
    "tools/list",
    "prompts/list",
    "resources/list",
    "prompts/get",
    "resources/read",
    "tools/call",
}
headers = {"x-api-key": api_key}
url = f"{api_base}/events/filter?{urllib.parse.urlencode({'server': server_name, 'limit': '1000'})}"
last = {}
last_error = None

for _ in range(60):
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=10) as resp:
            last = json.loads(resp.read().decode())
        methods = {
            payload.get("rpc_method")
            for payload in (
                event.get("payload", {})
                for event in last.get("events", [])
                if isinstance(event.get("payload"), dict)
            )
            if payload.get("rpc_method")
        }
        if methods >= expected:
            ok(f"{label} audit events include full MCP curl method set")
            break
    except Exception as exc:
        last_error = exc
    time.sleep(2)
else:
    if last:
        fail(f"timed out waiting for {label} audit methods: {json.dumps(last, indent=2)}")
    if last_error is not None:
        raise last_error
    fail(f"timed out waiting for {label} audit methods")
PY
}

# Real-provider agent prompts are intentionally disabled for now. The e2e suite
# generates deterministic MCP traffic with curl so CI does not consume provider
# tokens while validating gateway policy, auth, audit, and observability paths.

wait_for_policy_text() {
  local text="$1"
  local tries="${2:-40}"
  local i
  for i in $(seq 1 "${tries}"); do
    local current
    current="$(kubectl get configmap "${SERVER_NAME}-gateway-policy" -n mcp-servers -o "jsonpath={.data.policy\.json}" 2>/dev/null || true)"
    if [[ "${current}" == *"${text}"* ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for policy text: ${text}" >&2
  return 1
}

wait_for_mcp_initialize_result() {
  local base_url="$1"
  local expected_status="$2"
  local expected_body_text="${3:-}"
  local expected_header_name="${4:-}"
  local expected_header_text="${5:-}"
  local tries="${6:-${MCP_POLICY_WAIT_TRIES}}"
  local i
  local last_result_file="${WORKDIR}/last-mcp-initialize-result.json"
  local last_stderr_file="${WORKDIR}/last-mcp-initialize-stderr.txt"

  for i in $(seq 1 "${tries}"); do
    if MCP_BASE="${base_url}" \
      MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
      MCP_EXPECT_STATUS="${expected_status}" \
      MCP_EXPECT_BODY_TEXT="${expected_body_text}" \
      MCP_EXPECT_HEADER_NAME="${expected_header_name}" \
      MCP_EXPECT_HEADER_TEXT="${expected_header_text}" \
      MCP_RESULT_FILE="${last_result_file}" \
      python3 <<'PY' >/dev/null 2>"${last_stderr_file}"
import json
import http.client
import os
import urllib.error
import urllib.parse
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]
expected_status = int(os.environ["MCP_EXPECT_STATUS"])
expected_body_text = os.environ.get("MCP_EXPECT_BODY_TEXT", "")
expected_header_name = os.environ.get("MCP_EXPECT_HEADER_NAME", "")
expected_header_text = os.environ.get("MCP_EXPECT_HEADER_TEXT", "")
result_file = os.environ["MCP_RESULT_FILE"]
initialize_payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": protocol,
        "capabilities": {},
        "clientInfo": {"name": "mcp-runtime-e2e", "version": "1.0.0"},
    },
}

headers = {
    "content-type": "application/json",
    "accept": "application/json, text/event-stream",
    "Mcp-Protocol-Version": protocol,
}
req = urllib.request.Request(
    base,
    data=json.dumps(initialize_payload).encode(),
    headers=headers,
)
try:
    resp = urllib.request.urlopen(req, timeout=10)
    status = resp.status
    response_headers = dict(resp.headers.items())
    body = resp.read().decode()
except urllib.error.HTTPError as exc:
    status = exc.code
    response_headers = dict(exc.headers.items())
    body = exc.read().decode()

with open(result_file, "w", encoding="utf-8") as fh:
    json.dump({"status": status, "headers": response_headers, "body": body}, fh)

if status != expected_status:
    raise SystemExit(1)
if expected_body_text and expected_body_text not in body:
    raise SystemExit(1)
if expected_header_name:
    header_value = response_headers.get(expected_header_name) or response_headers.get(expected_header_name.title())
    if not header_value:
        raise SystemExit(1)
    if expected_header_text and expected_header_text not in header_value:
        raise SystemExit(1)
PY
    then
      echo "[mcp] observed initialize returning ${expected_status}"
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for initialize to return ${expected_status}" >&2
  if [[ -s "${last_stderr_file}" ]]; then
    echo "[debug] last initialize python stderr:" >&2
    cat "${last_stderr_file}" >&2 || true
  fi
  if [[ -f "${last_result_file}" ]]; then
    echo "[debug] last initialize response while waiting:" >&2
    cat "${last_result_file}" >&2 || true
  fi
  return 1
}

wait_for_http_result() {
  local url="$1"
  local method="$2"
  local headers_json="$3"
  local body_mode="$4"
  local body_text="$5"
  local expected_status="$6"
  local expected_body_text="${7:-}"
  local expected_header_name="${8:-}"
  local expected_header_text="${9:-}"
  local tries="${10:-${RAW_REQUEST_TRIES}}"
  local i
  local last_result_file="${WORKDIR}/last-http-result.json"
  local last_stderr_file="${WORKDIR}/last-http-stderr.txt"

  for i in $(seq 1 "${tries}"); do
    if MCP_URL="${url}" \
      MCP_METHOD="${method}" \
      MCP_HEADERS_JSON="${headers_json}" \
      MCP_BODY_MODE="${body_mode}" \
      MCP_BODY_TEXT="${body_text}" \
      MCP_EXPECT_STATUS="${expected_status}" \
      MCP_EXPECT_BODY_TEXT="${expected_body_text}" \
      MCP_EXPECT_HEADER_NAME="${expected_header_name}" \
      MCP_EXPECT_HEADER_TEXT="${expected_header_text}" \
      MCP_RESULT_FILE="${last_result_file}" \
      python3 <<'PY' >/dev/null 2>"${last_stderr_file}"
import json
import http.client
import os
import urllib.error
import urllib.parse
import urllib.request

url = os.environ["MCP_URL"]
method = os.environ["MCP_METHOD"]
headers = json.loads(os.environ["MCP_HEADERS_JSON"])
body_mode = os.environ["MCP_BODY_MODE"]
body_text = os.environ["MCP_BODY_TEXT"]
expected_status = int(os.environ["MCP_EXPECT_STATUS"])
expected_body_text = os.environ.get("MCP_EXPECT_BODY_TEXT", "")
expected_header_name = os.environ.get("MCP_EXPECT_HEADER_NAME", "")
expected_header_text = os.environ.get("MCP_EXPECT_HEADER_TEXT", "")
result_file = os.environ["MCP_RESULT_FILE"]

if body_mode == "none":
    data = None
elif body_mode == "text":
    data = body_text.encode()
elif body_mode == "chunked-text":
    parsed = urllib.parse.urlsplit(url)
    scheme = parsed.scheme or "http"
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port or (443 if scheme == "https" else 80)
    path = parsed.path or "/"
    if parsed.query:
        path += "?" + parsed.query
    chunk_body = body_text.encode()
    chunk_size = max(1, len(chunk_body) // 2) if chunk_body else 1
    chunks = [chunk_body[i:i + chunk_size] for i in range(0, len(chunk_body), chunk_size)]
    if not chunks:
        chunks = [b""]
    connection_class = http.client.HTTPSConnection if scheme == "https" else http.client.HTTPConnection
    conn = connection_class(host, port, timeout=10)
    req_headers = dict(headers)
    req_headers["Transfer-Encoding"] = "chunked"
    conn.request(method, path, body=chunks, headers=req_headers, encode_chunked=True)
    resp = conn.getresponse()
    status = resp.status
    response_headers = dict(resp.getheaders())
    body = resp.read().decode()
    conn.close()
    with open(result_file, "w", encoding="utf-8") as fh:
        json.dump({"status": status, "headers": response_headers, "body": body}, fh)
    if status != expected_status:
        raise SystemExit(1)
    if expected_body_text and expected_body_text not in body:
        raise SystemExit(1)
    if expected_header_name:
        header_value = response_headers.get(expected_header_name) or response_headers.get(expected_header_name.title())
        if not header_value:
            raise SystemExit(1)
        if expected_header_text and expected_header_text not in header_value:
            raise SystemExit(1)
    raise SystemExit(0)
else:
    raise SystemExit(f"unknown body_mode: {body_mode!r}")

req = urllib.request.Request(url, data=data, headers=headers, method=method)
try:
    resp = urllib.request.urlopen(req, timeout=10)
    status = resp.status
    response_headers = dict(resp.headers.items())
    body = resp.read().decode()
except urllib.error.HTTPError as exc:
    status = exc.code
    response_headers = dict(exc.headers.items())
    body = exc.read().decode()

with open(result_file, "w", encoding="utf-8") as fh:
    json.dump({"status": status, "headers": response_headers, "body": body}, fh)

if status != expected_status:
    raise SystemExit(1)
if expected_body_text and expected_body_text not in body:
    raise SystemExit(1)
if expected_header_name:
    header_value = response_headers.get(expected_header_name) or response_headers.get(expected_header_name.title())
    if not header_value:
        raise SystemExit(1)
    if expected_header_text and expected_header_text not in header_value:
        raise SystemExit(1)
PY
    then
      echo "[mcp] observed ${method} ${url} returning ${expected_status}"
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${method} ${url} to return ${expected_status}" >&2
  if [[ -s "${last_stderr_file}" ]]; then
    echo "[debug] last http python stderr:" >&2
    cat "${last_stderr_file}" >&2 || true
  fi
  if [[ -f "${last_result_file}" ]]; then
    echo "[debug] last HTTP response while waiting:" >&2
    cat "${last_result_file}" >&2 || true
  fi
  return 1
}

wait_for_mcp_tool_result() {
  local base_url="$1"
  local tool_name="$2"
  local tool_args_json="$3"
  local expected_status="$4"
  local expected_body_text="${5:-}"
  local tries="${6:-${MCP_POLICY_WAIT_TRIES}}"
  local host_header="${7:-}"
  local i
  local last_result_file="${WORKDIR}/last-mcp-tool-result.json"
  local last_stderr_file="${WORKDIR}/last-mcp-tool-stderr.txt"

  for i in $(seq 1 "${tries}"); do
    if MCP_BASE="${base_url}" \
      MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
      MCP_TOOL_NAME="${tool_name}" \
      MCP_TOOL_ARGS="${tool_args_json}" \
      MCP_EXPECT_STATUS="${expected_status}" \
      MCP_EXPECT_BODY_TEXT="${expected_body_text}" \
      MCP_RESULT_FILE="${last_result_file}" \
      MCP_HOST_HEADER="${host_header}" \
      python3 <<'PY' >/dev/null 2>"${last_stderr_file}"
import http.client
import json
import os
import urllib.parse

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]
initialize_payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": protocol,
        "capabilities": {},
        "clientInfo": {"name": "mcp-runtime-e2e", "version": "1.0.0"},
    },
}


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())
tool_name = os.environ["MCP_TOOL_NAME"]
tool_args = json.loads(os.environ["MCP_TOOL_ARGS"])
expected_status = int(os.environ["MCP_EXPECT_STATUS"])
expected_body_text = os.environ.get("MCP_EXPECT_BODY_TEXT", "")
result_file = os.environ["MCP_RESULT_FILE"]
host_header = os.environ.get("MCP_HOST_HEADER", "")


def write_result(phase, status, body):
    with open(result_file, "w", encoding="utf-8") as fh:
        json.dump({"phase": phase, "status": status, "body": body}, fh)


def post(msg, mcp_session_id=None):
    parsed = urllib.parse.urlsplit(base)
    target = parsed.path or "/"
    if parsed.query:
        target += "?" + parsed.query
    headers = {
        "content-type": "application/json",
        "accept": "application/json, text/event-stream",
        "Mcp-Protocol-Version": protocol,
    }
    host_value = host_header or parsed.netloc
    if mcp_session_id:
        headers["Mcp-Session-Id"] = mcp_session_id
    body = json.dumps(msg).encode()
    headers["Content-Length"] = str(len(body))
    conn_class = http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
    conn = conn_class(parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80), timeout=10)
    try:
        conn.putrequest("POST", target, skip_host=True)
        conn.putheader("Host", host_value)
        for key, value in headers.items():
            conn.putheader(key, value)
        conn.endheaders(body)
        resp = conn.getresponse()
        return resp.status, resp.getheader("Mcp-Session-Id") or mcp_session_id, resp.read().decode()
    finally:
        conn.close()


status, mcp_session_id, body = post(initialize_payload)
if status != 200 or not mcp_session_id:
    write_result("initialize", status, body)
    raise SystemExit(1)

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
if status not in (200, 202):
    write_result("notifications/initialized", status, body)
    raise SystemExit(1)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool_name, "arguments": tool_args}},
    mcp_session_id=mcp_session_id,
)
write_result("tools/call", status, body)
if status != expected_status:
    raise SystemExit(1)
if expected_body_text and expected_body_text not in body:
    raise SystemExit(1)
PY
    then
      echo "[mcp] observed ${tool_name} returning ${expected_status}"
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${tool_name} to return ${expected_status}" >&2
  if [[ -s "${last_stderr_file}" ]]; then
    echo "[debug] last tool python stderr:" >&2
    cat "${last_stderr_file}" >&2 || true
  fi
  if [[ -f "${last_result_file}" ]]; then
    echo "[debug] last ${tool_name} response while waiting:" >&2
    cat "${last_result_file}" >&2 || true
  fi
  print_gateway_policy_debug >&2 || true
  return 1
}

wait_for_named_server_ready() {
  local server_name="$1"
  local namespace="${2:-mcp-servers}"
  local tries="${3:-60}"
  local i
  for i in $(seq 1 "${tries}"); do
    local deployment_ready
    local phase
    local service_ready
    local ingress_ready
    deployment_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.deploymentReady}' 2>/dev/null || true)"
    service_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.serviceReady}' 2>/dev/null || true)"
    ingress_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.ingressReady}' 2>/dev/null || true)"
    phase="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    if [[ "${deployment_ready}" == "true" && "${service_ready}" == "true" && "${ingress_ready}" == "true" && "${phase}" == "Ready" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for MCPServer ${server_name} to report core readiness and phase Ready" >&2
  kubectl get mcpserver "${server_name}" -n "${namespace}" -o yaml || true
  return 1
}

print_gateway_policy_debug() {
  local policy_json
  policy_json="$(kubectl get configmap "${SERVER_NAME}-gateway-policy" -n mcp-servers -o "jsonpath={.data.policy\.json}" 2>/dev/null || true)"
  if [[ -z "${policy_json}" ]]; then
    echo "[debug] gateway policy ConfigMap is unavailable"
    return 0
  fi

  POLICY_JSON="${policy_json}" \
  DEBUG_GRANT_NAME="${SERVER_NAME}-grant" \
  DEBUG_SESSION_NAME="${SESSION_ID}" \
  python3 <<'PY'
import json
import os
import sys

try:
    doc = json.loads(os.environ["POLICY_JSON"])
except json.JSONDecodeError as exc:
    print(f"[debug] failed to decode gateway policy JSON: {exc}", file=sys.stderr)
    raise SystemExit(0)

grant_name = os.environ["DEBUG_GRANT_NAME"]
session_name = os.environ["DEBUG_SESSION_NAME"]

summary = {
    "policy": doc.get("policy", {}),
    "session": doc.get("session", {}),
    "grants": [grant for grant in doc.get("grants", []) if grant.get("name") == grant_name],
    "sessions": [session for session in doc.get("sessions", []) if session.get("name") == session_name],
    "tools": doc.get("tools", []),
}

print("[debug] gateway policy snapshot:", file=sys.stderr)
print(json.dumps(summary, indent=2, sort_keys=True), file=sys.stderr)
PY
}

wait_for_server_ready() {
  wait_for_named_server_ready "${SERVER_NAME}" "mcp-servers" "${1:-60}"
}

wait_for_deployment_exists() {
  local namespace="$1"
  local name="$2"
  local tries="${3:-60}"
  local i
  for i in $(seq 1 "${tries}"); do
    if kubectl get deployment "${name}" -n "${namespace}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for deployment ${name} in namespace ${namespace}" >&2
  kubectl get deployment -n "${namespace}" || true
  return 1
}

restart_deployment_pods() {
  local namespace="$1"
  local name="$2"
  local timeout="${3:-180s}"

  kubectl rollout restart "deploy/${name}" -n "${namespace}" >/dev/null
  kubectl rollout status "deploy/${name}" -n "${namespace}" --timeout="${timeout}"
}

prepare_example_metadata() {
  local metadata_dir="$1"
  local server_name="$2"
  local ingress_host="$3"
  local route_path="$4"
  local image_repo="$5"
  local image_tag="$6"

  SERVER_NAME_OVERRIDE="${server_name}" \
  SERVER_HOST_OVERRIDE="${ingress_host}" \
  SERVER_ROUTE_OVERRIDE="${route_path}" \
  SERVER_IMAGE_OVERRIDE="${image_repo}" \
  SERVER_IMAGE_TAG_OVERRIDE="${image_tag}" \
  METADATA_DIR_OVERRIDE="${metadata_dir}" \
  python3 <<'PY'
from pathlib import Path
import os

metadata_dir = Path(os.environ["METADATA_DIR_OVERRIDE"])
path = metadata_dir / "servers.yaml"
lines = path.read_text(encoding="utf-8").splitlines()
updated = []
server_name_updated = False
server_image_updated = False
server_image_tag_updated = False
mcp_path_updated = False
public_path_prefix_updated = False
in_env_vars = False
current_env_name = None
resources_present = any(line.startswith("    resources:") for line in lines)
image_tag_present = any(line.lstrip().startswith("imageTag: ") for line in lines)

route_override = os.environ["SERVER_ROUTE_OVERRIDE"].strip()
route_prefix = route_override.strip("/")
if route_prefix.endswith("/mcp"):
    route_prefix = route_prefix[: -len("/mcp")].rstrip("/")
if not route_prefix:
    route_prefix = os.environ["SERVER_NAME_OVERRIDE"]

for line in lines:
    stripped = line.lstrip()
    indent = line[: len(line) - len(stripped)]
    if not server_name_updated and indent == "  " and stripped.startswith("- name: "):
        updated.append(f"{indent}- name: {os.environ['SERVER_NAME_OVERRIDE']}")
        server_name_updated = True
    elif stripped.startswith("ingressHost: "):
        updated.append(f"{indent}ingressHost: {os.environ['SERVER_HOST_OVERRIDE']}")
    elif stripped.startswith("route: "):
        updated.append(f"{indent}route: {os.environ['SERVER_ROUTE_OVERRIDE']}")
    elif stripped.startswith("publicPathPrefix: "):
        updated.append(f"{indent}publicPathPrefix: {route_prefix}")
        public_path_prefix_updated = True
    elif not server_image_updated and indent == "    " and stripped.startswith("image: "):
        updated.append(f"{indent}image: {os.environ['SERVER_IMAGE_OVERRIDE']}")
        server_image_updated = True
        if not image_tag_present:
            updated.append(f"{indent}imageTag: {os.environ['SERVER_IMAGE_TAG_OVERRIDE']}")
            server_image_tag_updated = True
    elif indent == "    " and stripped.startswith("imageTag: "):
        updated.append(f"{indent}imageTag: {os.environ['SERVER_IMAGE_TAG_OVERRIDE']}")
        server_image_tag_updated = True
    elif indent == "    " and stripped == "envVars:":
        in_env_vars = True
        current_env_name = None
        updated.append(line)
    elif in_env_vars and indent == "      " and stripped.startswith("- name: "):
        current_env_name = stripped.split(": ", 1)[1]
        updated.append(line)
    elif in_env_vars and current_env_name == "MCP_PATH" and indent == "        " and stripped.startswith("value: "):
        updated.append(f'{indent}value: "{os.environ["SERVER_ROUTE_OVERRIDE"]}"')
        mcp_path_updated = True
    else:
        if in_env_vars and indent.startswith("    ") and indent != "      " and indent != "        ":
            in_env_vars = False
            current_env_name = None
        updated.append(line)
if not server_image_updated:
    final = []
    inserted = False
    for line in updated:
        final.append(line)
        stripped = line.lstrip()
        indent = line[: len(line) - len(stripped)]
        if not inserted and indent == "  " and stripped.startswith("- name: "):
            final.append(f"{indent}  image: {os.environ['SERVER_IMAGE_OVERRIDE']}")
            final.append(f"{indent}  imageTag: {os.environ['SERVER_IMAGE_TAG_OVERRIDE']}")
            inserted = True
    updated = final
    server_image_updated = inserted
    server_image_tag_updated = inserted
if not mcp_path_updated:
    final = []
    inserted = False
    for line in updated:
        final.append(line)
        stripped = line.lstrip()
        indent = line[: len(line) - len(stripped)]
        if not inserted and indent == "    " and stripped.startswith("namespace: "):
            final.append(f"{indent}envVars:")
            final.append(f"{indent}  - name: MCP_PATH")
            final.append(f'{indent}    value: "{os.environ["SERVER_ROUTE_OVERRIDE"]}"')
            inserted = True
    updated = final
    mcp_path_updated = inserted
if not public_path_prefix_updated:
    final = []
    inserted = False
    for line in updated:
        final.append(line)
        stripped = line.lstrip()
        indent = line[: len(line) - len(stripped)]
        if not inserted and indent == "    " and stripped.startswith("route: "):
            final.append(f"{indent}publicPathPrefix: {route_prefix}")
            inserted = True
    updated = final
    public_path_prefix_updated = inserted
if not resources_present:
    final = []
    inserted = False
    for line in updated:
        final.append(line)
        stripped = line.lstrip()
        indent = line[: len(line) - len(stripped)]
        if not inserted and indent == "    " and stripped.startswith("namespace: "):
            final.append(f"{indent}resources:")
            final.append(f"{indent}  requests:")
            final.append(f"{indent}    cpu: 1m")
            final.append(f"{indent}    memory: 32Mi")
            inserted = True
    updated = final
    resources_present = inserted
path.write_text("\n".join(updated) + "\n", encoding="utf-8")

# Verify substitutions landed; missing fields cause silent failures later.
if not server_name_updated:
    raise SystemExit(f"prepare_example_metadata: no '- name:' entry found to replace in {path}")
if not server_image_updated:
    raise SystemExit(f"prepare_example_metadata: image field was not updated in {path}")
if not server_image_tag_updated:
    raise SystemExit(f"prepare_example_metadata: imageTag field was not updated in {path}")
if not mcp_path_updated:
    raise SystemExit(f"prepare_example_metadata: MCP_PATH env var was not updated in {path}")
if not public_path_prefix_updated:
    raise SystemExit(f"prepare_example_metadata: publicPathPrefix was not updated in {path}")
if not resources_present:
    raise SystemExit(f"prepare_example_metadata: resources were not inserted in {path}")
PY
}

deploy_example_server_via_pipeline() {
  local server_name="$1"
  local ingress_host="$2"
  local route_path="$3"
  local example_source_dir="$4"
  local example_workspace_dir="$5"
  local image_repo
  local image_ref

  rm -rf "${example_workspace_dir}"
  mkdir -p "$(dirname "${example_workspace_dir}")"
  cp -R "${example_source_dir}" "${example_workspace_dir}"

  image_repo="registry.registry.svc.cluster.local:5000/${server_name}"
  image_ref="${image_repo}:${E2E_WORKLOAD_TAG}"
  prepare_example_metadata "${example_workspace_dir}/.mcp" "${server_name}" "${ingress_host}" "${route_path}" "${image_repo}" "${E2E_WORKLOAD_TAG}"

  if cache_mode_enabled && docker image inspect "${image_ref}" >/dev/null 2>&1; then
    echo "[cache] skipping example image build for ${image_ref}"
  else
    echo "[deploy] building example image ${image_ref}"
    (
      cd "${example_workspace_dir}"
      "${PROJECT_ROOT}/bin/mcp-runtime" server build image "${server_name}" \
        --metadata-dir .mcp \
        --dockerfile Dockerfile \
        --registry "registry.registry.svc.cluster.local:5000" \
        --tag "${E2E_WORKLOAD_TAG}" \
        --context .
    )
  fi
  load_image_into_kind "${image_ref}"

  (
    cd "${example_workspace_dir}"
    "${PROJECT_ROOT}/bin/mcp-runtime" pipeline generate --dir .mcp --output manifests
    "${PROJECT_ROOT}/bin/mcp-runtime" pipeline deploy --dir manifests
  )

  echo "[deploy] waiting for ${server_name} rollout"
  wait_for_deployment_exists mcp-servers "${server_name}"
  if ! kubectl rollout status "deploy/${server_name}" -n mcp-servers --timeout=180s; then
    echo "[debug] ${server_name} rollout failed; collecting diagnostics" >&2
    kubectl get mcpserver "${server_name}" -n mcp-servers -o yaml || true
    kubectl get deploy,rs,pods,svc,ingress,configmap -n mcp-servers || true
    kubectl describe deployment "${server_name}" -n mcp-servers || true
    kubectl describe pods -n mcp-servers -l "app=${server_name}" || true
    kubectl logs -n mcp-servers -l "app=${server_name}" --all-containers=true --tail=200 || true
    kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --all-containers=true --tail=200 || true
    exit 1
  fi
  wait_for_named_server_ready "${server_name}" "mcp-servers" 60
}

wait_for_grant_tool_rule() {
  local grant_name="$1"
  local tool_name="$2"
  local expected_decision="$3"
  local tries="${4:-40}"
  local i
  for i in $(seq 1 "${tries}"); do
    local policy_json
    policy_json="$(kubectl get configmap "${SERVER_NAME}-gateway-policy" -n mcp-servers -o "jsonpath={.data.policy\.json}" 2>/dev/null || true)"
    if POLICY_JSON="${policy_json}" GRANT_NAME="${grant_name}" TOOL_NAME="${tool_name}" EXPECTED_DECISION="${expected_decision}" python3 <<'PY'
import json
import os
import sys

policy = os.environ.get("POLICY_JSON", "")
if not policy:
    raise SystemExit(1)

try:
    doc = json.loads(policy)
except json.JSONDecodeError:
    raise SystemExit(1)

grant_name = os.environ["GRANT_NAME"]
tool_name = os.environ["TOOL_NAME"]
expected = os.environ["EXPECTED_DECISION"]

for grant in doc.get("grants", []):
    if grant.get("name") != grant_name:
        continue
    for rule in grant.get("tool_rules", []):
        if rule.get("name") == tool_name and rule.get("decision") == expected:
            raise SystemExit(0)

raise SystemExit(1)
PY
    then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for tool rule ${tool_name}=${expected_decision} in grant ${grant_name}" >&2
  kubectl get configmap "${SERVER_NAME}-gateway-policy" -n mcp-servers -o yaml || true
  return 1
}

mirror_repository_path() {
  local image="$1"
  local path="${image#docker.io/}"

  if [[ "${path}" == "${image}" && "${path}" != */* ]]; then
    path="library/${path}"
  fi

  echo "${path}"
}

local_registry_target() {
  local image="$1"
  echo "${LOCAL_REGISTRY_PUSH_HOST}/$(mirror_repository_path "${image}")"
}

cache_mode_enabled() {
  [[ "${E2E_CACHE_MODE}" == "1" ]]
}

kind_cluster_exists() {
  kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}

load_image_into_kind() {
  local image="$1"
  echo "[kind] loading ${image} into kind cluster ${CLUSTER_NAME}"
  kind load docker-image --name "${CLUSTER_NAME}" "${image}"
}

registry_target_exists() {
  local target="$1"
  local ref="${target#${LOCAL_REGISTRY_PUSH_HOST}/}"
  local repo="${ref%:*}"
  local tag="${ref##*:}"

  if [[ "${repo}" == "${ref}" ]]; then
    repo="${ref}"
    tag="latest"
  fi

  curl -fsS \
    -H "Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json" \
    "http://${LOCAL_REGISTRY_PUSH_HOST}/v2/${repo}/manifests/${tag}" >/dev/null 2>&1
}

pull_cached_image() {
  local image="$1"
  local target
  target="$(local_registry_target "${image}")"

  if ! cache_mode_enabled; then
    return 1
  fi
  ensure_local_registry_running
  if ! registry_target_exists "${target}"; then
    return 1
  fi

  echo "[cache] reusing ${image} from local registry ${target}"
  docker pull "${target}" >/dev/null
  docker tag "${target}" "${image}"
}

run_with_retry() {
  local description="$1"
  shift

  local attempt
  local exit_code=0
  for attempt in $(seq 1 "${LOCAL_REGISTRY_RETRY_TRIES}"); do
    if "$@"; then
      return 0
    fi
    exit_code=$?
    if [[ "${attempt}" -lt "${LOCAL_REGISTRY_RETRY_TRIES}" ]]; then
      echo "[retry] ${description} failed (attempt ${attempt}/${LOCAL_REGISTRY_RETRY_TRIES}, exit ${exit_code}); retrying in ${LOCAL_REGISTRY_RETRY_DELAY}s" >&2
      sleep "${LOCAL_REGISTRY_RETRY_DELAY}"
    fi
  done

  echo "[retry] ${description} failed after ${LOCAL_REGISTRY_RETRY_TRIES} attempts" >&2
  return "${exit_code}"
}

safe_log_label() {
  local label="$1"
  printf '%s' "${label}" | tr -c '[:alnum:]_.-' '_'
}

relative_log_path() {
  local path="$1"
  printf '%s' "${path#"${WORKDIR}/"}"
}

format_duration() {
  local seconds="$1"
  local minutes
  if [[ "${seconds}" -lt 60 ]]; then
    printf '%ss' "${seconds}"
    return
  fi
  minutes=$((seconds / 60))
  seconds=$((seconds % 60))
  printf '%sm%02ss' "${minutes}" "${seconds}"
}

log_status() {
  local state="$1"
  local message="$2"
  local color="${E2E_COLOR_INFO}"
  case "${state}" in
    DONE)
      color="${E2E_COLOR_SUCCESS}"
      ;;
    FAILED)
      color="${E2E_COLOR_WARN}"
      ;;
    RUNNING|PLAN)
      color="${E2E_COLOR_POLICY}"
      ;;
    STDOUT|STDERR)
      color="${E2E_COLOR_DEBUG}"
      ;;
  esac
  printf '%b%-7s%b %s\n' "${color}" "${state}" "${E2E_COLOR_RESET}" "${message}"
}

log_status_err() {
  local state="$1"
  local message="$2"
  log_status "${state}" "${message}" >&2
}

combine_stream_logs() {
  local stdout_file="$1"
  local stderr_file="$2"
  local log_file="$3"
  local log_dir="${log_file%/*}"

  mkdir -p "${log_dir}"
  : >"${log_file}"
  if [[ -s "${stdout_file}" ]]; then
    {
      echo "===== stdout ====="
      cat "${stdout_file}"
    } >>"${log_file}"
  fi
  if [[ -s "${stderr_file}" ]]; then
    {
      echo "===== stderr ====="
      cat "${stderr_file}"
    } >>"${log_file}"
  fi
  return 0
}

preview_log_stream() {
  local state="$1"
  local label="$2"
  local log_file="$3"
  local lines="$4"
  local stream="${5:-stdout}"

  if [[ "${lines}" -eq 0 || ! -s "${log_file}" ]]; then
    return 0
  fi

  if [[ "${stream}" == "stderr" ]]; then
    log_status_err "${state}" "${label}: last ${lines} stderr lines from $(relative_log_path "${log_file}")"
    tail -n "${lines}" "${log_file}" | sed 's/^/        /' >&2
  else
    log_status "${state}" "${label}: last ${lines} stdout lines from $(relative_log_path "${log_file}")"
    tail -n "${lines}" "${log_file}" | sed 's/^/        /'
  fi
}

run_logged_stage() {
  local label="$1"
  local log_file
  local heartbeat_pid=""
  local safe_label
  local started_at
  local stdout_file
  local stderr_file
  local status=0
  shift

  mkdir -p "${STAGE_LOG_DIR}"
  STAGE_SEQ=$((STAGE_SEQ + 1))
  safe_label="$(safe_log_label "${label}")"
  log_file="${STAGE_LOG_DIR}/stage-$(printf '%03d' "${STAGE_SEQ}")-${safe_label}.log"
  stdout_file="${log_file%.log}.stdout.log"
  stderr_file="${log_file%.log}.stderr.log"
  started_at="$(date +%s)"
  log_status "START" "${label} (full log: $(relative_log_path "${log_file}"))"

  (
    while true; do
      sleep 30
      log_status "RUNNING" "${label} for $(format_duration "$(( $(date +%s) - started_at ))")"
    done
  ) &
  heartbeat_pid="$!"
  PIDS+=("${heartbeat_pid}")

  if "$@" >"${stdout_file}" 2>"${stderr_file}"; then
    kill "${heartbeat_pid}" >/dev/null 2>&1 || true
    wait "${heartbeat_pid}" 2>/dev/null || true
    combine_stream_logs "${stdout_file}" "${stderr_file}" "${log_file}"
    log_status "DONE" "${label} in $(format_duration "$(( $(date +%s) - started_at ))") (full log: $(relative_log_path "${log_file}"))"
    preview_log_stream "STDOUT" "${label}" "${stdout_file}" "${E2E_LOG_PREVIEW_LINES}" stdout
    preview_log_stream "STDERR" "${label}" "${stderr_file}" "${E2E_LOG_PREVIEW_LINES}" stderr
  else
    status=$?
    kill "${heartbeat_pid}" >/dev/null 2>&1 || true
    wait "${heartbeat_pid}" 2>/dev/null || true
    combine_stream_logs "${stdout_file}" "${stderr_file}" "${log_file}"
    log_status_err "FAILED" "${label} after $(format_duration "$(( $(date +%s) - started_at ))") with exit ${status} (full log: $(relative_log_path "${log_file}"))"
    if [[ -s "${stdout_file}" || -s "${stderr_file}" ]]; then
      preview_log_stream "STDOUT" "${label}" "${stdout_file}" "${E2E_LOG_FAILURE_LINES}" stdout
      preview_log_stream "STDERR" "${label}" "${stderr_file}" "${E2E_LOG_FAILURE_LINES}" stderr
    else
      log_status_err "FAILED" "${label}: command produced no captured stdout or stderr"
    fi
    return "${status}"
  fi
}

parallel_reset() {
  PARALLEL_PIDS=()
  PARALLEL_LABELS=()
  PARALLEL_LOGS=()
  PARALLEL_STDOUT_LOGS=()
  PARALLEL_STDERR_LOGS=()
  PARALLEL_STARTED_AT=()
  PARALLEL_FAILED=0
}

parallel_wait_next() {
  local pid="${PARALLEL_PIDS[0]}"
  local label="${PARALLEL_LABELS[0]}"
  local log_file="${PARALLEL_LOGS[0]}"
  local stdout_file="${PARALLEL_STDOUT_LOGS[0]}"
  local stderr_file="${PARALLEL_STDERR_LOGS[0]}"
  local started_at="${PARALLEL_STARTED_AT[0]}"
  local heartbeat_pid=""
  local status=0

  (
    while true; do
      sleep 30
      log_status "RUNNING" "${label} for $(format_duration "$(( $(date +%s) - started_at ))")"
    done
  ) &
  heartbeat_pid="$!"
  PIDS+=("${heartbeat_pid}")

  if wait "${pid}"; then
    kill "${heartbeat_pid}" >/dev/null 2>&1 || true
    wait "${heartbeat_pid}" 2>/dev/null || true
    combine_stream_logs "${stdout_file}" "${stderr_file}" "${log_file}"
    log_status "DONE" "${label} in $(format_duration "$(( $(date +%s) - started_at ))") (full log: $(relative_log_path "${log_file}"))"
    preview_log_stream "STDOUT" "${label}" "${stdout_file}" "${E2E_LOG_PREVIEW_LINES}" stdout
    preview_log_stream "STDERR" "${label}" "${stderr_file}" "${E2E_LOG_PREVIEW_LINES}" stderr
  else
    status=$?
    kill "${heartbeat_pid}" >/dev/null 2>&1 || true
    wait "${heartbeat_pid}" 2>/dev/null || true
    combine_stream_logs "${stdout_file}" "${stderr_file}" "${log_file}"
    log_status_err "FAILED" "${label} after $(format_duration "$(( $(date +%s) - started_at ))") with exit ${status} (full log: $(relative_log_path "${log_file}"))"
    if [[ -s "${stdout_file}" || -s "${stderr_file}" ]]; then
      preview_log_stream "STDOUT" "${label}" "${stdout_file}" "${E2E_LOG_FAILURE_LINES}" stdout
      preview_log_stream "STDERR" "${label}" "${stderr_file}" "${E2E_LOG_FAILURE_LINES}" stderr
    else
      log_status_err "FAILED" "${label}: command produced no captured stdout or stderr"
    fi
    PARALLEL_FAILED=1
  fi

  PARALLEL_PIDS=("${PARALLEL_PIDS[@]:1}")
  PARALLEL_LABELS=("${PARALLEL_LABELS[@]:1}")
  PARALLEL_LOGS=("${PARALLEL_LOGS[@]:1}")
  PARALLEL_STDOUT_LOGS=("${PARALLEL_STDOUT_LOGS[@]:1}")
  PARALLEL_STDERR_LOGS=("${PARALLEL_STDERR_LOGS[@]:1}")
  PARALLEL_STARTED_AT=("${PARALLEL_STARTED_AT[@]:1}")
}

parallel_start() {
  local max_parallel="$1"
  local label="$2"
  local log_file
  local safe_label
  local stdout_file
  local stderr_file
  shift 2

  while [[ ${#PARALLEL_PIDS[@]} -ge ${max_parallel} ]]; do
    parallel_wait_next
    if [[ "${PARALLEL_FAILED}" -ne 0 ]]; then
      return 1
    fi
  done

  mkdir -p "${STAGE_LOG_DIR}"
  PARALLEL_SEQ=$((PARALLEL_SEQ + 1))
  safe_label="$(safe_log_label "${label}")"
  log_file="${STAGE_LOG_DIR}/parallel-$(printf '%03d' "${PARALLEL_SEQ}")-${safe_label}.log"
  stdout_file="${log_file%.log}.stdout.log"
  stderr_file="${log_file%.log}.stderr.log"
  log_status "START" "${label} (parallel worker; full log: $(relative_log_path "${log_file}"))"
  "$@" >"${stdout_file}" 2>"${stderr_file}" &
  local pid="$!"
  PARALLEL_PIDS+=("${pid}")
  PARALLEL_LABELS+=("${label}")
  PARALLEL_LOGS+=("${log_file}")
  PARALLEL_STDOUT_LOGS+=("${stdout_file}")
  PARALLEL_STDERR_LOGS+=("${stderr_file}")
  PARALLEL_STARTED_AT+=("$(date +%s)")
  PIDS+=("${pid}")
}

parallel_wait_all() {
  while [[ ${#PARALLEL_PIDS[@]} -gt 0 ]]; do
    parallel_wait_next
  done

  if [[ "${PARALLEL_FAILED}" -ne 0 ]]; then
    return 1
  fi
}

publish_image_to_local_registry() {
  local image="$1"
  local target
  target="$(local_registry_target "${image}")"

  ensure_local_registry_running
  echo "[registry] publishing ${image} to ${target}"
  docker tag "${image}" "${target}"
  run_with_retry "docker push ${target}" docker push "${target}"
}

build_and_publish_image() {
  local image="$1"
  local dockerfile="$2"
  local context_dir="$3"

  if pull_cached_image "${image}"; then
    return 0
  fi

  echo "[image] building ${image}"
  docker build -t "${image}" -f "${dockerfile}" "${context_dir}"
  publish_image_to_local_registry "${image}"
}

image_build_label() {
  local image="$1"
  case "${image}" in
    docker.io/library/mcp-runtime-operator:*)
      echo "build operator image (${image})"
      ;;
    docker.io/library/mcp-runtime-registry:*)
      echo "build test registry image (${image})"
      ;;
    docker.io/library/mcp-sentinel-mcp-proxy:*)
      echo "build Sentinel MCP proxy image (${image})"
      ;;
    docker.io/library/mcp-sentinel-ingest:*)
      echo "build Sentinel ingest image (${image})"
      ;;
    docker.io/library/mcp-sentinel-api:*)
      echo "build Sentinel API image (${image})"
      ;;
    docker.io/library/mcp-sentinel-processor:*)
      echo "build Sentinel processor image (${image})"
      ;;
    docker.io/library/mcp-sentinel-ui:*)
      echo "build Sentinel UI image (${image})"
      ;;
    *)
      echo "build image ${image}"
      ;;
  esac
}

build_and_publish_images_parallel() {
  local start_failed=0

  log_status "PLAN" "Building runtime and Sentinel images with ${E2E_IMAGE_BUILD_PARALLELISM} parallel workers"
  parallel_reset
  while [[ $# -gt 0 ]]; do
    local image="$1"
    local dockerfile="$2"
    local context_dir="$3"
    shift 3
    if ! parallel_start "${E2E_IMAGE_BUILD_PARALLELISM}" "$(image_build_label "${image}")" build_and_publish_image "${image}" "${dockerfile}" "${context_dir}"; then
      start_failed=1
      break
    fi
  done
  if ! parallel_wait_all; then
    return 1
  fi
  if [[ "${start_failed}" -ne 0 ]]; then
    return 1
  fi
}

mirror_upstream_image() {
  local image="$1"
  local target

  echo "[image] mirroring ${image} into ${LOCAL_REGISTRY_NAME}"
  if pull_cached_image "${image}"; then
    return 0
  fi

  target="$(local_registry_target "${image}")"
  ensure_local_registry_running
  if docker pull "${target}" >/dev/null 2>&1; then
    echo "[image] found ${image} in local mirror ${target}"
    docker tag "${target}" "${image}"
  else
    echo "[image] ${image} not present in local mirror; falling back to upstream"
    run_with_retry "docker pull ${image}" docker pull "${image}"
  fi
  publish_image_to_local_registry "${image}"
}

mirror_upstream_images_parallel() {
  local start_failed=0

  log_status "PLAN" "Mirroring upstream images into the local registry with ${E2E_IMAGE_MIRROR_PARALLELISM} parallel workers"
  parallel_reset
  local image
  for image in "$@"; do
    if ! parallel_start "${E2E_IMAGE_MIRROR_PARALLELISM}" "mirror ${image}" mirror_upstream_image "${image}"; then
      start_failed=1
      break
    fi
  done
  if ! parallel_wait_all; then
    return 1
  fi
  if [[ "${start_failed}" -ne 0 ]]; then
    return 1
  fi
}

wait_core_platform_rollouts() {
  echo "[verify] waiting for core platform components"
  kubectl get namespace mcp-servers mcp-servers-org mcp-servers-public >/dev/null

  # Setup already waits for these workloads. Keep the post-setup sanity check
  # sequential so transient kubectl watch cancellation does not fail a healthy run.
  run_logged_stage "verify registry rollout" rollout_status_with_logs registry deploy registry 180s
  run_logged_stage "verify operator rollout" rollout_status_with_logs mcp-runtime deploy mcp-runtime-operator-controller-manager 180s
  run_logged_stage "verify traefik rollout" rollout_status_with_logs traefik deploy traefik 180s
  run_logged_stage "verify sentinel api rollout" rollout_status_with_logs mcp-sentinel deploy mcp-sentinel-api 180s
  run_logged_stage "verify sentinel gateway rollout" rollout_status_with_logs mcp-sentinel deploy mcp-sentinel-gateway 180s
  run_logged_stage "verify tempo rollout" rollout_status_with_logs mcp-sentinel statefulset tempo 180s
  run_logged_stage "verify loki rollout" rollout_status_with_logs mcp-sentinel statefulset loki 300s
}

delete_mcp_server_and_wait() {
  local server_name="$1"
  local namespace="$2"
  local timeout="${3:-120s}"

  ./bin/mcp-runtime server --use-kube delete "${server_name}" --namespace "${namespace}"
  kubectl wait --for=delete "mcpserver/${server_name}" -n "${namespace}" --timeout="${timeout}" || true
}

cleanup_mcp_server_and_wait() {
  local server_name="$1"
  local namespace="$2"
  local timeout="${3:-120s}"

  if delete_mcp_server_and_wait "${server_name}" "${namespace}" "${timeout}"; then
    return 0
  fi

  log_line warn "server delete ${server_name} failed; falling back to kubectl cleanup"
  kubectl delete "mcpserver/${server_name}" -n "${namespace}" --ignore-not-found --wait=false
  kubectl wait --for=delete "mcpserver/${server_name}" -n "${namespace}" --timeout="${timeout}" || true
}

deploy_primary_server_manifests() {
  ./bin/mcp-runtime pipeline generate --file "${METADATA_FILE}" --output "${MANIFEST_DIR}"
  ./bin/mcp-runtime pipeline deploy --dir "${MANIFEST_DIR}"
}

deploy_oauth_server_manifests() {
  ./bin/mcp-runtime pipeline generate --file "${OAUTH_METADATA_FILE}" --output "${OAUTH_MANIFEST_DIR}"
  ./bin/mcp-runtime pipeline deploy --dir "${OAUTH_MANIFEST_DIR}"
}

start_local_registry() {
  if docker ps -a --format '{{.Names}}' | grep -qx "${LOCAL_REGISTRY_NAME}"; then
    if cache_mode_enabled; then
      if docker ps --format '{{.Names}}' | grep -qx "${LOCAL_REGISTRY_NAME}"; then
        echo "[registry] reusing local docker hub mirror ${LOCAL_REGISTRY_NAME} on localhost:${LOCAL_REGISTRY_PORT}"
      else
        echo "[registry] starting existing local docker hub mirror ${LOCAL_REGISTRY_NAME} on localhost:${LOCAL_REGISTRY_PORT}"
        docker start "${LOCAL_REGISTRY_NAME}" >/dev/null
      fi
      wait_http "http://127.0.0.1:${LOCAL_REGISTRY_PORT}/v2/" "" 30
      return
    fi
    docker rm -f "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1 || true
  fi

  echo "[registry] starting local docker hub mirror ${LOCAL_REGISTRY_NAME} on localhost:${LOCAL_REGISTRY_PORT}"
  docker run -d \
    -p "127.0.0.1:${LOCAL_REGISTRY_PORT}:5000" \
    --name "${LOCAL_REGISTRY_NAME}" \
    registry:2.8.3 >/dev/null
  wait_http "http://127.0.0.1:${LOCAL_REGISTRY_PORT}/v2/" "" 30
}

connect_local_registry_to_kind_network() {
  docker network connect kind "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1 || true
}

ensure_local_registry_running() {
  if ! docker ps --format '{{.Names}}' | grep -qx "${LOCAL_REGISTRY_NAME}"; then
    echo "[registry] local mirror ${LOCAL_REGISTRY_NAME} is not running; restarting"
    start_local_registry
    if docker network inspect kind >/dev/null 2>&1; then
      connect_local_registry_to_kind_network
    fi
  fi
}

platform_cache_ready() {
  if ! cache_mode_enabled; then
    return 1
  fi
  kubectl get namespace registry mcp-runtime mcp-sentinel mcp-servers mcp-servers-org mcp-servers-public >/dev/null 2>&1 || return 1
  kubectl rollout status deploy/registry -n registry --timeout=5s >/dev/null 2>&1 || return 1
  kubectl rollout status deploy/mcp-runtime-operator-controller-manager -n mcp-runtime --timeout=5s >/dev/null 2>&1 || return 1
  kubectl rollout status deploy/traefik -n traefik --timeout=5s >/dev/null 2>&1 || return 1
  kubectl rollout status deploy/mcp-sentinel-api -n mcp-sentinel --timeout=5s >/dev/null 2>&1 || return 1
  kubectl rollout status deploy/mcp-sentinel-gateway -n mcp-sentinel --timeout=5s >/dev/null 2>&1 || return 1
}

cat > "${KIND_CONFIG}" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["http://${LOCAL_REGISTRY_MIRROR_ENDPOINT}", "https://registry-1.docker.io"]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry-1.docker.io"]
    endpoint = ["http://${LOCAL_REGISTRY_MIRROR_ENDPOINT}", "https://registry-1.docker.io"]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
    endpoint = ["http://127.0.0.1:32000"]
EOF

run_logged_stage "start local registry" start_local_registry

if cache_mode_enabled && kind_cluster_exists; then
  echo "[kind] reusing cluster ${CLUSTER_NAME}"
else
  echo "[kind] creating cluster ${CLUSTER_NAME}"
  run_logged_stage "kind create cluster" kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
fi
connect_local_registry_to_kind_network
KUBECONFIG_FILE="/tmp/kubeconfig-kind"
kind get kubeconfig --name "${CLUSTER_NAME}" > "${KUBECONFIG_FILE}"
export KUBECONFIG="${KUBECONFIG_FILE}"
kubectl config use-context "kind-${CLUSTER_NAME}"
mkdir -p "${HOME}/.kube"
cp "${KUBECONFIG_FILE}" "${HOME}/.kube/config"

echo "[build] rebuilding CLI"
run_logged_stage "build CLI" env GOCACHE="${PROJECT_ROOT}/.gocache" go build -o bin/mcp-runtime ./cmd/mcp-runtime

echo "[cli] checking static command output"
./bin/mcp-runtime --version >/dev/null
./bin/mcp-runtime help >/dev/null
./bin/mcp-runtime completion bash >/dev/null

PLATFORM_CACHE_READY=0
if platform_cache_ready; then
  PLATFORM_CACHE_READY=1
  echo "[cache] reusing ready platform in cluster ${CLUSTER_NAME}"
else
  mirror_upstream_images_parallel \
    "registry:2.8.3" \
    "traefik:v2.10" \
    "traefik:v3.0" \
    "clickhouse/clickhouse-server:23.8" \
    "confluentinc/cp-zookeeper:7.5.1" \
    "confluentinc/cp-kafka:7.5.1" \
    "prom/prometheus:v2.49.1" \
    "otel/opentelemetry-collector:0.92.0" \
    "grafana/tempo:2.3.1" \
    "grafana/loki:2.9.4" \
    "grafana/promtail:2.9.4" \
    "grafana/grafana:10.2.3" \
    "nginx:1.27-alpine"
  build_and_publish_images_parallel \
    "docker.io/library/mcp-runtime-operator:latest" "Dockerfile.operator" "." \
    "${TEST_MODE_REGISTRY_IMAGE}" "test/e2e/registry.Dockerfile" "." \
    "docker.io/library/mcp-sentinel-mcp-proxy:latest" "${SENTINEL_ROOT}/services/mcp-proxy/Dockerfile" "${SENTINEL_ROOT}" \
    "docker.io/library/mcp-sentinel-ingest:latest" "${SENTINEL_ROOT}/services/ingest/Dockerfile" "${SENTINEL_ROOT}" \
    "docker.io/library/mcp-sentinel-api:latest" "${SENTINEL_ROOT}/services/api/Dockerfile" "${SENTINEL_ROOT}" \
    "docker.io/library/mcp-sentinel-processor:latest" "${SENTINEL_ROOT}/services/processor/Dockerfile" "${SENTINEL_ROOT}" \
    "docker.io/library/mcp-sentinel-ui:latest" "${SENTINEL_ROOT}/services/ui/Dockerfile" "${SENTINEL_ROOT}"
fi

export MCP_SETUP_WAIT_TIMEOUT="${MCP_SETUP_WAIT_TIMEOUT:-900}"
export MCP_DEPLOYMENT_TIMEOUT="${MCP_DEPLOYMENT_TIMEOUT:-900s}"
export MCP_REGISTRY_ENDPOINT="${MCP_REGISTRY_ENDPOINT:-registry.registry.svc.cluster.local:5000}"
export MCP_INGRESS_READINESS_MODE="${MCP_INGRESS_READINESS_MODE:-permissive}"
export MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT="${MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT:-http://otel-collector.mcp-sentinel.svc.cluster.local:4318}"
if [[ "${PLATFORM_CACHE_READY}" == "1" ]]; then
  echo "[setup] skipping platform setup because E2E_CACHE_MODE=1 found a ready platform"
else
  echo "[setup] running platform setup in test mode (platform mode: ${E2E_PLATFORM_MODE})"
  run_logged_stage "setup test mode" \
    env MCP_RUNTIME_REGISTRY_IMAGE_OVERRIDE="${TEST_MODE_REGISTRY_IMAGE}" \
    ./bin/mcp-runtime setup --test-mode --parallel-builds --platform-mode "${E2E_PLATFORM_MODE}" --ingress-manifest config/ingress/overlays/http
fi

wait_core_platform_rollouts

echo "[cli] checking platform status commands"
./bin/mcp-runtime status
./bin/mcp-runtime cluster status
./bin/mcp-runtime registry status
./bin/mcp-runtime registry info

echo "[cli] checking auth, bootstrap, cluster, registry, and sentinel commands"
MCP_RUNTIME_CONFIG_DIR="${WORKDIR}/auth-config" ./bin/mcp-runtime auth status
MCP_RUNTIME_CONFIG_DIR="${WORKDIR}/auth-config" ./bin/mcp-runtime auth login \
  --api-url "http://127.0.0.1:${SENTINEL_PORT}" \
  --token e2e-token \
  --skip-verify \
  --registry-host "${LOCAL_REGISTRY_PUSH_HOST}"
MCP_RUNTIME_CONFIG_DIR="${WORKDIR}/auth-config" ./bin/mcp-runtime auth status
MCP_RUNTIME_CONFIG_DIR="${WORKDIR}/auth-config" ./bin/mcp-runtime auth logout

./bin/mcp-runtime bootstrap --provider generic
./bin/mcp-runtime cluster init
./bin/mcp-runtime cluster config --ingress none
./bin/mcp-runtime cluster provision \
  --provider kind \
  --name "${CLUSTER_NAME}-dry-run" \
  --nodes 1 \
  --dry-run >"${WORKDIR}/cluster-provision-dry-run.txt"
if cache_mode_enabled; then
  echo "[cache] removing previous policy/OAuth test grants and sessions"
  kubectl delete mcpaccessgrant -n mcp-servers \
    "${SERVER_NAME}-grant" \
    "${SERVER_NAME}-e2e-api-grant" \
    "${OAUTH_SERVER_NAME}-grant" \
    "alice-${MT_TENANT_A}" \
    "bob-${MT_TENANT_B}" \
    --ignore-not-found --wait=true >/dev/null
  kubectl delete mcpagentsession -n mcp-servers \
    "${SESSION_ID}" \
    "${SERVER_NAME}-e2e-api-session" \
    "${MT_SESSION_A}" \
    "${MT_SESSION_B}" \
    --ignore-not-found --wait=true >/dev/null
  echo "[cache] removing previous policy/OAuth test workloads before cluster doctor"
  kubectl delete mcpserver -n mcp-servers "${SERVER_NAME}" "${OAUTH_SERVER_NAME}" "${MT_TENANT_A}" "${MT_TENANT_B}" --ignore-not-found --wait=true >/dev/null
  kubectl delete deployment -n mcp-servers "${SERVER_NAME}" "${OAUTH_SERVER_NAME}" "${MT_TENANT_A}" "${MT_TENANT_B}" --ignore-not-found --wait=true >/dev/null
  kubectl delete pod -n mcp-servers -l "app=${SERVER_NAME}" --ignore-not-found --wait=false >/dev/null
  kubectl delete pod -n mcp-servers -l "app=${OAUTH_SERVER_NAME}" --ignore-not-found --wait=false >/dev/null
  kubectl delete pod -n mcp-servers -l "app=${MT_TENANT_A}" --ignore-not-found --wait=false >/dev/null
  kubectl delete pod -n mcp-servers -l "app=${MT_TENANT_B}" --ignore-not-found --wait=false >/dev/null
  echo "[cache] deleting stale pending mcp-servers pods before cluster doctor"
  kubectl delete pod -n mcp-servers --field-selector=status.phase=Pending --ignore-not-found --wait=false >/dev/null
  echo "[cache] refreshing operator e2e environment before cluster doctor"
  kubectl set env deploy/mcp-runtime-operator-controller-manager -n mcp-runtime \
    "MCP_INGRESS_READINESS_MODE=${MCP_INGRESS_READINESS_MODE}" \
    "MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT=${MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT}" >/dev/null
  echo "[cache] restarting operator before cluster doctor to avoid stale retained reconcile logs"
  kubectl rollout restart deploy/mcp-runtime-operator-controller-manager -n mcp-runtime >/dev/null
  rollout_status_with_logs mcp-runtime deploy mcp-runtime-operator-controller-manager 180s
fi
if cache_mode_enabled; then
  run_logged_stage "cluster doctor" run_with_retry "cluster doctor" ./bin/mcp-runtime cluster doctor
else
  run_logged_stage "cluster doctor" ./bin/mcp-runtime cluster doctor
fi
run_cli_allowing_cert_prereq_failure cluster-cert-status ./bin/mcp-runtime cluster cert status
run_cli_allowing_cert_prereq_failure cluster-cert-apply-dry-run ./bin/mcp-runtime cluster cert apply --dry-run
run_cli_allowing_cert_prereq_failure cluster-cert-wait ./bin/mcp-runtime cluster cert wait --timeout 1s
./bin/mcp-runtime registry provision \
  --url "${LOCAL_REGISTRY_PUSH_HOST}" \
  --username e2e \
  --password e2e \
  --dry-run >"${WORKDIR}/registry-provision-dry-run.txt"
./bin/mcp-runtime sentinel status
./bin/mcp-runtime sentinel events >"${WORKDIR}/sentinel-events.txt"
./bin/mcp-runtime sentinel logs api --tail 20 >"${WORKDIR}/sentinel-api-logs.txt"
require_port_available "${CLI_SENTINEL_API_PORT}" "sentinel CLI port-forward"
./bin/mcp-runtime sentinel port-forward api \
  --port "${CLI_SENTINEL_API_PORT}" \
  --address 127.0.0.1 >"${WORKDIR}/sentinel-cli-port-forward.log" 2>&1 &
_cli_pf_pid="$!"
PIDS+=("${_cli_pf_pid}")
wait_managed_port "${CLI_SENTINEL_API_PORT}" "${_cli_pf_pid}" "${WORKDIR}/sentinel-cli-port-forward.log" "sentinel CLI port-forward" 30
kill "${_cli_pf_pid}" >/dev/null 2>&1 || true
wait "${_cli_pf_pid}" 2>/dev/null || true

API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.UI_API_KEY}' | decode_base64)"
if [[ -z "${API_KEY}" ]]; then
  API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.API_KEYS}' | decode_base64 | cut -d',' -f1)"
fi
if [[ -z "${API_KEY}" ]]; then
  echo "[error] failed to resolve mcp-sentinel UI/API key from secret" >&2
  exit 1
fi
INGEST_API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.INGEST_API_KEYS}' | decode_base64 | cut -d',' -f1)"
if [[ -z "${INGEST_API_KEY}" ]]; then
  INGEST_API_KEY="${API_KEY}"
fi
if [[ -z "${INGEST_API_KEY}" ]]; then
  echo "[error] failed to resolve mcp-sentinel ingest API key from secret" >&2
  exit 1
fi
GRAFANA_ADMIN_USER=""
GRAFANA_ADMIN_PASSWORD=""
if scenario_selected "observability"; then
  GRAFANA_ADMIN_USER="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.GRAFANA_ADMIN_USER}' | decode_base64)"
  GRAFANA_ADMIN_PASSWORD="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.GRAFANA_ADMIN_PASSWORD}' | decode_base64)"
  if [[ -z "${GRAFANA_ADMIN_USER}" || -z "${GRAFANA_ADMIN_PASSWORD}" ]]; then
    echo "[error] failed to resolve Grafana admin credentials from mcp-sentinel-secrets" >&2
    exit 1
  fi
fi

METADATA_FILE="${WORKDIR}/metadata.yaml"
MANIFEST_DIR="${WORKDIR}/manifests"
SERVER_IMAGE="registry.registry.svc.cluster.local:5000/${SERVER_NAME}:${E2E_WORKLOAD_TAG}"
SERVER_SECRET_NAME="${SERVER_NAME}-analytics-creds"
PYTHON_EXAMPLE_SOURCE_DIR="${PROJECT_ROOT}/examples/python-mcp-server"
PYTHON_EXAMPLE_WORKDIR="${WORKDIR}/python-mcp-server"
RUST_EXAMPLE_SOURCE_DIR="${PROJECT_ROOT}/examples/rust-mcp-server"
RUST_EXAMPLE_WORKDIR="${WORKDIR}/rust-mcp-server"
GO_EXAMPLE_SOURCE_DIR="${PROJECT_ROOT}/examples/go-mcp-server"
GO_EXAMPLE_WORKDIR="${WORKDIR}/go-mcp-server"

echo "[deploy] creating server-local analytics credentials secret"
kubectl create secret generic "${SERVER_SECRET_NAME}" \
  -n mcp-servers \
  --from-literal=api-key="${INGEST_API_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

cat > "${METADATA_FILE}" <<EOF
version: v1
servers:
  - name: ${SERVER_NAME}
    image: ${SERVER_IMAGE%:*}
    imageTag: ${SERVER_IMAGE##*:}
    route: /${SERVER_NAME}/mcp
    publicPathPrefix: ${SERVER_NAME}
    port: 8090
    namespace: mcp-servers
    resources:
      requests:
        cpu: 1m
        memory: 32Mi
    envVars:
      - name: PORT
        value: "8090"
      - name: MCP_PATH
        value: "/${SERVER_NAME}/mcp"
    tools:
      - name: aaa-ping
        requiredTrust: low
        sideEffect: read
      - name: echo
        requiredTrust: low
        sideEffect: read
      - name: add
        requiredTrust: low
        sideEffect: read
      - name: upper
        requiredTrust: medium
        sideEffect: read
      - name: slugify
        requiredTrust: low
        sideEffect: read
    auth:
      mode: header
      humanIDHeader: X-MCP-Human-ID
      agentIDHeader: X-MCP-Agent-ID
      sessionIDHeader: X-MCP-Agent-Session
    policy:
      mode: allow-list
      defaultDecision: deny
      policyVersion: v1
    session:
      required: true
    gateway:
      enabled: true
      resources:
        requests:
          cpu: 1m
          memory: 32Mi
    analytics:
      enabled: true
      ingestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      apiKeySecretRef:
        name: ${SERVER_SECRET_NAME}
        key: api-key
EOF

if cache_mode_enabled && docker image inspect "${SERVER_IMAGE}" >/dev/null 2>&1; then
  echo "[cache] skipping MCP server image build for ${SERVER_IMAGE}"
else
  echo "[cli] building MCP server image via CLI"
  run_logged_stage "build primary MCP server image" ./bin/mcp-runtime server build image "${SERVER_NAME}" \
    --metadata-file "${METADATA_FILE}" \
    --dockerfile "${GO_EXAMPLE_SOURCE_DIR}/Dockerfile" \
    --registry registry.registry.svc.cluster.local:5000 \
    --tag "${E2E_WORKLOAD_TAG}" \
    --context "${GO_EXAMPLE_SOURCE_DIR}"
fi
load_image_into_kind "${SERVER_IMAGE}"

echo "[cli] generating and deploying MCPServer manifests"
run_logged_stage "deploy primary MCP server manifests" deploy_primary_server_manifests

echo "[deploy] waiting for MCP server rollout"
wait_for_deployment_exists mcp-servers "${SERVER_NAME}"
if ! restart_deployment_pods mcp-servers "${SERVER_NAME}" 180s; then
  echo "[debug] MCP server rollout failed; collecting diagnostics" >&2
  kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o yaml || true
  kubectl get deploy,rs,pods,svc,ingress,configmap -n mcp-servers || true
  kubectl describe deployment "${SERVER_NAME}" -n mcp-servers || true
  kubectl describe pods -n mcp-servers || true
  kubectl logs -n mcp-servers -l "app=${SERVER_NAME}" --all-containers=true --tail=200 || true
  kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --all-containers=true --tail=200 || true
  exit 1
fi
wait_for_server_ready

echo "[cli] checking server mutation helpers"
SERVER_EXPORT_FILE="${WORKDIR}/${SERVER_NAME}-export.yaml"
./bin/mcp-runtime server --use-kube apply --file "${MANIFEST_DIR}/${SERVER_NAME}.yaml"
./bin/mcp-runtime server --use-kube export "${SERVER_NAME}" \
  --namespace mcp-servers \
  --file "${SERVER_EXPORT_FILE}"
assert_file_contains "name: ${SERVER_NAME}" "${SERVER_EXPORT_FILE}"
./bin/mcp-runtime server --use-kube patch "${SERVER_NAME}" \
  --namespace mcp-servers \
  --type merge \
  --patch '{"metadata":{"annotations":{"e2e.mcpruntime.org/cli-patch":"true"}}}'
./bin/mcp-runtime server --use-kube status --namespace mcp-servers >"${WORKDIR}/server-status.txt"
assert_file_contains "${SERVER_NAME}" "${WORKDIR}/server-status.txt"
./bin/mcp-runtime server --use-kube logs "${SERVER_NAME}" \
  --namespace mcp-servers \
  --tail 20 >"${WORKDIR}/server-logs.txt"
TEMP_CLI_SERVER="${SERVER_NAME}-cli-create"
./bin/mcp-runtime server --use-kube create "${TEMP_CLI_SERVER}" \
  --namespace mcp-servers \
  --image docker.io/library/nginx \
  --tag 1.27-alpine
./bin/mcp-runtime server --use-kube delete "${TEMP_CLI_SERVER}" --namespace mcp-servers
kubectl wait --for=delete "mcpserver/${TEMP_CLI_SERVER}" -n mcp-servers --timeout=120s || true

echo "[deploy] deploying official SDK example MCP servers"
parallel_reset
parallel_start 3 "deploy ${PYTHON_EXAMPLE_SERVER_NAME}" deploy_example_server_via_pipeline \
  "${PYTHON_EXAMPLE_SERVER_NAME}" \
  "${PYTHON_EXAMPLE_SERVER_HOST}" \
  "${PYTHON_EXAMPLE_SERVER_ROUTE}" \
  "${PYTHON_EXAMPLE_SOURCE_DIR}" \
  "${PYTHON_EXAMPLE_WORKDIR}"
parallel_start 3 "deploy ${RUST_EXAMPLE_SERVER_NAME}" deploy_example_server_via_pipeline \
  "${RUST_EXAMPLE_SERVER_NAME}" \
  "${RUST_EXAMPLE_SERVER_HOST}" \
  "${RUST_EXAMPLE_SERVER_ROUTE}" \
  "${RUST_EXAMPLE_SOURCE_DIR}" \
  "${RUST_EXAMPLE_WORKDIR}"
parallel_start 3 "deploy ${GO_EXAMPLE_SERVER_NAME}" deploy_example_server_via_pipeline \
  "${GO_EXAMPLE_SERVER_NAME}" \
  "${GO_EXAMPLE_SERVER_HOST}" \
  "${GO_EXAMPLE_SERVER_ROUTE}" \
  "${GO_EXAMPLE_SOURCE_DIR}" \
  "${GO_EXAMPLE_WORKDIR}"
parallel_wait_all

echo "[cli] checking server commands"

# --- server list: assert the primary server appears ---
_cli_list_out="$(./bin/mcp-runtime server --use-kube list --namespace mcp-servers 2>&1)"
if ! printf '%s\n' "${_cli_list_out}" | grep -qF "${SERVER_NAME}"; then
  echo "[cli][fail] 'server list' output does not contain ${SERVER_NAME}" >&2
  printf '%s\n' "${_cli_list_out}" >&2
  exit 1
fi
echo "[cli][pass] server list contains ${SERVER_NAME}"

# --- server get: capture YAML and assert readiness fields ---
_cli_get_out="$(./bin/mcp-runtime server --use-kube get "${SERVER_NAME}" --namespace mcp-servers 2>&1)"
_cli_get_file="${WORKDIR}/${SERVER_NAME}-get.yaml"
printf '%s\n' "${_cli_get_out}" >"${_cli_get_file}"

PY_SERVER_NAME="${SERVER_NAME}" \
PY_SERVER_HOST="${SERVER_HOST}" \
PY_WORKDIR="${WORKDIR}" \
PY_TRAEFIK_PORT="${TRAEFIK_PORT}" \
PY_MCP_CURL_ANON_PORT="${MCP_CURL_ANON_PORT}" \
PY_MCP_CURL_SESSION_PORT="${MCP_CURL_SESSION_PORT}" \
E2E_HELPERS="${PROJECT_ROOT}/test/e2e/e2e_helpers.py" \
python3 <<'PYEOF'
import os
import re

helpers_path = os.environ.get("E2E_HELPERS", "")
if helpers_path:
    exec(open(helpers_path).read())

server_name = os.environ["PY_SERVER_NAME"]
server_host = os.environ["PY_SERVER_HOST"]
workdir     = os.environ["PY_WORKDIR"]

get_yaml = open(f"{workdir}/{server_name}-get.yaml").read()

# Assert readiness flags are true in status
check("deploymentReady: true" in get_yaml,
      "deploymentReady: true",
      f"server get: deploymentReady is not true\n{get_yaml}")
check("serviceReady: true" in get_yaml,
      "serviceReady: true",
      f"server get: serviceReady is not true\n{get_yaml}")
check('type: CanaryReady' in get_yaml and 'status: "False"' in get_yaml,
      'CanaryReady condition is false',
      f"server get: CanaryReady condition is not false for a server without canary rollout\n{get_yaml}")

# Assert spec fields reflect what was deployed
expected_path = f"/{server_name}/mcp"
check(f"ingressPath: {expected_path}" in get_yaml,
      f"ingressPath: {expected_path}",
      f"server get: ingressPath not '{expected_path}'\n{get_yaml}")
check(f"publicPathPrefix: {server_name}" in get_yaml,
      f"publicPathPrefix: {server_name}",
      f"server get: publicPathPrefix not '{server_name}'\n{get_yaml}")

# Extract ingressPath and ingressHost to build MCP client config URL
m_path = re.search(r'ingressPath:\s*(\S+)', get_yaml)
ingress_path = m_path.group(1) if m_path else expected_path

traefik_port = os.environ.get("PY_TRAEFIK_PORT", "18080")
anon_proxy_port = os.environ.get("PY_MCP_CURL_ANON_PORT", "18084")
session_proxy_port = os.environ.get("PY_MCP_CURL_SESSION_PORT", "18086")

# Path-based local e2e usage should prefer local header proxies that already inject
# MCP protocol and identity headers where needed.
canonical_mcp_url = f"http://127.0.0.1:{traefik_port}{ingress_path}"
local_anon_url = f"http://127.0.0.1:{anon_proxy_port}{ingress_path}"
local_session_url = f"http://127.0.0.1:{session_proxy_port}{ingress_path}"
import json
config = {
    "mcpServers": {
        server_name: {"url": local_session_url},
        f"{server_name}-anon": {"url": local_anon_url},
    }
}
print(f"[cli] Canonical ingress URL for {server_name}: {canonical_mcp_url}")
print(f"[cli] Local e2e MCP client config for {server_name}:")
print(json.dumps(config, indent=2))
PYEOF

echo "[policy] applying access grant via CLI"
cat >"${WORKDIR}/access-grant.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${SERVER_NAME}-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  maxTrust: high
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
    - name: echo
      decision: allow
    - name: upper
      decision: allow
EOF
(cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube grant apply --file access-grant.yaml)

echo "[policy] applying low-trust session via CLI"
cat >"${WORKDIR}/access-session.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: ${SESSION_ID}
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  consentedTrust: low
  policyVersion: v1
EOF
(cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube session apply --file access-session.yaml)

wait_for_policy_text "\"name\": \"${SESSION_ID}\""
wait_for_policy_text "\"consented_trust\": \"low\""
print_gateway_policy_debug
./bin/mcp-runtime server --use-kube policy inspect "${SERVER_NAME}" \
  --namespace mcp-servers >"${WORKDIR}/server-policy.json"
assert_file_contains "${SESSION_ID}" "${WORKDIR}/server-policy.json"

if scenario_selected "governance"; then
  echo "[cli] checking access management commands"
  ./bin/mcp-runtime access --use-kube grant list --namespace mcp-servers >"${WORKDIR}/access-grant-list.txt"
  assert_file_contains "${SERVER_NAME}-grant" "${WORKDIR}/access-grant-list.txt"
  ./bin/mcp-runtime access --use-kube grant get "${SERVER_NAME}-grant" --namespace mcp-servers >"${WORKDIR}/access-grant-get.yaml"
  assert_file_contains "maxTrust: high" "${WORKDIR}/access-grant-get.yaml"
  ./bin/mcp-runtime access --use-kube session list --namespace mcp-servers >"${WORKDIR}/access-session-list.txt"
  assert_file_contains "${SESSION_ID}" "${WORKDIR}/access-session-list.txt"
  ./bin/mcp-runtime access --use-kube session get "${SESSION_ID}" --namespace mcp-servers >"${WORKDIR}/access-session-get.yaml"
  assert_file_contains "consentedTrust: low" "${WORKDIR}/access-session-get.yaml"

  cat >"${WORKDIR}/access-temp.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${SERVER_NAME}-grant-cli-temp
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}-cli-temp
    agentID: ${AGENT_ID}-cli-temp
  maxTrust: low
  policyVersion: v1
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: ${SESSION_ID}-cli-temp
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}-cli-temp
    agentID: ${AGENT_ID}-cli-temp
  consentedTrust: low
  policyVersion: v1
EOF
  ./bin/mcp-runtime access --use-kube grant apply --file "${WORKDIR}/access-temp.yaml"
  ./bin/mcp-runtime access --use-kube grant disable "${SERVER_NAME}-grant-cli-temp" --namespace mcp-servers
  ./bin/mcp-runtime access --use-kube grant enable "${SERVER_NAME}-grant-cli-temp" --namespace mcp-servers
  ./bin/mcp-runtime access --use-kube session revoke "${SESSION_ID}-cli-temp" --namespace mcp-servers
  ./bin/mcp-runtime access --use-kube session unrevoke "${SESSION_ID}-cli-temp" --namespace mcp-servers
  ./bin/mcp-runtime access --use-kube grant delete "${SERVER_NAME}-grant-cli-temp" --namespace mcp-servers
  ./bin/mcp-runtime access --use-kube session delete "${SESSION_ID}-cli-temp" --namespace mcp-servers
fi

echo "[port-forward] exposing ingress and observability services"
port_forward_bg traefik traefik "${TRAEFIK_PORT}" 8000 "${WORKDIR}/traefik-port-forward.log"
port_forward_bg mcp-sentinel mcp-sentinel-gateway "${SENTINEL_PORT}" 8083 "${WORKDIR}/sentinel-port-forward.log"
port_forward_bg mcp-sentinel tempo "${TEMPO_PORT}" 3200 "${WORKDIR}/tempo-port-forward.log"
port_forward_bg mcp-sentinel loki "${LOKI_PORT}" 3100 "${WORKDIR}/loki-port-forward.log"
if scenario_selected "governance" || scenario_selected "observability"; then
  port_forward_bg mcp-sentinel mcp-sentinel-api "${API_SERVICE_PORT}" 8080 "${WORKDIR}/api-port-forward.log"
fi
if scenario_selected "observability"; then
  port_forward_bg mcp-sentinel mcp-sentinel-api "${API_METRICS_PORT}" 9090 "${WORKDIR}/api-metrics-port-forward.log"
  port_forward_bg mcp-sentinel mcp-sentinel-ingest "${INGEST_SERVICE_PORT}" 8081 "${WORKDIR}/ingest-port-forward.log"
  port_forward_bg mcp-sentinel mcp-sentinel-ingest "${INGEST_METRICS_PORT}" 9091 "${WORKDIR}/ingest-metrics-port-forward.log"
  port_forward_bg mcp-sentinel mcp-sentinel-processor "${PROCESSOR_METRICS_PORT}" 9102 "${WORKDIR}/processor-metrics-port-forward.log"
  port_forward_bg mcp-sentinel mcp-sentinel-ui "${UI_SERVICE_PORT}" 8082 "${WORKDIR}/ui-port-forward.log"
  port_forward_bg mcp-servers "${SERVER_NAME}" "${SERVER_PROXY_PORT}" 80 "${WORKDIR}/server-proxy-port-forward.log"
  port_forward_resource_bg mcp-servers "deployment/${SERVER_NAME}" "${SERVER_UPSTREAM_PORT}" 8090 "${WORKDIR}/server-upstream-port-forward.log"
fi

wait_port "${TRAEFIK_PORT}"
wait_port "${SENTINEL_PORT}"
wait_port "${TEMPO_PORT}"
wait_port "${LOKI_PORT}"
if scenario_selected "governance" || scenario_selected "observability"; then
  wait_port "${API_SERVICE_PORT}"
fi
if scenario_selected "observability"; then
  wait_port "${API_METRICS_PORT}"
  wait_port "${INGEST_SERVICE_PORT}"
  wait_port "${INGEST_METRICS_PORT}"
  wait_port "${PROCESSOR_METRICS_PORT}"
  wait_port "${UI_SERVICE_PORT}"
  wait_port "${SERVER_PROXY_PORT}"
  wait_port "${SERVER_UPSTREAM_PORT}"
fi
wait_http "http://127.0.0.1:${SENTINEL_PORT}/api/stats" "x-api-key: ${API_KEY}"
wait_http "http://127.0.0.1:${TEMPO_PORT}/ready"
wait_http "http://127.0.0.1:${LOKI_PORT}/ready"

echo "[registry] checking public ingress admin auth"
REGISTRY_PUBLIC_URL="http://127.0.0.1:${TRAEFIK_PORT}/v2/_catalog"
REGISTRY_UNAUTH_STATUS="$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: registry.local" "${REGISTRY_PUBLIC_URL}" || true)"
if [[ "${REGISTRY_UNAUTH_STATUS}" != "401" && "${REGISTRY_UNAUTH_STATUS}" != "403" ]]; then
  echo "[registry][fail] unauthenticated public registry catalog returned ${REGISTRY_UNAUTH_STATUS}, want 401 or 403" >&2
  exit 1
fi
REGISTRY_ADMIN_STATUS="$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: registry.local" -H "x-api-key: ${API_KEY}" "${REGISTRY_PUBLIC_URL}" || true)"
if [[ "${REGISTRY_ADMIN_STATUS}" != "200" ]]; then
  echo "[registry][fail] admin public registry catalog returned ${REGISTRY_ADMIN_STATUS}, want 200" >&2
  exit 1
fi
echo "[registry][pass] public registry catalog requires admin auth"

echo "[proxy] starting local ingress proxies for curl MCP checks"
start_header_proxy_bg "${MCP_CURL_ANON_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-anon-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
start_header_proxy_bg "${MCP_CURL_IDENTITY_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-identity-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}"
start_header_proxy_bg "${MCP_CURL_SESSION_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-session-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}" \
  --header "X-MCP-Agent-Session=${SESSION_ID}"
start_header_proxy_bg "${MCP_CURL_BAD_SESSION_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-bad-session-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}" \
  --header "X-MCP-Agent-Session=${UNKNOWN_SESSION_ID}"
start_header_proxy_bg "${PYTHON_EXAMPLE_PROXY_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/python-example-proxy.log" \
  --host-header "${PYTHON_EXAMPLE_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
start_header_proxy_bg "${RUST_EXAMPLE_PROXY_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/rust-example-proxy.log" \
  --host-header "${RUST_EXAMPLE_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
start_header_proxy_bg "${GO_EXAMPLE_PROXY_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/go-example-proxy.log" \
  --host-header "${GO_EXAMPLE_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
wait_port "${MCP_CURL_ANON_PORT}"
wait_port "${MCP_CURL_IDENTITY_PORT}"
wait_port "${MCP_CURL_SESSION_PORT}"
wait_port "${MCP_CURL_BAD_SESSION_PORT}"
wait_port "${PYTHON_EXAMPLE_PROXY_PORT}"
wait_port "${RUST_EXAMPLE_PROXY_PORT}"
wait_port "${GO_EXAMPLE_PROXY_PORT}"

MCP_INGRESS_PATH="/${SERVER_NAME}/mcp"
MCP_DIRECT_URL="http://127.0.0.1:${TRAEFIK_PORT}${MCP_INGRESS_PATH}"
MCP_ANON_URL="http://127.0.0.1:${MCP_CURL_ANON_PORT}${MCP_INGRESS_PATH}"
MCP_IDENTITY_URL="http://127.0.0.1:${MCP_CURL_IDENTITY_PORT}${MCP_INGRESS_PATH}"
MCP_SESSION_URL="http://127.0.0.1:${MCP_CURL_SESSION_PORT}${MCP_INGRESS_PATH}"
MCP_BAD_SESSION_URL="http://127.0.0.1:${MCP_CURL_BAD_SESSION_PORT}${MCP_INGRESS_PATH}"
PYTHON_EXAMPLE_URL="http://127.0.0.1:${PYTHON_EXAMPLE_PROXY_PORT}${PYTHON_EXAMPLE_SERVER_ROUTE}"
RUST_EXAMPLE_URL="http://127.0.0.1:${RUST_EXAMPLE_PROXY_PORT}${RUST_EXAMPLE_SERVER_ROUTE}"
GO_EXAMPLE_URL="http://127.0.0.1:${GO_EXAMPLE_PROXY_PORT}${GO_EXAMPLE_SERVER_ROUTE}"

log_line ingress "validating distinct MCP server behaviors across routes"
wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200 "pong"
wait_for_mcp_tool_result "${PYTHON_EXAMPLE_URL}" "echo" '{"message":"python example ready"}' 200 "python example ready"
wait_for_mcp_tool_result "${RUST_EXAMPLE_URL}" "repeat" '{"message":"rust","times":3}' 200 "rustrustrust"
wait_for_mcp_tool_result "${GO_EXAMPLE_URL}" "lower" '{"message":"GO Example Ready"}' 200 "go example ready"

if scenario_selected "smoke-auth"; then
  log_line mcp "validating raw MCP request edge cases"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=2099-01-01")" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    400 \
    "Unsupported protocol version"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=text/plain" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    text \
    '' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    text \
    '{"jsonrpc":' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    text \
    '{"jsonrpc":"2.0","id":1,"params":{}}' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream")" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    chunked-text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    GET \
    "$(build_headers_json "Host=${SERVER_HOST}" "accept=text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    none \
    '' \
    400 \
    "GET requires an Mcp-Session-Id header"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    DELETE \
    "$(build_headers_json "Host=${SERVER_HOST}" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}")" \
    none \
    '' \
    400 \
    "DELETE requires an Mcp-Session-Id header"

  log_line mcp "running curl-based MCP smoke checks against ingress"
  run_mcp_curl_expect "mcp-curl-missing-identity" "${MCP_ANON_URL}" false "missing_identity" \
    || run_mcp_curl_expect "mcp-curl-missing-identity-retry" "${MCP_ANON_URL}" false "missing_identity"
  run_mcp_curl_expect "mcp-curl-missing-session" "${MCP_IDENTITY_URL}" false "missing_session" \
    || run_mcp_curl_expect "mcp-curl-missing-session-retry" "${MCP_IDENTITY_URL}" false "missing_session"
  run_mcp_curl_expect "mcp-curl-session-not-found" "${MCP_BAD_SESSION_URL}" false "session_not_found" \
    || run_mcp_curl_expect "mcp-curl-session-not-found-retry" "${MCP_BAD_SESSION_URL}" false "session_not_found"
  log_line mcp "waiting for session-backed allow policy to reach the gateway"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200
  run_mcp_curl_expect "mcp-curl-allow-aaa-ping" "${MCP_SESSION_URL}" true
  if scenario_selected "observability"; then
    wait_for_gateway_rpc_methods "${SERVER_NAME}" "policy server MCP curl"
  fi
fi

if scenario_selected "governance"; then
  log_line policy "revoking access session via CLI; gateway should reject session-backed calls with session_revoked"
  ./bin/mcp-runtime access --use-kube session revoke "${SESSION_ID}" --namespace mcp-servers
  wait_for_policy_text "\"revoked\": true"
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 401 "session_revoked"
  run_mcp_curl_expect "mcp-curl-session-revoked" "${MCP_SESSION_URL}" false "session_revoked"

  log_line policy "restoring access session via CLI; gateway should allow low-trust tools again"
  ./bin/mcp-runtime access --use-kube session unrevoke "${SESSION_ID}" --namespace mcp-servers
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200

  log_line policy "expiring access session via manifest update; gateway should reject calls with session_expired"
  EXPIRED_AT="$(python3 <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc) - timedelta(minutes=5)).replace(microsecond=0).isoformat().replace("+00:00", "Z"))
PY
)"
  cat >"${WORKDIR}/access-session-expired.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: ${SESSION_ID}
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  consentedTrust: low
  policyVersion: v1
  expiresAt: ${EXPIRED_AT}
EOF
  (cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube session apply --file access-session-expired.yaml)
  wait_for_policy_text "\"expires_at\": \"${EXPIRED_AT}\""
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 401 "session_expired"
  run_mcp_curl_expect "mcp-curl-session-expired" "${MCP_SESSION_URL}" false "session_expired"

  log_line policy "restoring non-expired access session"
  cat >"${WORKDIR}/access-session-restored.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: ${SESSION_ID}
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  consentedTrust: low
  policyVersion: v1
EOF
  (cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube session apply --file access-session-restored.yaml)
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200

  log_line policy "disabling access grant via CLI; gateway should reject granted tools with tool_not_granted"
  ./bin/mcp-runtime access --use-kube grant disable "${SERVER_NAME}-grant" --namespace mcp-servers
  wait_for_policy_text "\"disabled\": true"
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 403 "tool_not_granted"
  run_mcp_curl_expect "mcp-curl-grant-disabled" "${MCP_SESSION_URL}" false "tool_not_granted"

  log_line policy "re-enabling access grant via CLI"
  ./bin/mcp-runtime access --use-kube grant enable "${SERVER_NAME}-grant" --namespace mcp-servers
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200

  # Phase 6: exercise the platform-issued adapter-session endpoint. The
  # existing governance grant pins humanID to ${HUMAN_ID}, while this flow
  # calls the endpoint as the platform admin principal. Apply a second,
  # subject-wildcard grant just for this test. The endpoint must pick the
  # wildcard grant, write/reuse an MCPAgentSession with the deterministic
  # adapter-<hash> name, and report reused=true on the second call.
  log_line policy "applying subject-wildcard grant for adapter-session test"
  cat >"${WORKDIR}/adapter-session-grant.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${SERVER_NAME}-adapter-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject: {}
  maxTrust: low
  allowedSideEffects: [read]
  policyVersion: v1
EOF
  (cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube grant apply --file adapter-session-grant.yaml)

  # Use a fresh agentID per e2e invocation so deterministic-name reuse
  # doesn't leak across re-runs in E2E_CACHE_MODE=1 (where the cluster and
  # prior adapter-<hash> sessions are retained between runs). A timestamp
  # suffix is sufficient; the assertions below verify the platform's own
  # reuse semantics (reused=true on the *second* call within this run).
  ADAPTER_AGENT_ID="e2e-adapter-agent-$(date +%s)"

  log_line policy "logging in platform admin for adapter-session test"
  ADAPTER_PLATFORM_TOKEN="$(PLATFORM_ADMIN_EMAIL="${PLATFORM_ADMIN_EMAIL}" PLATFORM_ADMIN_PASSWORD="${PLATFORM_ADMIN_PASSWORD}" python3 -c '
import json, os
print(json.dumps({"email": os.environ["PLATFORM_ADMIN_EMAIL"], "password": os.environ["PLATFORM_ADMIN_PASSWORD"]}))
' | curl -fsS -X POST \
    -H "content-type: application/json" \
    --data-binary @- \
    "http://127.0.0.1:${API_SERVICE_PORT}/api/auth/login" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"

  log_line policy "adapter-session endpoint should issue a session for the wildcard grant"
  ADAPTER_SESSION_BODY="$(printf '{"serverName":"%s","namespace":"mcp-servers","agentID":"%s"}' "${SERVER_NAME}" "${ADAPTER_AGENT_ID}")"
  ADAPTER_SESSION_RESP="$(curl -fsS -X POST \
    -H "Authorization: Bearer ${ADAPTER_PLATFORM_TOKEN}" \
    -H "content-type: application/json" \
    --data "${ADAPTER_SESSION_BODY}" \
    "http://127.0.0.1:${SENTINEL_PORT}/api/runtime/adapter/sessions")"
  echo "${ADAPTER_SESSION_RESP}" | ADAPTER_AGENT_ID="${ADAPTER_AGENT_ID}" python3 -c "
import json, os, sys
resp = json.load(sys.stdin)
assert resp['namespace'] == 'mcp-servers', resp
assert resp['serverName'] == '${SERVER_NAME}', resp
assert resp['agentID'] == os.environ['ADAPTER_AGENT_ID'], resp
assert resp['name'].startswith('adapter-'), resp
assert resp['humanID'], 'humanID derived from principal must be non-empty: %r' % resp
assert resp['consentedTrust'] in ('none','low','mid','high','full'), resp
assert resp['expiresAt'], resp
print('adapter-session issued:', resp['name'], 'reused=', resp['reused'])
"
  ADAPTER_SESSION_NAME="$(echo "${ADAPTER_SESSION_RESP}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["name"])')"
  if ! kubectl get mcpagentsession "${ADAPTER_SESSION_NAME}" -n mcp-servers >/dev/null 2>&1; then
    echo "expected MCPAgentSession ${ADAPTER_SESSION_NAME} in mcp-servers" >&2
    exit 1
  fi

  # The second call must hit the platform's reuse path: same body within the
  # same run, no Kubernetes round-trip, reused=true. This is independent of
  # whether the first call hit a leftover from a previous e2e run.
  log_line policy "adapter-session endpoint should reuse the existing session on a second call"
  ADAPTER_SESSION_RESP2="$(curl -fsS -X POST \
    -H "Authorization: Bearer ${ADAPTER_PLATFORM_TOKEN}" \
    -H "content-type: application/json" \
    --data "${ADAPTER_SESSION_BODY}" \
    "http://127.0.0.1:${SENTINEL_PORT}/api/runtime/adapter/sessions")"
  echo "${ADAPTER_SESSION_RESP2}" | python3 -c "
import json, sys
resp = json.load(sys.stdin)
assert resp['name'] == '${ADAPTER_SESSION_NAME}', resp
assert resp['reused'] is True, resp
print('adapter-session reused:', resp['name'])
"

  log_line policy "adapter-session endpoint must reject requests with no matching grant"
  ADAPTER_SESSION_REJECT_STATUS="$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
    -H "Authorization: Bearer ${ADAPTER_PLATFORM_TOKEN}" \
    -H "content-type: application/json" \
    --data '{"serverName":"definitely-missing","namespace":"mcp-servers","agentID":"ops-agent"}' \
    "http://127.0.0.1:${SENTINEL_PORT}/api/runtime/adapter/sessions")"
  if [[ "${ADAPTER_SESSION_REJECT_STATUS}" != "403" ]]; then
    echo "expected 403 when no grant matches, got ${ADAPTER_SESSION_REJECT_STATUS}" >&2
    exit 1
  fi
fi

if scenario_selected "trust"; then
  log_line mcp "validating targeted echo and upper tool behavior"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]
initialize_payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": protocol,
        "capabilities": {},
        "clientInfo": {"name": "mcp-runtime-e2e", "version": "1.0.0"},
    },
}


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


def post(msg, mcp_session_id=None):
    headers = {
        "content-type": "application/json",
        "accept": "application/json, text/event-stream",
        "Mcp-Protocol-Version": protocol,
    }
    if mcp_session_id:
        headers["Mcp-Session-Id"] = mcp_session_id
    req = urllib.request.Request(base, data=json.dumps(msg).encode(), headers=headers)
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return resp.status, resp.headers.get("Mcp-Session-Id") or mcp_session_id, resp.read().decode()
    except urllib.error.HTTPError as exc:
        return exc.code, exc.headers.get("Mcp-Session-Id") or mcp_session_id, exc.read().decode()


status, mcp_session_id, body = post(initialize_payload)
check(
    status == 200 and bool(mcp_session_id),
    "trust pre-update initialize succeeded",
    f"initialize failed before trust update: {status} {body}",
)

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
check(
    status in (200, 202),
    "trust pre-update notifications/initialized succeeded",
    f"notifications/initialized failed: {status} {body}",
)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "echo", "arguments": {"message": "hello"}}},
    mcp_session_id=mcp_session_id,
)
check(
    status == 200 and "hello" in body,
    "trust pre-update echo allowed",
    f"expected echo to succeed before trust update, got {status}: {body}",
)
print("echo allow:", body)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "upper", "arguments": {"message": "governance"}}},
    mcp_session_id=mcp_session_id,
)
payload = json.loads(body)
check(
    status == 403 and payload.get("error") == "trust_too_low",
    "trust pre-update upper denied with trust_too_low",
    f"expected upper to be denied before trust update, got {status}: {body}",
)
print("upper deny:", body)
PY

  log_line policy "raising consented trust to medium; upper should become allowed while add stays ungranted"
  cat <<EOF | kubectl apply -f -
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: ${SESSION_ID}
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  consentedTrust: medium
  policyVersion: v1
EOF

  wait_for_policy_text "\"consented_trust\": \"medium\""
  print_gateway_policy_debug
  log_line mcp "waiting for updated consented trust to reach the gateway"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "upper" '{"message":"governance"}' 200 "GOVERNANCE"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "add" '{"a":2,"b":3}' 403 "tool_not_granted"

  log_line mcp "validating updated policy allows the higher-trust tool"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())

initialize_payload = make_initialize_payload(protocol)


def post(msg, mcp_session_id=None):
    headers = {
        "content-type": "application/json",
        "accept": "application/json, text/event-stream",
        "Mcp-Protocol-Version": protocol,
    }
    if mcp_session_id:
        headers["Mcp-Session-Id"] = mcp_session_id
    req = urllib.request.Request(base, data=json.dumps(msg).encode(), headers=headers)
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return resp.status, resp.headers.get("Mcp-Session-Id") or mcp_session_id, resp.read().decode()
    except urllib.error.HTTPError as exc:
        return exc.code, exc.headers.get("Mcp-Session-Id") or mcp_session_id, exc.read().decode()


status, mcp_session_id, body = post({
    **initialize_payload,
    "id": 6,
})
check(
    status == 200 and bool(mcp_session_id),
    "trust post-update initialize succeeded",
    f"initialize failed after trust update: {status} {body}",
)

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
check(
    status in (200, 202),
    "trust post-update notifications/initialized succeeded",
    f"notifications/initialized failed: {status} {body}",
)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": {"name": "upper", "arguments": {"message": "governance"}}},
    mcp_session_id=mcp_session_id,
)
check(
    status == 200,
    "trust post-update upper returned 200",
    f"expected upper to succeed after trust update, got {status}: {body}",
)
check(
    "GOVERNANCE" in body,
    "trust post-update upper returned GOVERNANCE",
    f"expected uppercase result, got {body}",
)
print("upper allow:", body)
PY

  log_line policy "temporarily expanding grant for deterministic multi-tool MCP checks"
  cat <<EOF | kubectl apply -f -
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${SERVER_NAME}-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  maxTrust: high
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
    - name: echo
      decision: allow
    - name: upper
      decision: allow
    - name: add
      decision: allow
    - name: slugify
      decision: allow
EOF
  wait_for_policy_text "\"slugify\""
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "add" '{"a":41,"b":1}' 200 "42"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "slugify" '{"message":"Hello World"}' 200 "hello-world"

  log_line policy "updating access grant to deny aaa-ping and echo"
  cat >"${WORKDIR}/access-grant-deny.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${SERVER_NAME}-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${SERVER_NAME}
  subject:
    humanID: ${HUMAN_ID}
    agentID: ${AGENT_ID}
  maxTrust: high
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: deny
    - name: echo
      decision: deny
    - name: upper
      decision: allow
EOF
  (cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube grant apply --file access-grant-deny.yaml)

  wait_for_grant_tool_rule "${SERVER_NAME}-grant" "aaa-ping" "deny"
  wait_for_grant_tool_rule "${SERVER_NAME}-grant" "echo" "deny"
  print_gateway_policy_debug

  log_line mcp "validating updated access grant denies aaa-ping and echo"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 403 "tool_denied"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "echo" '{"message":"analytics"}' 403 "tool_denied"
  run_mcp_curl_expect "mcp-curl-aaa-ping-deny" "${MCP_SESSION_URL}" false "tool_denied"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


def post(msg, mcp_session_id=None):
    headers = {
        "content-type": "application/json",
        "accept": "application/json, text/event-stream",
        "Mcp-Protocol-Version": protocol,
    }
    if mcp_session_id:
        headers["Mcp-Session-Id"] = mcp_session_id
    req = urllib.request.Request(base, data=json.dumps(msg).encode(), headers=headers)
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return resp.status, resp.headers.get("Mcp-Session-Id") or mcp_session_id, resp.read().decode()
    except urllib.error.HTTPError as exc:
        return exc.code, exc.headers.get("Mcp-Session-Id") or mcp_session_id, exc.read().decode()


status, mcp_session_id, body = post({"jsonrpc": "2.0", "id": 8, "method": "initialize", "params": {}})
check(
    status == 200 and bool(mcp_session_id),
    "grant update initialize succeeded",
    f"initialize failed after grant update: {status} {body}",
)

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
check(
    status in (200, 202),
    "grant update notifications/initialized succeeded",
    f"notifications/initialized failed: {status} {body}",
)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": {"name": "echo", "arguments": {"message": "analytics"}}},
    mcp_session_id=mcp_session_id,
)
payload = json.loads(body)
check(
    status == 403 and payload.get("error") == "tool_denied",
    "grant update echo denied with tool_denied",
    f"expected echo to be denied after grant update, got {status}: {body}",
)
print("echo deny:", body)
PY
fi

if scenario_selected "oauth"; then
  OAUTH_FIXTURE_DIR="${WORKDIR}/oauth-fixtures"
  generate_oauth_fixtures "${OAUTH_FIXTURE_DIR}"
  OAUTH_VALID_TOKEN="$(tr -d '\n' <"${OAUTH_FIXTURE_DIR}/valid-token.txt")"
  OAUTH_INVALID_TOKEN="$(tr -d '\n' <"${OAUTH_FIXTURE_DIR}/invalid-token.txt")"

  echo "[oauth] deploying mock OAuth issuer"
  kubectl create configmap "${OAUTH_ISSUER_NAME}-files" \
  -n mcp-servers \
  --from-file=oauth-authorization-server="${OAUTH_FIXTURE_DIR}/oauth-authorization-server" \
  --from-file=keys="${OAUTH_FIXTURE_DIR}/keys" \
  --dry-run=client -o yaml | kubectl apply -f -
  cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${OAUTH_ISSUER_NAME}
  namespace: mcp-servers
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${OAUTH_ISSUER_NAME}
  template:
    metadata:
      labels:
        app: ${OAUTH_ISSUER_NAME}
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: http
          image: docker.io/library/python:3.12-alpine
          command: ["python3"]
          args: ["-m", "http.server", "8080", "--directory", "/usr/share/nginx/html"]
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            runAsNonRoot: true
            runAsUser: 65532
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: files
              mountPath: /usr/share/nginx/html/.well-known/oauth-authorization-server
              subPath: oauth-authorization-server
            - name: files
              mountPath: /usr/share/nginx/html/keys
              subPath: keys
      volumes:
        - name: files
          configMap:
            name: ${OAUTH_ISSUER_NAME}-files
---
apiVersion: v1
kind: Service
metadata:
  name: ${OAUTH_ISSUER_NAME}
  namespace: mcp-servers
spec:
  selector:
    app: ${OAUTH_ISSUER_NAME}
  ports:
    - name: http
      port: 8080
      targetPort: 8080
EOF
  # The issuer mounts fixture files through subPath, so restart to pick up new
  # JWKS/token fixtures when E2E cache mode reuses an existing Deployment.
  kubectl rollout restart "deploy/${OAUTH_ISSUER_NAME}" -n mcp-servers >/dev/null
  kubectl rollout status "deploy/${OAUTH_ISSUER_NAME}" -n mcp-servers --timeout=180s

  OAUTH_METADATA_FILE="${WORKDIR}/oauth-metadata.yaml"
  OAUTH_MANIFEST_DIR="${WORKDIR}/oauth-manifests"
  cat > "${OAUTH_METADATA_FILE}" <<EOF
version: v1
servers:
  - name: ${OAUTH_SERVER_NAME}
    image: ${SERVER_IMAGE%:*}
    imageTag: ${SERVER_IMAGE##*:}
    route: /${OAUTH_SERVER_NAME}/mcp
    publicPathPrefix: ${OAUTH_SERVER_NAME}
    port: 8090
    namespace: mcp-servers
    resources:
      requests:
        cpu: 1m
        memory: 32Mi
    rollout:
      maxUnavailable: "1"
      maxSurge: "0"
    envVars:
      - name: PORT
        value: "8090"
      - name: MCP_PATH
        value: "/${OAUTH_SERVER_NAME}/mcp"
    tools:
      - name: aaa-ping
        requiredTrust: low
        sideEffect: read
      - name: add
        requiredTrust: low
        sideEffect: read
      - name: upper
        requiredTrust: low
        sideEffect: read
    auth:
      mode: oauth
      humanIDHeader: X-MCP-Human-ID
      agentIDHeader: X-MCP-Agent-ID
      sessionIDHeader: X-MCP-Agent-Session
      tokenHeader: Authorization
      issuerURL: ${OAUTH_ISSUER_URL}
      audience: ${OAUTH_AUDIENCE}
    policy:
      mode: allow-list
      defaultDecision: deny
      policyVersion: v1
    session:
      required: false
      upstreamTokenHeader: Authorization
    gateway:
      enabled: true
      resources:
        requests:
          cpu: 1m
          memory: 32Mi
    analytics:
      enabled: true
      ingestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      apiKeySecretRef:
        name: ${SERVER_SECRET_NAME}
        key: api-key
EOF

  echo "[oauth] deploying OAuth-protected MCP server"
  run_logged_stage "deploy OAuth MCP server manifests" deploy_oauth_server_manifests
  wait_for_deployment_exists mcp-servers "${OAUTH_SERVER_NAME}"
  if ! restart_deployment_pods mcp-servers "${OAUTH_SERVER_NAME}" 180s; then
    echo "[debug] OAuth MCP server rollout failed; collecting diagnostics" >&2
    kubectl get mcpserver "${OAUTH_SERVER_NAME}" -n mcp-servers -o yaml || true
    kubectl get deploy,rs,pods,svc,ingress,configmap -n mcp-servers || true
    kubectl describe deployment "${OAUTH_SERVER_NAME}" -n mcp-servers || true
    kubectl describe pods -n mcp-servers || true
    kubectl logs -n mcp-servers -l "app=${OAUTH_SERVER_NAME}" --all-containers=true --tail=200 || true
    exit 1
  fi
  wait_for_named_server_ready "${OAUTH_SERVER_NAME}"

  echo "[oauth] applying OAuth grant"
  cat <<EOF | kubectl apply -f -
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${OAUTH_SERVER_NAME}-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${OAUTH_SERVER_NAME}
  subject:
    humanID: ${OAUTH_HUMAN_ID}
    agentID: ${OAUTH_AGENT_ID}
  maxTrust: low
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
    - name: add
      decision: allow
    - name: upper
      decision: allow
EOF
  
  echo "[oauth] starting local ingress proxies"
  # mcp_header_proxy.py uses NAME=VALUE syntax: the part after the first '='
  # becomes the HTTP header value, so "Authorization=Bearer <token>" sets the
  # Authorization header to "Bearer <token>" (not "=Bearer <token>").
  start_header_proxy_bg "${MCP_CURL_OAUTH_ANON_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-oauth-anon-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
  start_header_proxy_bg "${MCP_CURL_OAUTH_INVALID_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-oauth-invalid-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "Authorization=Bearer ${OAUTH_INVALID_TOKEN}"
  start_header_proxy_bg "${MCP_CURL_OAUTH_VALID_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-curl-oauth-valid-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "Authorization=Bearer ${OAUTH_VALID_TOKEN}"
  
  wait_port "${MCP_CURL_OAUTH_ANON_PORT}"
  wait_port "${MCP_CURL_OAUTH_INVALID_PORT}"
  wait_port "${MCP_CURL_OAUTH_VALID_PORT}"
  if scenario_selected "observability"; then
    port_forward_bg mcp-servers "${OAUTH_SERVER_NAME}" "${OAUTH_PROXY_PORT}" 80 "${WORKDIR}/oauth-proxy-port-forward.log"
    port_forward_resource_bg mcp-servers "deployment/${OAUTH_SERVER_NAME}" "${OAUTH_UPSTREAM_PORT}" 8090 "${WORKDIR}/oauth-upstream-port-forward.log"
    wait_port "${OAUTH_PROXY_PORT}"
    wait_port "${OAUTH_UPSTREAM_PORT}"
  fi

  OAUTH_INGRESS_PATH="/${OAUTH_SERVER_NAME}/mcp"
  MCP_OAUTH_DIRECT_URL="http://127.0.0.1:${TRAEFIK_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_ANON_URL="http://127.0.0.1:${MCP_CURL_OAUTH_ANON_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_INVALID_URL="http://127.0.0.1:${MCP_CURL_OAUTH_INVALID_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_VALID_URL="http://127.0.0.1:${MCP_CURL_OAUTH_VALID_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_METADATA_URL="http://127.0.0.1:${MCP_CURL_OAUTH_ANON_PORT}/.well-known/oauth-protected-resource${OAUTH_INGRESS_PATH}"

  echo "[oauth] validating protected-resource metadata"
  wait_http "${MCP_OAUTH_METADATA_URL}"
  MCP_OAUTH_METADATA_URL="${MCP_OAUTH_METADATA_URL}" \
  OAUTH_ISSUER_URL="${OAUTH_ISSUER_URL}" \
  OAUTH_RESOURCE_URL="http://${OAUTH_SERVER_HOST}${OAUTH_INGRESS_PATH}" \
  python3 <<'PY'
import json
import os
import urllib.request


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


req = urllib.request.Request(os.environ["MCP_OAUTH_METADATA_URL"], headers={"accept": "application/json"})
resp = urllib.request.urlopen(req, timeout=10)
doc = json.loads(resp.read().decode())

check(
    resp.status == 200,
    "oauth protected-resource metadata returned 200",
    f"expected 200 from protected resource metadata, got {resp.status}",
)
check(
    doc.get("authorization_servers") == [os.environ["OAUTH_ISSUER_URL"]],
    "oauth metadata authorization_servers matched issuer",
    f"unexpected authorization_servers: {doc}",
)
check(
    doc.get("resource") == os.environ["OAUTH_RESOURCE_URL"],
    "oauth metadata resource URL matched",
    f"unexpected resource URL: {doc}",
)
check(
    "header" in doc.get("bearer_methods_supported", []),
    "oauth metadata bearer_methods_supported includes header",
    f"expected bearer_methods_supported to include header, got {doc}",
)
print("oauth metadata:", json.dumps(doc))
PY

  echo "[oauth] validating missing and invalid bearer token challenges"
  wait_for_mcp_initialize_result "${MCP_OAUTH_ANON_URL}" 401 "missing_bearer_token" "www-authenticate" "resource_metadata="
  wait_for_mcp_initialize_result "${MCP_OAUTH_INVALID_URL}" 401 "invalid_token" "www-authenticate" 'error="invalid_token"'
  wait_for_http_result \
    "${MCP_OAUTH_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${OAUTH_SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" "Authorization=${OAUTH_VALID_TOKEN}")" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    401 \
    "missing_bearer_token" \
    "www-authenticate" \
    "resource_metadata="
  run_mcp_curl_expect "mcp-curl-oauth-missing-token" "${MCP_OAUTH_ANON_URL}" false "missing_bearer_token"
  run_mcp_curl_expect "mcp-curl-oauth-invalid-token" "${MCP_OAUTH_INVALID_URL}" false "invalid_token"

  echo "[oauth] validating valid bearer token MCP flow"
  wait_for_mcp_tool_result "${MCP_OAUTH_VALID_URL}" "add" '{"a":7,"b":5}' 200 "12"
  API_BASE="http://127.0.0.1:${SENTINEL_PORT}/api" \
  API_KEY="${API_KEY}" \
  OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME}" \
  OAUTH_HUMAN_ID="${OAUTH_HUMAN_ID}" \
  OAUTH_AGENT_ID="${OAUTH_AGENT_ID}" \
  OAUTH_SESSION_ID="${OAUTH_SESSION_ID}" \
  python3 <<'PY'
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


api_base = os.environ["API_BASE"]
api_key = os.environ["API_KEY"]
server_name = os.environ["OAUTH_SERVER_NAME"]
human_id = os.environ["OAUTH_HUMAN_ID"]
agent_id = os.environ["OAUTH_AGENT_ID"]
session_id = os.environ["OAUTH_SESSION_ID"]

params = urllib.parse.urlencode(
    {
        "server": server_name,
        "decision": "allow",
        "tool_name": "add",
        "human_id": human_id,
        "agent_id": agent_id,
        "session_id": session_id,
        "limit": "20",
    }
)
url = f"{api_base}/events/filter?{params}"
headers = {"x-api-key": api_key}
last_doc = {}

for _ in range(60):
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            last_doc = json.loads(resp.read().decode())
    except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, OSError):
        time.sleep(2)
        continue
    events = last_doc.get("events", [])
    if events:
        payload = events[0].get("payload", {})
        check(
            payload.get("human_id") == human_id
            and payload.get("agent_id") == agent_id
            and payload.get("session_id") == session_id
            and payload.get("tool_name") == "add"
            and payload.get("decision") == "allow",
            "oauth bearer token identity appeared in allow audit event",
            f"unexpected oauth allow audit payload: {payload}",
        )
        break
    time.sleep(2)
else:
    fail(f"timed out waiting for oauth allow audit event: {json.dumps(last_doc, indent=2)}")
PY
  wait_for_http_result \
    "${MCP_OAUTH_DIRECT_URL}" \
    POST \
    "$(build_headers_json "Host=${OAUTH_SERVER_HOST}" "content-type=application/json" "accept=application/json, text/event-stream" "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" "Authorization=Bearer ${OAUTH_VALID_TOKEN}")" \
    chunked-text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  run_mcp_curl_expect "mcp-curl-oauth-valid" "${MCP_OAUTH_VALID_URL}" true
fi

if scenario_selected "observability"; then
  echo "[observe] validating direct Sentinel service routes"
  SENTINEL_GATEWAY_BASE="http://127.0.0.1:${SENTINEL_PORT}" \
  SENTINEL_API_BASE="http://127.0.0.1:${API_SERVICE_PORT}" \
  SENTINEL_API_METRICS_URL="http://127.0.0.1:${API_METRICS_PORT}/metrics" \
  SENTINEL_INGEST_BASE="http://127.0.0.1:${INGEST_SERVICE_PORT}" \
  SENTINEL_INGEST_METRICS_URL="http://127.0.0.1:${INGEST_METRICS_PORT}/metrics" \
  SENTINEL_PROCESSOR_BASE="http://127.0.0.1:${PROCESSOR_METRICS_PORT}" \
  SENTINEL_UI_BASE="http://127.0.0.1:${UI_SERVICE_PORT}" \
  SERVER_PROXY_BASE="http://127.0.0.1:${SERVER_PROXY_PORT}" \
  SERVER_UPSTREAM_BASE="http://127.0.0.1:${SERVER_UPSTREAM_PORT}" \
  OAUTH_PROXY_BASE="http://127.0.0.1:${OAUTH_PROXY_PORT}" \
  OAUTH_UPSTREAM_BASE="http://127.0.0.1:${OAUTH_UPSTREAM_PORT}" \
  API_KEY="${API_KEY}" \
  INGEST_API_KEY="${INGEST_API_KEY}" \
  SERVER_NAME="${SERVER_NAME}" \
  SERVER_HOST="${SERVER_HOST}" \
  SESSION_ID="${SESSION_ID}" \
  HUMAN_ID="${HUMAN_ID}" \
  AGENT_ID="${AGENT_ID}" \
  OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME}" \
  OAUTH_SERVER_HOST="${OAUTH_SERVER_HOST}" \
  OAUTH_ISSUER_URL="${OAUTH_ISSUER_URL}" \
  OAUTH_VALID_TOKEN="${OAUTH_VALID_TOKEN}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  E2E_PLATFORM_MODE="${E2E_PLATFORM_MODE}" \
  python3 <<'PY'
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request

gateway_base = os.environ["SENTINEL_GATEWAY_BASE"]
api_base = os.environ["SENTINEL_API_BASE"]
api_metrics_url = os.environ["SENTINEL_API_METRICS_URL"]
ingest_base = os.environ["SENTINEL_INGEST_BASE"]
ingest_metrics_url = os.environ["SENTINEL_INGEST_METRICS_URL"]
processor_base = os.environ["SENTINEL_PROCESSOR_BASE"]
ui_base = os.environ["SENTINEL_UI_BASE"]
server_proxy_base = os.environ["SERVER_PROXY_BASE"]
server_upstream_base = os.environ["SERVER_UPSTREAM_BASE"]
oauth_proxy_base = os.environ["OAUTH_PROXY_BASE"]
oauth_upstream_base = os.environ["OAUTH_UPSTREAM_BASE"]
api_key = os.environ["API_KEY"]
ingest_api_key = os.environ["INGEST_API_KEY"]
server_name = os.environ["SERVER_NAME"]
server_host = os.environ["SERVER_HOST"]
session_id = os.environ["SESSION_ID"]
human_id = os.environ["HUMAN_ID"]
agent_id = os.environ["AGENT_ID"]
oauth_server_name = os.environ["OAUTH_SERVER_NAME"]
oauth_server_host = os.environ["OAUTH_SERVER_HOST"]
oauth_issuer_url = os.environ["OAUTH_ISSUER_URL"]
oauth_valid_token = os.environ["OAUTH_VALID_TOKEN"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]
platform_mode = os.environ["E2E_PLATFORM_MODE"]
grant_name = f"{server_name}-grant"
oauth_public_base = f"http://{oauth_server_host}"
server_mcp_path = f"/{server_name}/mcp"
oauth_mcp_path = f"/{oauth_server_name}/mcp"
catalog_namespace = "mcp-servers"


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


def request(url, *, method="GET", headers=None, body=None):
    headers = dict(headers or {})
    data = None
    if body is not None:
        if isinstance(body, (bytes, bytearray)):
            data = bytes(body)
        else:
            data = json.dumps(body).encode()
            headers.setdefault("content-type", "application/json")
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, dict(resp.headers.items()), resp.read().decode()
    except urllib.error.HTTPError as exc:
        return exc.code, dict(exc.headers.items()), exc.read().decode()


def expect_status(url, status, *, method="GET", headers=None, body=None, contains=None):
    got_status, _, got_body = request(url, method=method, headers=headers, body=body)
    check(
        got_status == status,
        f"{method} {url} returned {status}",
        f"{method} {url} returned {got_status}: {got_body}",
    )
    if contains:
        check(
            contains in got_body,
            f"{method} {url} contained {contains!r}",
            f"{method} {url} missing {contains!r}: {got_body}",
        )
    return got_body


def expect_json(url, status=200, *, method="GET", headers=None, body=None):
    payload = expect_status(url, status, method=method, headers=headers, body=body)
    return json.loads(payload)


def wait_for_json(url, predicate, *, headers=None, retries=60, delay=2, description="response"):
    last = None
    for _ in range(retries):
        last = expect_json(url, headers=headers)
        if predicate(last):
            ok(f"waited for {description}")
            return last
        time.sleep(delay)
    fail(f"timed out waiting for {description}: {json.dumps(last, indent=2)}")


def expect_mcp_initialize(url, *, headers=None, status=200, contains=None):
    req_headers = {
        "accept": "application/json, text/event-stream",
        "content-type": "application/json",
        "Mcp-Protocol-Version": protocol,
    }
    req_headers.update(headers or {})
    got_status, got_headers, got_body = request(
        url,
        method="POST",
        headers=req_headers,
        body={
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": protocol,
                "capabilities": {},
                "clientInfo": {"name": "mcp-runtime-e2e", "version": "1.0.0"},
            },
        },
    )
    check(
        got_status == status,
        f"POST {url} initialize returned {status}",
        f"POST {url} initialize returned {got_status}: {got_body}",
    )
    if contains:
        check(
            contains in got_body,
            f"POST {url} initialize contained {contains!r}",
            f"POST {url} initialize missing {contains!r}: {got_body}",
        )
    if got_status == 200:
        doc = json.loads(got_body)
        check(
            "result" in doc,
            f"POST {url} initialize returned result",
            f"POST {url} initialize missing result: {doc}",
        )
        header_map = {k.lower(): v for k, v in got_headers.items()}
        check(
            "mcp-session-id" in header_map,
            f"POST {url} initialize returned Mcp-Session-Id",
            f"POST {url} initialize missing Mcp-Session-Id: {got_headers}",
        )
    return got_body


auth_headers = {"x-api-key": api_key}
ingest_headers = {"x-api-key": ingest_api_key}

# Gateway-routed UI, API, and example MCP routes.
gateway_summary = expect_json(f"{gateway_base}/api/dashboard/summary", headers=auth_headers)
for key in ("total_events", "active_servers", "active_grants", "active_sessions"):
    check(
        key in gateway_summary,
        f"gateway dashboard summary contains {key}",
        f"gateway dashboard summary missing {key}: {gateway_summary}",
    )
expect_status(f"{gateway_base}/ping", 200, contains="OK")
expect_status(f"{gateway_base}/", 200, contains="MCP Sentinel Control Plane")
gateway_config = expect_status(f"{gateway_base}/config.js", 200, contains="window.MCP_API_BASE")
check(
    f'window.MCP_PLATFORM_MODE = "{platform_mode}"' in gateway_config,
    f"gateway config.js exposes platform mode {platform_mode}",
    f"gateway config.js missing platform mode {platform_mode}: {gateway_config}",
)
expect_status(f"{gateway_base}/app.js", 200, contains="const apiBase")
expect_status(f"{gateway_base}/styles.css", 200, contains=".canvas")
expect_status(f"{gateway_base}/grafana/api/health", 200, contains="database")
expect_status(f"{gateway_base}/prometheus/-/healthy", 200, contains="Healthy")

# Direct UI service.
expect_status(f"{ui_base}/health", 200, contains='"ok":true')
expect_status(f"{ui_base}/", 200, contains="MCP Sentinel Control Plane")
ui_config = expect_status(f"{ui_base}/config.js", 200, contains="window.MCP_API_BASE")
check(
    f'window.MCP_PLATFORM_MODE = "{platform_mode}"' in ui_config,
    f"ui config.js exposes platform mode {platform_mode}",
    f"ui config.js missing platform mode {platform_mode}: {ui_config}",
)
expect_status(f"{ui_base}/app.js", 200, contains="const apiBase")
expect_status(f"{ui_base}/styles.css", 200, contains=".canvas")

# Direct MCP proxy and upstream server surfaces.
expect_status(f"{server_proxy_base}/health", 200, contains="ok")
expect_mcp_initialize(
    f"{server_proxy_base}{server_mcp_path}",
    headers={
        "X-MCP-Human-ID": human_id,
        "X-MCP-Agent-ID": agent_id,
        "X-MCP-Agent-Session": session_id,
    },
)
expect_status(f"{server_upstream_base}/health", 200, contains='"ok":true')
expect_mcp_initialize(f"{server_upstream_base}{server_mcp_path}")

expect_status(f"{oauth_proxy_base}/health", 200, contains="ok")
oauth_metadata = expect_json(f"{oauth_proxy_base}/.well-known/oauth-protected-resource")
check(
    oauth_metadata.get("authorization_servers") == [oauth_issuer_url],
    "oauth proxy metadata authorization_servers matched issuer",
    f"unexpected oauth metadata authorization servers: {oauth_metadata}",
)
check(
    oauth_metadata.get("bearer_methods_supported") == ["header"],
    "oauth proxy metadata bearer_methods_supported matched",
    f"unexpected oauth metadata bearer methods: {oauth_metadata}",
)
oauth_resource_url = oauth_metadata.get("resource", "")
oauth_resource_path = urllib.parse.urlsplit(oauth_resource_url).path or "/"
check(
    oauth_resource_path == "/",
    "oauth proxy metadata root resource path matched",
    f"unexpected oauth metadata resource URL: {oauth_metadata}",
)
oauth_metadata_path = expect_json(
    f"{oauth_proxy_base}/.well-known/oauth-protected-resource/{oauth_server_name}/mcp"
)
oauth_resource_path_url = oauth_metadata_path.get("resource", "")
oauth_resource_path_value = urllib.parse.urlsplit(oauth_resource_path_url).path
check(
    oauth_resource_path_value == f"/{oauth_server_name}/mcp",
    "oauth proxy metadata path resource matched",
    f"unexpected oauth metadata path resource URL: {oauth_metadata_path}",
)
expect_mcp_initialize(
    f"{oauth_proxy_base}{oauth_mcp_path}",
    headers={"Authorization": f"Bearer {oauth_valid_token}"},
)
expect_status(f"{oauth_upstream_base}/health", 200, contains='"ok":true')
expect_mcp_initialize(f"{oauth_upstream_base}{oauth_mcp_path}")

# API service surfaces.
expect_status(f"{api_base}/health", 200, contains='"ok":true')
expect_status(api_metrics_url, 200, contains="# HELP")
events = expect_json(f"{api_base}/api/events?limit=5", headers=auth_headers)
check(
    bool(events.get("events")),
    "api /api/events returned events",
    f"expected /api/events to return events: {events}",
)
stats = expect_json(f"{api_base}/api/stats", headers=auth_headers)
check(
    int(stats.get("events_total", 0)) >= 1,
    "api /api/stats events_total >= 1",
    f"expected /api/stats events_total >= 1: {stats}",
)
sources = expect_json(f"{api_base}/api/sources", headers=auth_headers)
check(
    bool(sources.get("sources")),
    "api /api/sources returned sources",
    f"expected /api/sources to return sources: {sources}",
)
event_types = expect_json(f"{api_base}/api/event-types", headers=auth_headers)
check(
    bool(event_types.get("event_types")),
    "api /api/event-types returned event types",
    f"expected /api/event-types to return event types: {event_types}",
)
filtered = wait_for_json(
    f"{api_base}/api/events/filter?server={urllib.parse.quote(server_name)}&limit=5",
    lambda doc: bool(doc.get("events")),
    headers=auth_headers,
    description="api /api/events/filter events",
)
check(
    bool(filtered.get("events")),
    "api /api/events/filter returned events",
    f"expected /api/events/filter to return events: {filtered}",
)
summary = expect_json(f"{api_base}/api/dashboard/summary", headers=auth_headers)
for key in ("total_events", "active_servers", "active_grants", "active_sessions"):
    check(
        key in summary,
        f"api dashboard summary contains {key}",
        f"dashboard summary missing {key}: {summary}",
    )
servers = expect_json(
    f"{api_base}/api/runtime/servers?namespace={urllib.parse.quote(catalog_namespace)}",
    headers=auth_headers,
)
server_names = {item.get("name") for item in servers.get("servers", [])}
check(
    server_name in server_names and oauth_server_name in server_names,
    "runtime servers contain expected entries",
    f"runtime servers missing expected entries: {servers}",
)
grants = expect_json(f"{api_base}/api/runtime/grants", headers=auth_headers)
grant_names = {item.get("name") for item in grants.get("grants", [])}
check(
    grant_name in grant_names,
    f"runtime grants contain {grant_name}",
    f"runtime grants missing {grant_name}: {grants}",
)
sessions = expect_json(f"{api_base}/api/runtime/sessions", headers=auth_headers)
session_names = {item.get("name") for item in sessions.get("sessions", [])}
check(
    session_id in session_names,
    f"runtime sessions contain {session_id}",
    f"runtime sessions missing {session_id}: {sessions}",
)
not_a_server = f"{server_name}-e2e-not-mcpserver"
bad_grant_body = expect_status(
    f"{api_base}/api/runtime/grants",
    400,
    method="POST",
    headers=auth_headers,
    body={
        "name": f"{server_name}-e2e-bad-grant",
        "namespace": "mcp-servers",
        "serverRef": {"name": not_a_server, "namespace": "mcp-servers"},
        "subject": {"humanID": human_id, "agentID": agent_id},
        "maxTrust": "low",
        "toolRules": [{"name": "add", "decision": "allow", "requiredTrust": "low"}],
    },
)
check(
    "unknown serverRef" in bad_grant_body,
    "POST /api/runtime/grants rejects unknown serverRef",
    f"body: {bad_grant_body}",
)
bad_session_body = expect_status(
    f"{api_base}/api/runtime/sessions",
    400,
    method="POST",
    headers=auth_headers,
    body={
        "name": f"{server_name}-e2e-bad-session",
        "namespace": "mcp-servers",
        "serverRef": {"name": not_a_server, "namespace": "mcp-servers"},
        "subject": {"humanID": human_id, "agentID": agent_id},
        "consentedTrust": "low",
    },
)
check(
    "unknown serverRef" in bad_session_body,
    "POST /api/runtime/sessions rejects unknown serverRef",
    f"body: {bad_session_body}",
)
api_runtime_grant = f"{server_name}-e2e-api-grant"
api_runtime_session = f"{server_name}-e2e-api-session"
created_grant = expect_json(
    f"{api_base}/api/runtime/grants",
    method="POST",
    headers=auth_headers,
    body={
        "name": api_runtime_grant,
        "namespace": "mcp-servers",
        "serverRef": {"name": server_name, "namespace": "mcp-servers"},
        "subject": {"humanID": human_id, "agentID": agent_id},
        "maxTrust": "low",
        "toolRules": [{"name": "add", "decision": "allow", "requiredTrust": "low"}],
    },
)
check(
    created_grant.get("grant", {}).get("name") == api_runtime_grant,
    "POST /api/runtime/grants created grant",
    f"body: {created_grant}",
)
created_session = expect_json(
    f"{api_base}/api/runtime/sessions",
    method="POST",
    headers=auth_headers,
    body={
        "name": api_runtime_session,
        "namespace": "mcp-servers",
        "serverRef": {"name": server_name, "namespace": "mcp-servers"},
        "subject": {"humanID": human_id, "agentID": agent_id},
        "consentedTrust": "low",
    },
)
check(
    created_session.get("session", {}).get("name") == api_runtime_session,
    "POST /api/runtime/sessions created session",
    f"body: {created_session}",
)
grants_after = expect_json(f"{api_base}/api/runtime/grants", headers=auth_headers)
grant_names_after = {item.get("name") for item in grants_after.get("grants", [])}
check(
    api_runtime_grant in grant_names_after,
    "list grants after API create",
    f"missing {api_runtime_grant}: {grants_after}",
)
sessions_after = expect_json(f"{api_base}/api/runtime/sessions", headers=auth_headers)
session_names_after = {item.get("name") for item in sessions_after.get("sessions", [])}
check(
    api_runtime_session in session_names_after,
    "list sessions after API create",
    f"missing {api_runtime_session}: {sessions_after}",
)
components = expect_json(f"{api_base}/api/runtime/components", headers=auth_headers)
component_keys = {item.get("key") for item in components.get("components", [])}
check(
    {"api", "gateway", "ui"}.issubset(component_keys),
    "runtime components contain api/gateway/ui",
    f"runtime components missing expected keys: {components}",
)
policy = expect_json(
    f"{api_base}/api/runtime/policy?namespace=mcp-servers&server={urllib.parse.quote(server_name)}",
    headers=auth_headers,
)
check(
    policy.get("server", {}).get("name") == server_name,
    f"runtime policy resolved server {server_name}",
    f"runtime policy missing server {server_name}: {policy}",
)

# Runtime mutation paths through the API.
disable = expect_json(
    f"{api_base}/api/runtime/grants/mcp-servers/{urllib.parse.quote(grant_name)}/disable",
    method="POST",
    headers=auth_headers,
)
check(
    disable.get("disabled") is True,
    "grant disable response marked disabled=true",
    f"grant disable response unexpected: {disable}",
)
enable = expect_json(
    f"{api_base}/api/runtime/grants/mcp-servers/{urllib.parse.quote(grant_name)}/enable",
    method="POST",
    headers=auth_headers,
)
check(
    enable.get("disabled") is False,
    "grant enable response marked disabled=false",
    f"grant enable response unexpected: {enable}",
)
revoke = expect_json(
    f"{api_base}/api/runtime/sessions/mcp-servers/{urllib.parse.quote(session_id)}/revoke",
    method="POST",
    headers=auth_headers,
)
check(
    revoke.get("revoked") is True,
    "session revoke response marked revoked=true",
    f"session revoke response unexpected: {revoke}",
)
unrevoke = expect_json(
    f"{api_base}/api/runtime/sessions/mcp-servers/{urllib.parse.quote(session_id)}/unrevoke",
    method="POST",
    headers=auth_headers,
)
check(
    unrevoke.get("revoked") is False,
    "session unrevoke response marked revoked=false",
    f"session unrevoke response unexpected: {unrevoke}",
)
expect_json(
    f"{api_base}/api/runtime/actions/restart",
    status=400,
    method="POST",
    headers=auth_headers,
    body={"component": "definitely-not-a-real-component"},
)

# Ingest and processor service surfaces.
expect_status(f"{ingest_base}/health", 200, contains='"ok":true')
expect_status(f"{ingest_base}/live", 200, contains='"ok":true')
expect_status(f"{ingest_base}/ready", 200, contains='"ok":true')
expect_status(ingest_metrics_url, 200, contains="# HELP")
ingest_event = expect_json(
    f"{ingest_base}/events",
    status=202,
    method="POST",
    headers=ingest_headers,
    body={
        "timestamp": "2026-03-29T00:00:00Z",
        "source": "e2e-direct-ingest",
        "event_type": "service.route.check",
        "payload": {"service": "ingest", "route": "/events"},
    },
)
check(
    ingest_event.get("ok") is True,
    "ingest /events returned ok=true",
    f"ingest /events response unexpected: {ingest_event}",
)
expect_status(f"{processor_base}/health", 200, contains="ok")
expect_status(f"{processor_base}/metrics", 200, contains="# HELP")

print("service routes:")
for route in (
    "gateway:/",
    "gateway:/api/dashboard/summary",
    "gateway:/ping",
    "gateway:/config.js",
    "gateway:/app.js",
    "gateway:/styles.css",
    "gateway:/grafana/api/health",
    "gateway:/prometheus/-/healthy",
    "ingress:{server-host}:/{server}/mcp",
    "ingress:{oauth-host}:/{oauth-server}/mcp",
    "ingress:{oauth-host}:/.well-known/oauth-protected-resource/{oauth-server}/mcp",
    "ui:/health",
    "ui:/",
    "ui:/config.js",
    "ui:/app.js",
    "ui:/styles.css",
    "mcp-proxy:/health",
    "mcp-proxy:/",
    "mcp-server:/health",
    "mcp-server:/",
    "oauth-proxy:/health",
    "oauth-proxy:/",
    "oauth-proxy:/.well-known/oauth-protected-resource",
    "oauth-proxy:/.well-known/oauth-protected-resource/{server}/mcp",
    "oauth-server:/health",
    "oauth-server:/",
    "api:/health",
    "api:/metrics",
    "api:/api/events",
    "api:/api/stats",
    "api:/api/sources",
    "api:/api/event-types",
    "api:/api/events/filter",
    "api:/api/dashboard/summary",
    "api:/api/runtime/servers",
    "api:/api/runtime/grants",
    "api:/api/runtime/sessions",
    "api:/api/runtime/components",
    "api:/api/runtime/policy",
    "api:/api/runtime/grants/{namespace}/{name}/disable",
    "api:/api/runtime/grants/{namespace}/{name}/enable",
    "api:/api/runtime/sessions/{namespace}/{name}/revoke",
    "api:/api/runtime/sessions/{namespace}/{name}/unrevoke",
    "api:/api/runtime/actions/restart",
    "ingest:/health",
    "ingest:/live",
    "ingest:/ready",
    "ingest:/events",
    "ingest:/metrics",
    "processor:/health",
    "processor:/metrics",
):
    print(f"  {route}")
PY

  echo "[observe] validating audit, traces, and logs"
  API_BASE="http://127.0.0.1:${SENTINEL_PORT}/api" \
  API_KEY="${API_KEY}" \
  INGEST_API_KEY="${INGEST_API_KEY}" \
  SERVER_NAME="${SERVER_NAME}" \
  OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME}" \
  OAUTH_HUMAN_ID="${OAUTH_HUMAN_ID}" \
  OAUTH_AGENT_ID="${OAUTH_AGENT_ID}" \
  SENTINEL_BASE="http://127.0.0.1:${SENTINEL_PORT}" \
  TEMPO_BASE="http://127.0.0.1:${TEMPO_PORT}" \
  GRAFANA_BASE="http://127.0.0.1:${SENTINEL_PORT}/grafana" \
  PROMETHEUS_BASE="http://127.0.0.1:${SENTINEL_PORT}/prometheus" \
  GRAFANA_ADMIN_USER="${GRAFANA_ADMIN_USER}" \
  GRAFANA_ADMIN_PASSWORD="${GRAFANA_ADMIN_PASSWORD}" \
  LOKI_BASE="http://127.0.0.1:${LOKI_PORT}" \
  python3 <<'PY'
import base64
import json
import os
import time
import urllib.parse
import urllib.request

api_base = os.environ["API_BASE"]
api_key = os.environ["API_KEY"]
ingest_api_key = os.environ["INGEST_API_KEY"]
server_name = os.environ["SERVER_NAME"]
oauth_server_name = os.environ["OAUTH_SERVER_NAME"]
oauth_human_id = os.environ["OAUTH_HUMAN_ID"]
oauth_agent_id = os.environ["OAUTH_AGENT_ID"]
tempo_base = os.environ["TEMPO_BASE"]
grafana_base = os.environ["GRAFANA_BASE"]
prometheus_base = os.environ["PROMETHEUS_BASE"]
grafana_user = os.environ["GRAFANA_ADMIN_USER"]
grafana_password = os.environ["GRAFANA_ADMIN_PASSWORD"]
loki_base = os.environ["LOKI_BASE"]
sentinel_base = os.environ["SENTINEL_BASE"]
gateway_trace_services = (f"{server_name}-gateway", f"{oauth_server_name}-gateway")


import os as _os; exec(open(_os.environ["E2E_HELPERS"]).read())


def get_json(url, headers=None, retries=30, delay=2):
    last = None
    for _ in range(retries):
        try:
            req = urllib.request.Request(url, headers=headers or {})
            return json.loads(urllib.request.urlopen(req, timeout=10).read().decode())
        except Exception as exc:
            last = exc
            time.sleep(delay)
    raise last


def wait_for_json(url, predicate, *, headers=None, retries=60, delay=2, description="response"):
    last = None
    last_error = None
    for _ in range(retries):
        try:
            last = get_json(url, headers=headers, retries=1, delay=delay)
            if predicate(last):
                ok(f"waited for {description}")
                return last
        except Exception as exc:
            last_error = exc
        time.sleep(delay)
    if last is not None:
        fail(f"timed out waiting for {description}: {json.dumps(last, indent=2)}")
    if last_error is not None:
        raise last_error
    fail(f"timed out waiting for {description}")

def post_json(url, body, headers):
    data = json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=10) as resp:
        return resp.getcode(), resp.read().decode()

def basic_auth_headers(user, password):
    token = base64.b64encode(f"{user}:{password}".encode()).decode()
    return {"Authorization": f"Basic {token}"}

def payload_dict(event):
    payload = event.get("payload", {})
    return payload if isinstance(payload, dict) else {}

def otlp_attr_value(attrs, key):
    for attr in attrs or []:
        if attr.get("key") != key:
            continue
        value = attr.get("value", {})
        for field in ("stringValue", "intValue", "doubleValue", "boolValue"):
            if field in value:
                return str(value[field])
    return ""

def trace_resource_batches(trace_doc):
    batches = []
    batches.extend(trace_doc.get("batches", []) or [])
    batches.extend(trace_doc.get("resourceSpans", []) or [])
    return batches

def trace_service_names(trace_doc):
    names = set()
    for batch in trace_resource_batches(trace_doc):
        name = otlp_attr_value(batch.get("resource", {}).get("attributes", []), "service.name")
        if name:
            names.add(name)
    for trace in trace_doc.get("data", []):
        for process in trace.get("processes", {}).values():
            name = process.get("serviceName")
            if name:
                names.add(name)
    return names

def trace_span_names(trace_doc):
    names = set()
    for batch in trace_resource_batches(trace_doc):
        scope_spans = []
        scope_spans.extend(batch.get("scopeSpans", []) or [])
        scope_spans.extend(batch.get("instrumentationLibrarySpans", []) or [])
        for scope_span in scope_spans:
            for span in scope_span.get("spans", []) or []:
                name = span.get("name")
                if name:
                    names.add(name)
    for trace in trace_doc.get("data", []):
        for span in trace.get("spans", []) or []:
            name = span.get("operationName") or span.get("name")
            if name:
                names.add(name)
    return names

def datasource_by_type(datasources, ds_type):
    for datasource in datasources:
        if datasource.get("type") == ds_type:
            return datasource
    fail(f"Grafana datasource of type {ds_type!r} not found: {datasources}")

def datasource_uid(datasources, ds_type):
    datasource = datasource_by_type(datasources, ds_type)
    uid = datasource.get("uid")
    if uid:
        return uid
    fail(f"Grafana datasource of type {ds_type!r} has no uid: {datasource}")

def wait_for_tempo_service(base_url, service_name, *, headers=None, description):
    end = int(time.time())
    start = end - 3600
    tag = urllib.parse.quote(f"service.name={service_name}")
    doc = wait_for_json(
        f"{base_url}/api/search?limit=20&start={start}&end={end}&tags={tag}",
        lambda payload: bool(payload.get("traces", [])),
        headers=headers,
        retries=90,
        delay=2,
        description=description,
    )
    traces_for_service = doc.get("traces", [])
    trace_id = traces_for_service[0].get("traceID") if traces_for_service else ""
    check(
        bool(trace_id),
        f"{description} returned a trace id",
        f"{description} missing trace id: {doc}",
    )
    trace_doc = wait_for_json(
        f"{base_url}/api/traces/{urllib.parse.quote(trace_id)}",
        lambda payload: service_name in trace_service_names(payload),
        headers=headers,
        retries=30,
        delay=2,
        description=f"{description} payload",
    )
    names = trace_service_names(trace_doc)
    check(
        service_name in names,
        f"{description} payload includes service.name={service_name}",
        f"{description} payload missing {service_name}; services={names}",
    )
    return len(traces_for_service), names

def wait_for_tempo_trace_path(base_url, gateway_service, required_services, required_spans, *, headers=None, description):
    end = int(time.time())
    start = end - 3600
    tag = urllib.parse.quote(f"service.name={gateway_service}")
    search_url = f"{base_url}/api/search?limit=100&start={start}&end={end}&tags={tag}"
    last_summary = {}
    last_error = None
    for _ in range(90):
        try:
            search_doc = get_json(search_url, headers=headers, retries=1, delay=2)
            for item in search_doc.get("traces", []) or []:
                trace_id = item.get("traceID")
                if not trace_id:
                    continue
                trace_doc = get_json(
                    f"{base_url}/api/traces/{urllib.parse.quote(trace_id)}",
                    headers=headers,
                    retries=1,
                    delay=2,
                )
                services = trace_service_names(trace_doc)
                spans = trace_span_names(trace_doc)
                last_summary = {
                    "trace_id": trace_id,
                    "services": sorted(services),
                    "spans": sorted(spans),
                }
                if required_services <= services and required_spans <= spans:
                    ok(f"waited for {description}")
                    return trace_id, services, spans
        except Exception as exc:
            last_error = exc
        time.sleep(2)
    if last_summary:
        fail(f"timed out waiting for {description}: {json.dumps(last_summary, indent=2)}")
    if last_error is not None:
        raise last_error
    fail(f"timed out waiting for {description}")

def wait_for_prometheus_up(base_url, *, headers=None, description):
    doc = wait_for_json(
        f"{base_url}/api/v1/query?query={urllib.parse.quote('up')}",
        lambda payload: payload.get("status") == "success"
        and bool(payload.get("data", {}).get("result", [])),
        headers=headers,
        retries=60,
        delay=2,
        description=description,
    )
    jobs = {}
    for result in doc.get("data", {}).get("result", []):
        metric = result.get("metric", {})
        value = result.get("value", [])
        if len(value) >= 2 and metric.get("job"):
            jobs[metric["job"]] = str(value[1])
    for job in ("mcp-sentinel-api", "mcp-sentinel-ingest", "mcp-sentinel-processor", "clickhouse"):
        check(
            jobs.get(job) == "1",
            f"{description} reports {job}=1",
            f"{description} missing healthy {job}: {jobs}",
        )
    return jobs


expected_gateway_rpc_methods = (
    "initialize",
    "notifications/initialized",
    "tools/list",
    "prompts/list",
    "resources/list",
    "prompts/get",
    "resources/read",
    "tools/call",
)
expected_gateway_rpc_method_set = set(expected_gateway_rpc_methods)
# The full policy-server method set is checked immediately after the successful
# curl flow. By this final observability pass, later policy traffic can push
# those older prompt/resource events outside the newest-first 1000-event window.
expected_recent_gateway_rpc_methods = (
    "initialize",
    "notifications/initialized",
    "tools/list",
    "prompts/list",
    "resources/list",
    "tools/call",
)
expected_recent_gateway_rpc_method_set = set(expected_recent_gateway_rpc_methods)


def rpc_methods_from_events_doc(doc):
    return {
        payload.get("rpc_method")
        for payload in (
            payload_dict(event)
            for event in doc.get("events", [])
        )
        if payload.get("rpc_method")
    }


headers = {"x-api-key": api_key}
ingest_headers = {"x-api-key": ingest_api_key}

# PII redaction check via the Sentinel gateway Traefik route.
pii_source = "pii-redaction-e2e"
pii_event_body = {
    "timestamp": "2026-03-29T00:00:00Z",
    "source": pii_source,
    "event_type": "pii.check",
    "payload": {
        "email": "alice@example.com",
        "phone": "+1-202-555-0188",
        "user_id": "123e4567-e89b-12d3-a456-426614174000",
        "secret": "tok-abcdef123",
    },
}
status, resp_body = post_json(
    f"{sentinel_base}/ingest/events",
    pii_event_body,
    {"content-type": "application/json", **ingest_headers},
)
check(
    status in (200, 202),
    "pii redaction ingest accepted event",
    f"pii redaction: unexpected status {status}, body={resp_body}",
)

pii_events = wait_for_json(
    f"{api_base}/events/filter?source={urllib.parse.quote(pii_source)}&event_type=pii.check&limit=1",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="pii redaction event",
).get("events", [])
pii_payload = payload_dict(pii_events[0])
serialized_pii = json.dumps(pii_payload)

check(
    "example.com" not in serialized_pii and "202-555" not in serialized_pii and "tok-abcdef" not in serialized_pii,
    "pii redaction removed raw PII from payload",
    f"pii redaction failed: found raw PII in payload {serialized_pii}",
)
check(
    pii_payload.get("email") == "[redacted]",
    "pii redaction masked email",
    f"pii redaction failed for email: {pii_payload}",
)
check(
    pii_payload.get("phone") == "[redacted]",
    "pii redaction masked phone",
    f"pii redaction failed for phone: {pii_payload}",
)
check(
    str(pii_payload.get("user_id", "")).startswith("hash:"),
    "pii redaction hashed uuid",
    f"pii redaction failed to hash uuid: {pii_payload}",
)
check(
    pii_payload.get("secret") == "[redacted]",
    "pii redaction masked secret",
    f"pii redaction failed for secret: {pii_payload}",
)


allow_aaa_ping = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=allow&tool_name=aaa-ping&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="allow audit event for aaa-ping",
).get("events", [])
allow_echo = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=allow&tool_name=echo&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="allow audit event for echo",
).get("events", [])
deny_upper = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=deny&tool_name=upper&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="deny audit event for upper",
).get("events", [])
deny_echo = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=deny&tool_name=echo&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="deny audit event for echo",
).get("events", [])
deny_aaa_ping = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=deny&tool_name=aaa-ping&limit=50",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="deny audit event for aaa-ping",
).get("events", [])
oauth_allow_aaa_ping = wait_for_json(
    f"{api_base}/events/filter?server={oauth_server_name}&decision=allow&tool_name=aaa-ping&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="oauth allow audit event for aaa-ping",
).get("events", [])
oauth_deny_events = wait_for_json(
    f"{api_base}/events/filter?server={oauth_server_name}&decision=deny&limit=50",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="oauth deny audit events",
).get("events", [])
all_oauth_events = wait_for_json(
    f"{api_base}/events/filter?server={oauth_server_name}&limit=1000",
    lambda doc: rpc_methods_from_events_doc(doc) >= expected_gateway_rpc_method_set,
    headers=headers,
    description="oauth server audit events",
).get("events", [])
allow_upper = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=allow&tool_name=upper&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="allow audit event for upper",
).get("events", [])
all_server_denies = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=deny&limit=250",
    lambda doc: {
        payload.get("reason")
        for payload in (
            event.get("payload", {})
            for event in doc.get("events", [])
            if isinstance(event.get("payload"), dict)
        )
        if payload.get("reason")
    } >= {
        "missing_identity",
        "missing_session",
        "session_not_found",
        "session_revoked",
        "session_expired",
        "rpc_inspection_failed",
        "trust_too_low",
        "tool_not_granted",
        "tool_denied",
    },
    headers=headers,
    description="server deny audit events",
).get("events", [])
all_server_events = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&limit=1000",
    lambda doc: rpc_methods_from_events_doc(doc) >= expected_recent_gateway_rpc_method_set,
    headers=headers,
    description="server audit events",
).get("events", [])
sources = wait_for_json(
    f"{api_base}/sources",
    lambda doc: all(
        int(item.get("count", 0)) >= 1
        for item in doc.get("sources", [])
        if item.get("source") in {server_name, oauth_server_name}
    ) and {item.get("source") for item in doc.get("sources", [])} >= {server_name, oauth_server_name},
    headers=headers,
    description="analytics sources",
).get("sources", [])
event_types = wait_for_json(
    f"{api_base}/event-types",
    lambda doc: {item.get("event_type") for item in doc.get("event_types", [])} >= {"mcp.request", "pii.check", "service.route.check"},
    headers=headers,
    description="analytics event types",
).get("event_types", [])
stats = wait_for_json(
    f"{api_base}/stats",
    lambda doc: int(doc.get("events_total", 0)) >= 8,
    headers=headers,
    description="analytics stats",
)

routing_methods = {
    payload.get("rpc_method")
    for payload in (payload_dict(event) for event in all_server_events)
    if payload.get("rpc_method")
}
server_latencies = [
    payload.get("latency_ms")
    for payload in (payload_dict(event) for event in all_server_events)
    if payload.get("latency_ms") is not None
]
source_counts = {item.get("source"): int(item.get("count", 0)) for item in sources}
event_type_counts = {item.get("event_type"): int(item.get("count", 0)) for item in event_types}
deny_aaa_ping_reasons = {
    payload.get("reason")
    for payload in (payload_dict(event) for event in deny_aaa_ping)
    if payload.get("reason")
}
server_deny_reasons = {
    payload.get("reason")
    for payload in (payload_dict(event) for event in all_server_denies)
    if payload.get("decision") == "deny" and payload.get("reason")
}
oauth_deny_reasons = {
    payload.get("reason")
    for payload in (payload_dict(event) for event in oauth_deny_events)
    if payload.get("reason")
}
server_statuses = {
    int(payload.get("status"))
    for payload in (payload_dict(event) for event in all_server_events)
    if payload.get("status") is not None
}
oauth_routing_methods = {
    payload.get("rpc_method")
    for payload in (payload_dict(event) for event in all_oauth_events)
    if payload.get("rpc_method")
}

deny_payload = deny_upper[0].get("payload", {})
deny_echo_payload = deny_echo[0].get("payload", {})
allow_payload = allow_upper[0].get("payload", {})
oauth_allow_payload = oauth_allow_aaa_ping[0].get("payload", {})
check(
    deny_payload.get("reason") == "trust_too_low",
    "deny payload reason is trust_too_low",
    f"unexpected deny payload: {deny_payload}",
)
check(
    deny_payload.get("required_trust") == "medium",
    "deny payload required_trust is medium",
    f"expected required_trust=medium, got {deny_payload}",
)
check(
    deny_payload.get("effective_trust") == "low",
    "deny payload effective_trust is low",
    f"expected effective_trust=low, got {deny_payload}",
)
check(
    deny_echo_payload.get("reason") == "tool_denied",
    "deny echo payload reason is tool_denied",
    f"unexpected deny echo payload: {deny_echo_payload}",
)
check(
    allow_payload.get("effective_trust") == "medium",
    "allow payload effective_trust updated to medium",
    f"expected effective_trust=medium after update, got {allow_payload}",
)
for reason in (
    "missing_identity",
    "missing_session",
    "session_not_found",
    "session_revoked",
    "session_expired",
    "rpc_inspection_failed",
    "trust_too_low",
    "tool_not_granted",
    "tool_denied",
):
    check(
        reason in server_deny_reasons,
        f"server deny reasons include {reason}",
        f"missing server deny reason {reason}: {server_deny_reasons}",
    )
for reason in ("missing_bearer_token", "invalid_token"):
    check(
        reason in oauth_deny_reasons,
        f"oauth deny reasons include {reason}",
        f"missing oauth deny reason {reason}: {oauth_deny_reasons}",
    )
for rpc_method in expected_recent_gateway_rpc_methods:
    check(
        rpc_method in routing_methods,
        f"gateway audit events include {rpc_method}",
        f"missing gateway audit event for {rpc_method}: {routing_methods}",
    )
for rpc_method in expected_gateway_rpc_methods:
    check(
        rpc_method in oauth_routing_methods,
        f"oauth gateway audit events include {rpc_method}",
        f"missing oauth gateway audit event for {rpc_method}: {oauth_routing_methods}",
    )
check(
    source_counts.get(server_name, 0) >= 1,
    f"gateway source counts include {server_name}",
    f"missing gateway source counts for {server_name}: {source_counts}",
)
check(
    source_counts.get(oauth_server_name, 0) >= 1,
    f"gateway source counts include {oauth_server_name}",
    f"missing gateway source counts for {oauth_server_name}: {source_counts}",
)
for event_type in ("mcp.request", "pii.check", "service.route.check"):
    check(
        event_type_counts.get(event_type, 0) >= 1,
        f"analytics event types include {event_type}",
        f"missing analytics event type {event_type}: {event_type_counts}",
    )
for status in (200, 401, 403):
    check(
        status in server_statuses,
        f"server audit statuses include {status}",
        f"missing server audit status {status}: {server_statuses}",
    )
check(
    int(stats.get("events_total", 0)) >= 8,
    "analytics stats events_total >= 8",
    f"expected at least 8 events after smoke and policy checks, got {stats}",
)
check(
    bool(server_latencies),
    "server audit events include latency_ms values",
    f"expected latency_ms values in server audit payloads: {all_server_events[:3]}",
)
check(
    all(isinstance(latency, (int, float)) for latency in server_latencies),
    "server audit event latencies are numeric",
    f"unexpected latency payload types: {server_latencies}",
)
check(
    all(latency >= 0 for latency in server_latencies),
    "server audit event latencies are non-negative",
    f"unexpected negative latency values: {server_latencies}",
)
check(
    oauth_allow_payload.get("human_id") == oauth_human_id and oauth_allow_payload.get("agent_id") == oauth_agent_id,
    "oauth allow payload identity matched",
    f"unexpected oauth allow identity payload: {oauth_allow_payload}",
)

tempo = wait_for_json(
    f"{tempo_base}/api/search?limit=20",
    lambda doc: bool(doc.get("traces", [])),
    retries=60,
    delay=2,
    description="tempo traces",
)
traces = tempo.get("traces", [])

tempo_gateway_counts = {}
tempo_gateway_services = set()
for service_name in gateway_trace_services:
    count, names = wait_for_tempo_service(
        tempo_base,
        service_name,
        description=f"tempo traces for {service_name}",
    )
    tempo_gateway_counts[service_name] = count
    tempo_gateway_services.update(names)

required_trace_services = {gateway_trace_services[0], "mcp-sentinel-ingest", "mcp-sentinel-processor"}
required_trace_spans = {"kafka.produce", "kafka.consume", "clickhouse.insert_event"}
tempo_full_trace_id, tempo_full_trace_services, tempo_full_trace_spans = wait_for_tempo_trace_path(
    tempo_base,
    gateway_trace_services[0],
    required_trace_services,
    required_trace_spans,
    description="tempo full gateway analytics trace path",
)

grafana_headers = basic_auth_headers(grafana_user, grafana_password)
grafana_datasources = wait_for_json(
    f"{grafana_base}/api/datasources",
    lambda doc: isinstance(doc, list)
    and any(item.get("type") == "tempo" for item in doc)
    and any(item.get("type") == "prometheus" for item in doc),
    headers=grafana_headers,
    retries=60,
    delay=2,
    description="grafana datasources",
)
tempo_uid = datasource_uid(grafana_datasources, "tempo")
prometheus_uid = datasource_uid(grafana_datasources, "prometheus")
loki_uid = datasource_uid(grafana_datasources, "loki")
prometheus_datasource = datasource_by_type(grafana_datasources, "prometheus")
check(
    str(prometheus_datasource.get("url", "")).rstrip("/").endswith("/prometheus"),
    "grafana Prometheus datasource includes route prefix",
    f"grafana Prometheus datasource URL is missing /prometheus route prefix: {prometheus_datasource}",
)

grafana_gateway_counts = {}
grafana_gateway_services = set()
grafana_tempo_base = f"{grafana_base}/api/datasources/proxy/uid/{tempo_uid}"
for service_name in gateway_trace_services:
    count, names = wait_for_tempo_service(
        grafana_tempo_base,
        service_name,
        headers=grafana_headers,
        description=f"grafana tempo traces for {service_name}",
    )
    grafana_gateway_counts[service_name] = count
    grafana_gateway_services.update(names)

grafana_full_trace_id, grafana_full_trace_services, grafana_full_trace_spans = wait_for_tempo_trace_path(
    grafana_tempo_base,
    gateway_trace_services[0],
    required_trace_services,
    required_trace_spans,
    headers=grafana_headers,
    description="grafana tempo full gateway analytics trace path",
)

prometheus_jobs = wait_for_prometheus_up(
    prometheus_base,
    description="prometheus up query",
)
grafana_prometheus_jobs = wait_for_prometheus_up(
    f"{grafana_base}/api/datasources/proxy/uid/{prometheus_uid}",
    headers=grafana_headers,
    description="grafana prometheus up query",
)
grafana_loki_base = f"{grafana_base}/api/datasources/proxy/uid/{loki_uid}"

end_ns = int(time.time() * 1e9)
start_ns = end_ns - int(10 * 60 * 1e9)
params = urllib.parse.urlencode(
    {
        "query": '{namespace=~"mcp-servers|mcp-sentinel"}',
        "limit": "20",
        "start": str(start_ns),
        "end": str(end_ns),
    }
)
loki = wait_for_json(
    f"{loki_base}/loki/api/v1/query_range?{params}",
    lambda doc: bool(doc.get("data", {}).get("result", [])),
    retries=60,
    delay=2,
    description="loki log streams",
)
streams = loki.get("data", {}).get("result", [])
grafana_loki = wait_for_json(
    f"{grafana_loki_base}/loki/api/v1/query_range?{params}",
    lambda doc: bool(doc.get("data", {}).get("result", [])),
    headers=grafana_headers,
    retries=60,
    delay=2,
    description="grafana loki log streams",
)
grafana_streams = grafana_loki.get("data", {}).get("result", [])

rows = [
    ("audit.events_total", str(stats.get("events_total", "n/a"))),
    ("audit.server_events", str(len(all_server_events))),
    ("audit.allow_aaa_ping", str(len(allow_aaa_ping))),
    ("audit.allow_echo", str(len(allow_echo))),
    ("audit.deny_upper", str(len(deny_upper))),
    ("audit.deny_aaa_ping", str(len(deny_aaa_ping))),
    ("audit.deny_echo", str(len(deny_echo))),
    ("audit.allow_upper", str(len(allow_upper))),
    ("audit.oauth_allow_aaa_ping", str(len(oauth_allow_aaa_ping))),
    ("audit.oauth_deny_events", str(len(oauth_deny_events))),
    ("audit.rpc_methods", str(len(routing_methods))),
    ("analytics.source.gateway", str(source_counts.get(server_name, 0))),
    ("analytics.source.oauth", str(source_counts.get(oauth_server_name, 0))),
    ("analytics.type.mcp.request", str(event_type_counts.get("mcp.request", 0))),
    ("analytics.type.pii.check", str(event_type_counts.get("pii.check", 0))),
    ("analytics.type.service.route.check", str(event_type_counts.get("service.route.check", 0))),
    ("analytics.latency_samples", str(len(server_latencies))),
    ("analytics.latency_max_ms", str(max(server_latencies) if server_latencies else "n/a")),
    ("traces.tempo_found", str(len(traces))),
    ("traces.tempo_gateway", ",".join(f"{k}:{v}" for k, v in sorted(tempo_gateway_counts.items()))),
    ("traces.tempo_services", ",".join(sorted(tempo_gateway_services))),
    ("traces.tempo_full_path", tempo_full_trace_id),
    ("traces.tempo_full_path_services", ",".join(sorted(tempo_full_trace_services))),
    ("traces.tempo_full_path_spans", ",".join(sorted(tempo_full_trace_spans))),
    ("traces.grafana_gateway", ",".join(f"{k}:{v}" for k, v in sorted(grafana_gateway_counts.items()))),
    ("traces.grafana_services", ",".join(sorted(grafana_gateway_services))),
    ("traces.grafana_full_path", grafana_full_trace_id),
    ("traces.grafana_full_path_services", ",".join(sorted(grafana_full_trace_services))),
    ("traces.grafana_full_path_spans", ",".join(sorted(grafana_full_trace_spans))),
    ("grafana.datasources", str(len(grafana_datasources))),
    ("prometheus.jobs", ",".join(f"{k}:{v}" for k, v in sorted(prometheus_jobs.items()))),
    ("grafana.prometheus.jobs", ",".join(f"{k}:{v}" for k, v in sorted(grafana_prometheus_jobs.items()))),
    ("logs.loki_streams", str(len(streams))),
    ("grafana.loki_streams", str(len(grafana_streams))),
]
width = max(len(k) for k, _ in rows)
print(f"{'check':{width}}  value")
print("-" * (width + 8))
for key, value in rows:
    print(f"{key:{width}}  {value}")
PY

  tempo_log_errors="$(kubectl logs -n mcp-sentinel statefulset/tempo --since=20m 2>/dev/null | grep -E 'failed to poll or create index for tenant.*tenant=wal|invalid UUID length' || true)"
  if [[ -n "${tempo_log_errors}" ]]; then
    log_line error "tempo logged WAL blocklist parse errors; local block storage must not share the WAL parent path"
    printf '%s\n' "${tempo_log_errors}" | tail -n 20 >&2
    exit 1
  fi
fi

if scenario_selected "multitenancy"; then
  # Multi-tenancy isolation: deploy two gateway-enabled MCPServers in mcp-servers
  # that reuse the policy-mcp-server image already in the kind registry, grant
  # alice on tenant-a and bob on tenant-b, then assert the cross-tenant deny
  # matrix:
  #   - allowed tool on own tenant   -> 200
  #   - same-tenant disallowed tool  -> 403 (subject known, tool not in grant)
  #   - cross-tenant request         -> 401 (no session/grant for subject on that server)
  echo "[multitenancy] preparing two-tenant isolation probe"

  MT_NS="mcp-servers"
  MT_IMAGE_REPO="${SERVER_IMAGE%:*}"
  MT_IMAGE_TAG="${SERVER_IMAGE##*:}"

  mt_apply_tenant() {
    local name="$1" prefix="$2"
    cat <<EOF | kubectl apply -f -
apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: ${name}
  namespace: ${MT_NS}
spec:
  description: Multi-tenancy verification tenant ${name}.
  image: ${MT_IMAGE_REPO}
  imageTag: ${MT_IMAGE_TAG}
  port: 8088
  servicePort: 80
  publicPathPrefix: ${prefix}
  ingressPath: /${prefix}/mcp
  resources:
    requests:
      cpu: 1m
      memory: 32Mi
  rollout:
    maxUnavailable: "1"
    maxSurge: "0"
  envVars:
    - name: MCP_PATH
      value: /${prefix}/mcp
  tools:
    - name: add
      description: Add two numbers.
      requiredTrust: low
      sideEffect: read
    - name: upper
      description: Uppercase the provided message.
      requiredTrust: medium
      sideEffect: read
  auth:
    mode: header
    humanIDHeader: X-MCP-Human-ID
    agentIDHeader: X-MCP-Agent-ID
    sessionIDHeader: X-MCP-Agent-Session
  policy:
    mode: allow-list
    defaultDecision: deny
    policyVersion: v1
  session:
    required: true
  gateway:
    enabled: true
    resources:
      requests:
        cpu: 1m
        memory: 32Mi
EOF
  }

  mt_apply_tenant "${MT_TENANT_A}" "${MT_TENANT_A}"
  mt_apply_tenant "${MT_TENANT_B}" "${MT_TENANT_B}"

  echo "[multitenancy] waiting for tenant rollouts"
  wait_for_deployment_exists "${MT_NS}" "${MT_TENANT_A}"
  wait_for_deployment_exists "${MT_NS}" "${MT_TENANT_B}"
  kubectl rollout status "deploy/${MT_TENANT_A}" -n "${MT_NS}" --timeout=180s
  kubectl rollout status "deploy/${MT_TENANT_B}" -n "${MT_NS}" --timeout=180s
  wait_for_named_server_ready "${MT_TENANT_A}" "${MT_NS}" 60
  wait_for_named_server_ready "${MT_TENANT_B}" "${MT_NS}" 60

  echo "[multitenancy] applying alice (tenant-a / add) and bob (tenant-b / upper) grants and sessions"
  cat <<EOF | kubectl apply -f -
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata: {name: alice-${MT_TENANT_A}, namespace: ${MT_NS}}
spec:
  serverRef: {name: ${MT_TENANT_A}}
  subject:   {humanID: ${MT_HUMAN_A}, agentID: ${MT_AGENT_A}}
  maxTrust: high
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - {name: add, decision: allow, requiredTrust: low}
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata: {name: ${MT_SESSION_A}, namespace: ${MT_NS}}
spec:
  serverRef: {name: ${MT_TENANT_A}}
  subject:   {humanID: ${MT_HUMAN_A}, agentID: ${MT_AGENT_A}}
  consentedTrust: high
  policyVersion: v1
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata: {name: bob-${MT_TENANT_B}, namespace: ${MT_NS}}
spec:
  serverRef: {name: ${MT_TENANT_B}}
  subject:   {humanID: ${MT_HUMAN_B}, agentID: ${MT_AGENT_B}}
  maxTrust: high
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - {name: upper, decision: allow, requiredTrust: medium}
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata: {name: ${MT_SESSION_B}, namespace: ${MT_NS}}
spec:
  serverRef: {name: ${MT_TENANT_B}}
  subject:   {humanID: ${MT_HUMAN_B}, agentID: ${MT_AGENT_B}}
  consentedTrust: high
  policyVersion: v1
EOF

  echo "[multitenancy] waiting for per-tenant policy materialization"
  mt_wait_for_session_in_policy() {
    local server="$1" session="$2" tries=60 i policy_json
    for i in $(seq 1 "${tries}"); do
      policy_json="$(kubectl get configmap "${server}-gateway-policy" -n "${MT_NS}" -o "jsonpath={.data.policy\.json}" 2>/dev/null || true)"
      if [[ -n "${policy_json}" ]] && printf '%s' "${policy_json}" | grep -q "\"name\": \"${session}\""; then
        return 0
      fi
      sleep 2
    done
    echo "[multitenancy] timed out waiting for session ${session} in ${server}-gateway-policy" >&2
    kubectl get configmap "${server}-gateway-policy" -n "${MT_NS}" -o yaml || true
    return 1
  }
  mt_wait_for_session_in_policy "${MT_TENANT_A}" "${MT_SESSION_A}"
  mt_wait_for_session_in_policy "${MT_TENANT_B}" "${MT_SESSION_B}"
  # The sidecar reloads from a ConfigMap volume, which can lag behind the
  # rendered ConfigMap. The matrix below retries each expected decision until
  # the sidecar observes the updated policy.

  echo "[multitenancy] running cross-tenant deny matrix"
  MT_BASE_A="http://localhost:${TRAEFIK_PORT}/${MT_TENANT_A}/mcp"
  MT_BASE_B="http://localhost:${TRAEFIK_PORT}/${MT_TENANT_B}/mcp"
  MT_REPORT="${WORKDIR}/multitenancy-matrix.json"

  MT_BASE_A="${MT_BASE_A}" \
  MT_BASE_B="${MT_BASE_B}" \
  MT_PROTO="${MCP_PROTOCOL_VERSION}" \
  MT_HUMAN_A="${MT_HUMAN_A}" MT_AGENT_A="${MT_AGENT_A}" MT_SESSION_A="${MT_SESSION_A}" \
  MT_HUMAN_B="${MT_HUMAN_B}" MT_AGENT_B="${MT_AGENT_B}" MT_SESSION_B="${MT_SESSION_B}" \
  MT_REPORT="${MT_REPORT}" \
  python3 <<'PY'
import json, os, subprocess, sys, tempfile, time

PROTO = os.environ["MT_PROTO"]
A = os.environ["MT_BASE_A"]
B = os.environ["MT_BASE_B"]
report = []

def post(base, body, human, agent, sess, mcp_session=""):
    with tempfile.TemporaryDirectory() as td:
        payload = os.path.join(td, "p.json"); headers = os.path.join(td, "h.txt"); bodyf = os.path.join(td, "b.txt")
        with open(payload, "w") as fh: json.dump(body, fh)
        cmd = [
            "curl", "-sS", "--max-time", "20", "-D", headers, "-o", bodyf, "-w", "%{http_code}",
            "-X", "POST",
            "-H", "content-type: application/json",
            "-H", "accept: application/json, text/event-stream",
            "-H", f"Mcp-Protocol-Version: {PROTO}",
        ]
        if human:   cmd += ["-H", f"X-MCP-Human-ID: {human}"]
        if agent:   cmd += ["-H", f"X-MCP-Agent-ID: {agent}"]
        if sess:    cmd += ["-H", f"X-MCP-Agent-Session: {sess}"]
        if mcp_session: cmd += ["-H", f"Mcp-Session-Id: {mcp_session}"]
        cmd += ["--data-binary", f"@{payload}", base]
        proc = subprocess.run(cmd, text=True, capture_output=True, check=False)
        try: status = int(proc.stdout.strip().splitlines()[-1])
        except Exception: status = 0
        with open(headers) as fh: h = fh.read()
        next_sess = ""
        for line in h.splitlines():
            k, sep, v = line.partition(":")
            if sep and k.lower() == "mcp-session-id":
                next_sess = v.strip()
        with open(bodyf) as fh: response_body = fh.read()
        return status, next_sess, response_body, proc.stderr

def run_call(label, base, human, agent, sess, tool, expect_status):
    init_status, mcp_sess, init_body, init_stderr = post(base, {"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}, human, agent, sess)
    if init_status != 200:
        # Cross-tenant or unknown-session requests are rejected at initialize;
        # that is the gateway behavior we want to assert.
        return {
            "case": label,
            "expect": expect_status,
            "got_at": "initialize",
            "got": init_status,
            "body": init_body,
            "stderr": init_stderr,
            "ok": init_status == expect_status,
        }
    notify_status, _, notify_body, notify_stderr = post(base, {"jsonrpc":"2.0","method":"notifications/initialized"}, human, agent, sess, mcp_sess)
    if notify_status not in (200, 202):
        return {
            "case": label,
            "expect": expect_status,
            "got_at": "notifications/initialized",
            "got": notify_status,
            "body": notify_body,
            "stderr": notify_stderr,
            "ok": notify_status == expect_status,
        }
    args = {"a": 2, "b": 3} if tool == "add" else {"message": "hello"}
    call_status, _, call_body, call_stderr = post(base, {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":tool,"arguments":args}}, human, agent, sess, mcp_sess)
    return {
        "case": label,
        "expect": expect_status,
        "got_at": "tools/call",
        "got": call_status,
        "body": call_body,
        "stderr": call_stderr,
        "ok": call_status == expect_status,
    }

def call(label, base, human, agent, sess, tool, expect_status, retries=90, delay=2):
    last = {}
    for attempt in range(1, retries + 1):
        last = run_call(label, base, human, agent, sess, tool, expect_status)
        last["attempts"] = attempt
        if last["ok"]:
            report.append(last)
            return
        time.sleep(delay)
    report.append(last)

H_A = (os.environ["MT_HUMAN_A"], os.environ["MT_AGENT_A"], os.environ["MT_SESSION_A"])
H_B = (os.environ["MT_HUMAN_B"], os.environ["MT_AGENT_B"], os.environ["MT_SESSION_B"])

call("alice -> tenant-a / add (allow)",     A, *H_A, "add",   200)
call("alice -> tenant-a / upper (deny)",    A, *H_A, "upper", 403)
call("alice -> tenant-b / add (cross)",     B, *H_A, "add",   401)
call("bob   -> tenant-b / upper (allow)",   B, *H_B, "upper", 200)
call("bob   -> tenant-b / add (deny)",      B, *H_B, "add",   403)
call("bob   -> tenant-a / upper (cross)",   A, *H_B, "upper", 401)
call("no-headers -> tenant-a (deny)",       A, "", "", "",    "add", 401)
call("bogus-session -> tenant-a (deny)",    A, os.environ["MT_HUMAN_A"], os.environ["MT_AGENT_A"], "bogus-session", "add", 401)

with open(os.environ["MT_REPORT"], "w") as fh:
    json.dump(report, fh, indent=2)

failed = [r for r in report if not r["ok"]]
for r in report:
    mark = "PASS" if r["ok"] else "FAIL"
    detail = ""
    if not r["ok"]:
        body = " ".join(str(r.get("body", "")).split())
        if body:
            detail = f" body={body[:160]}"
    print(f"  [{mark}] {r['case']:<42}  expect={r['expect']}  got={r['got']} ({r['got_at']}, attempts={r.get('attempts', 1)}){detail}")
sys.exit(1 if failed else 0)
PY

  echo "[multitenancy] cleaning up tenant resources"
  kubectl delete mcpaccessgrant "alice-${MT_TENANT_A}" "bob-${MT_TENANT_B}" -n "${MT_NS}" --ignore-not-found --wait=false >/dev/null
  kubectl delete mcpagentsession "${MT_SESSION_A}" "${MT_SESSION_B}" -n "${MT_NS}" --ignore-not-found --wait=false >/dev/null
  parallel_reset
  parallel_start 2 "delete ${MT_TENANT_A}" delete_mcp_server_and_wait "${MT_TENANT_A}" "${MT_NS}" 60s
  parallel_start 2 "delete ${MT_TENANT_B}" delete_mcp_server_and_wait "${MT_TENANT_B}" "${MT_NS}" 60s
  parallel_wait_all
fi

echo "[cli] checking sentinel restart command"
# The full E2E stack packs single-node Kind tightly, so avoid requiring surge CPU for this restart smoke.
kubectl patch deployment mcp-sentinel-api -n mcp-sentinel --type merge -p '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":0,"maxUnavailable":1}}}}' >/dev/null
./bin/mcp-runtime sentinel restart api
rollout_status_with_logs mcp-sentinel deploy mcp-sentinel-api 180s

echo "[cli] deleting deployed MCP servers"
if scenario_selected "oauth"; then
  cleanup_mcp_server_and_wait "${OAUTH_SERVER_NAME}" mcp-servers 120s
fi
cleanup_mcp_server_and_wait "${PYTHON_EXAMPLE_SERVER_NAME}" mcp-servers 120s
cleanup_mcp_server_and_wait "${RUST_EXAMPLE_SERVER_NAME}" mcp-servers 120s
cleanup_mcp_server_and_wait "${GO_EXAMPLE_SERVER_NAME}" mcp-servers 120s
cleanup_mcp_server_and_wait "${SERVER_NAME}" mcp-servers 120s

echo "[done] E2E completed successfully"
