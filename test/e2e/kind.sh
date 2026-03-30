#!/usr/bin/env bash
set -euo pipefail

# End-to-end check on a fresh kind cluster:
# - build the CLI and publish runtime/sentinel images to a local docker mirror registry
# - run `mcp-runtime setup --test-mode`
# - deploy a policy-enabled MCP server through the CLI pipeline flow
# - exercise the deployed server through `mcp-smoke-agent` plus targeted MCP requests
# - verify audit events plus trace/log backends
#
# Set E2E_SCENARIOS to a comma-separated subset for local debugging.
# Supported values: all, smoke-auth, governance, trust, oauth, observability.
# observability requires the full traffic suite: smoke-auth, governance, trust, oauth.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"
echo "[info] Running from: ${PROJECT_ROOT}"

SENTINEL_ROOT="${PROJECT_ROOT}"
if [[ ! -d "${SENTINEL_ROOT}/services" || ! -d "${SENTINEL_ROOT}/k8s" ]]; then
  echo "expected flattened services/ and k8s/ layout under ${SENTINEL_ROOT}" >&2
  exit 1
fi
echo "[info] Sentinel root: ${SENTINEL_ROOT}"

CLUSTER_NAME="${CLUSTER_NAME:-mcp-e2e}"
SERVER_NAME="${SERVER_NAME:-policy-mcp-server}"
SERVER_HOST="${SERVER_HOST:-policy.example.local}"
OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME:-oauth-mcp-server}"
OAUTH_SERVER_HOST="${OAUTH_SERVER_HOST:-oauth.example.local}"
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
API_METRICS_PORT="${API_METRICS_PORT:-19090}"
INGEST_METRICS_PORT="${INGEST_METRICS_PORT:-19091}"
PROCESSOR_METRICS_PORT="${PROCESSOR_METRICS_PORT:-19092}"
MCP_SMOKE_DIR="${MCP_SMOKE_DIR:-}"
MCP_SMOKE_REF="${MCP_SMOKE_REF:-v0.3.0}"
MCP_SMOKE_REPO_URL="${MCP_SMOKE_REPO_URL:-https://github.com/Agent-Hellboy/mcp-smoke}"
MCP_SMOKE_TIMEOUT="${MCP_SMOKE_TIMEOUT:-20s}"
MCP_SMOKE_ANON_PORT="${MCP_SMOKE_ANON_PORT:-18084}"
MCP_SMOKE_IDENTITY_PORT="${MCP_SMOKE_IDENTITY_PORT:-18085}"
MCP_SMOKE_SESSION_PORT="${MCP_SMOKE_SESSION_PORT:-18086}"
MCP_SMOKE_BAD_SESSION_PORT="${MCP_SMOKE_BAD_SESSION_PORT:-18087}"
MCP_SMOKE_OAUTH_ANON_PORT="${MCP_SMOKE_OAUTH_ANON_PORT:-18088}"
MCP_SMOKE_OAUTH_INVALID_PORT="${MCP_SMOKE_OAUTH_INVALID_PORT:-18089}"
MCP_SMOKE_OAUTH_VALID_PORT="${MCP_SMOKE_OAUTH_VALID_PORT:-18090}"
MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION:-2025-06-18}"
MCP_POLICY_WAIT_TRIES="${MCP_POLICY_WAIT_TRIES:-90}"
RAW_REQUEST_TRIES="${RAW_REQUEST_TRIES:-10}"
MCP_SMOKE_AGENT_ENV_FILE="${MCP_SMOKE_AGENT_ENV_FILE:-.env}"
MCP_SMOKE_AGENT_PROMPT="${MCP_SMOKE_AGENT_PROMPT:-Use the MCP upper tool to convert the exact word governance to uppercase. Reply with only the uppercase result.}"
MCP_SMOKE_AGENT_PROVIDER="${MCP_SMOKE_AGENT_PROVIDER:-}"
MCP_SMOKE_AGENT_MODEL="${MCP_SMOKE_AGENT_MODEL:-}"
MCP_SMOKE_AGENT_TIMEOUT="${MCP_SMOKE_AGENT_TIMEOUT:-90s}"
UNKNOWN_SESSION_ID="${UNKNOWN_SESSION_ID:-sess-does-not-exist}"
TEST_MODE_REGISTRY_IMAGE="${TEST_MODE_REGISTRY_IMAGE:-docker.io/library/mcp-runtime-registry:latest}"
LOCAL_REGISTRY_NAME="${LOCAL_REGISTRY_NAME:-${CLUSTER_NAME}-dockerhub-mirror}"
LOCAL_REGISTRY_PORT="${LOCAL_REGISTRY_PORT:-5001}"
LOCAL_REGISTRY_PUSH_HOST="${LOCAL_REGISTRY_PUSH_HOST:-127.0.0.1:${LOCAL_REGISTRY_PORT}}"
LOCAL_REGISTRY_MIRROR_ENDPOINT="${LOCAL_REGISTRY_NAME}:5000"
E2E_ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-}"
E2E_SCENARIOS="${E2E_SCENARIOS-all}"
E2E_SCENARIOS="${E2E_SCENARIOS//[[:space:]]/}"
E2E_VALIDATE_SCENARIOS_ONLY="${E2E_VALIDATE_SCENARIOS_ONLY:-0}"
E2E_KEEP_CLUSTER="${E2E_KEEP_CLUSTER:-0}"

IFS=',' read -r -a E2E_SCENARIO_LIST <<< "${E2E_SCENARIOS}"
if [[ ${#E2E_SCENARIO_LIST[@]} -eq 0 || -z "${E2E_SCENARIO_LIST[0]}" ]]; then
  echo "E2E_SCENARIOS must not be empty" >&2
  exit 1
fi

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
  local scenario
  for scenario in "${E2E_SCENARIO_LIST[@]}"; do
    case "${scenario}" in
      all|smoke-auth|governance|trust|oauth|observability)
        ;;
      *)
        echo "unsupported E2E scenario: ${scenario}" >&2
        echo "supported values: all, smoke-auth, governance, trust, oauth, observability" >&2
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
echo "[info] E2E scenarios: $(describe_selected_scenarios)"
if [[ "${E2E_VALIDATE_SCENARIOS_ONLY}" == "1" ]]; then
  exit 0
fi

git config --global --add safe.directory "${PROJECT_ROOT}" >/dev/null 2>&1 || true

WORKDIR="$(mktemp -d)"
KIND_CONFIG="$(mktemp)"
ORIG_CONTEXT="$(kubectl config current-context 2>/dev/null || true)"
PIDS=()

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
    if (echo >/dev/tcp/127.0.0.1/"${port}") >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for localhost:${port}" >&2
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

assert_file_contains() {
  local needle="$1"
  local file="$2"

  if command -v rg >/dev/null 2>&1; then
    rg -F -q -- "${needle}" "${file}"
    return
  fi

  grep -F -q -- "${needle}" "${file}"
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

  kubectl port-forward -n "${namespace}" "svc/${service}" "${local_port}:${remote_port}" >"${log_file}" 2>&1 &
  PIDS+=("$!")
}

port_forward_resource_bg() {
  local namespace="$1"
  local resource="$2"
  local local_port="$3"
  local remote_port="$4"
  local log_file="$5"

  kubectl port-forward -n "${namespace}" "${resource}" "${local_port}:${remote_port}" >"${log_file}" 2>&1 &
  PIDS+=("$!")
}

start_header_proxy_bg() {
  local local_port="$1"
  local upstream_origin="$2"
  local log_file="$3"
  shift 3

  python3 "${PROJECT_ROOT}/test/e2e/mcp_header_proxy.py" \
    --listen-host 127.0.0.1 \
    --listen-port "${local_port}" \
    --upstream-origin "${upstream_origin}" \
    "$@" >"${log_file}" 2>&1 &
  PIDS+=("$!")
}

resolve_mcp_smoke_dir() {
  if [[ -n "${MCP_SMOKE_DIR}" ]]; then
    if [[ -f "${MCP_SMOKE_DIR}/go.mod" ]]; then
      echo "${MCP_SMOKE_DIR}"
      return 0
    fi
    echo "MCP_SMOKE_DIR does not point to an mcp-smoke checkout: ${MCP_SMOKE_DIR}" >&2
    return 1
  fi

  local cached_dir="/tmp/mcp-smoke-${MCP_SMOKE_REF}"
  if [[ -f "${cached_dir}/go.mod" ]]; then
    echo "${cached_dir}"
    return 0
  fi

  local clone_dir="${WORKDIR}/mcp-smoke-${MCP_SMOKE_REF}"
  git clone --depth 1 --branch "${MCP_SMOKE_REF}" "${MCP_SMOKE_REPO_URL}" "${clone_dir}" >&2
  echo "${clone_dir}"
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

run_mcp_smoke_expect() {
  local name="$1"
  local url="$2"
  local expected_ok="$3"
  local expected_tool_error="${4:-}"
  local output_file="${WORKDIR}/${name}.json"
  local smoke_exit_code=0

  if "${MCP_SMOKE_BIN}" smoke \
    --transport=http \
    --url "${url}" \
    --timeout "${MCP_SMOKE_TIMEOUT}" \
    --protocol "${MCP_PROTOCOL_VERSION}" \
    --tool-name "aaa-ping" \
    --tool-args '{}' \
    --prompt-name "hello" \
    --prompt-args '{}' \
    --resource-uri "embedded:readme" \
    >"${output_file}"; then
    smoke_exit_code=0
  else
    smoke_exit_code=$?
  fi

  SMOKE_NAME="${name}" \
  SMOKE_OUTPUT="${output_file}" \
  EXPECTED_OK="${expected_ok}" \
  EXPECTED_TOOL_ERROR="${expected_tool_error}" \
  SMOKE_EXIT_CODE="${smoke_exit_code}" \
  python3 <<'PY'
import json
import os

name = os.environ["SMOKE_NAME"]
expected_ok = os.environ["EXPECTED_OK"].lower() == "true"
expected_tool_error = os.environ.get("EXPECTED_TOOL_ERROR", "")
smoke_exit_code = int(os.environ.get("SMOKE_EXIT_CODE", "0"))

with open(os.environ["SMOKE_OUTPUT"], "r", encoding="utf-8") as fh:
    doc = json.load(fh)

if doc.get("transport") != "http":
    raise AssertionError(f"{name}: expected transport=http, got {doc.get('transport')!r}")

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
    if step_name not in steps:
        raise AssertionError(f"{name}: missing step {step_name}")

if bool(doc.get("ok")) != expected_ok:
    raise AssertionError(
        f"{name}: expected ok={expected_ok}, got {doc.get('ok')}: {json.dumps(doc, indent=2)}"
    )

if expected_ok:
    if smoke_exit_code != 0:
        raise AssertionError(f"{name}: expected exit code 0, got {smoke_exit_code}")
    for step_name in ("tools/call", "prompts/get", "resources/read"):
        step = steps[step_name]
        if not step.get("ok"):
            raise AssertionError(f"{name}: expected {step_name} to succeed: {json.dumps(step, indent=2)}")
else:
    if smoke_exit_code == 0:
        raise AssertionError(f"{name}: expected non-zero exit code for failed smoke run")
    failed_steps = {
        step_name: step
        for step_name, step in steps.items()
        if not step.get("ok") and not step.get("skipped")
    }
    if not failed_steps:
        raise AssertionError(f"{name}: expected at least one failed step: {json.dumps(doc, indent=2)}")
    if expected_tool_error:
        matching_steps = {
            step_name: step
            for step_name, step in failed_steps.items()
            if expected_tool_error in step.get("error", "")
        }
        if not matching_steps:
            rendered = json.dumps(failed_steps, indent=2)
            raise AssertionError(
                f"{name}: expected a failed step error to contain {expected_tool_error!r}, got {rendered}"
            )
    for step_name in ("tools/call", "prompts/get", "resources/read"):
        step = steps[step_name]
        if not step.get("ok") and not step.get("skipped"):
            if not expected_tool_error or expected_tool_error not in step.get("error", ""):
                raise AssertionError(
                    f"{name}: expected {step_name} to succeed, skip, or fail with {expected_tool_error!r}: "
                    f"{json.dumps(step, indent=2)}"
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
exit_code = str(smoke_exit_code)
if not expected_ok and smoke_exit_code != 0:
    exit_code = f"{smoke_exit_code} (expected non-zero)"
print(f"  exit code{' ' * (width - len('exit code'))}  {exit_code}")
for step_name, status in rows:
    print(f"  {step_name:{width}}  {status}")
PY
}

should_run_mcp_smoke_agent() {
  if [[ -n "${OPENAI_API_KEY:-}" || -n "${ANTHROPIC_API_KEY:-}" ]]; then
    return 0
  fi

  if [[ -f "${MCP_SMOKE_AGENT_ENV_FILE}" ]] && grep -Eq '^[[:space:]]*(export[[:space:]]+)?(OPENAI_API_KEY|ANTHROPIC_API_KEY)=' "${MCP_SMOKE_AGENT_ENV_FILE}"; then
    return 0
  fi

  return 1
}

run_mcp_smoke_agent_prompt() {
  local url="$1"
  local stdout_file="${WORKDIR}/mcp-smoke-agent.stdout"
  local stderr_file="${WORKDIR}/mcp-smoke-agent.stderr"
  local agent_exit_code=0
  local agent_cmd=(
    "${MCP_SMOKE_BIN}" agent
    --server "${url}"
    --env-file "${MCP_SMOKE_AGENT_ENV_FILE}"
    --prompt "${MCP_SMOKE_AGENT_PROMPT}"
    --timeout "${MCP_SMOKE_AGENT_TIMEOUT}"
  )

  if [[ -n "${MCP_SMOKE_AGENT_PROVIDER}" ]]; then
    agent_cmd+=(--provider "${MCP_SMOKE_AGENT_PROVIDER}")
  fi
  if [[ -n "${MCP_SMOKE_AGENT_MODEL}" ]]; then
    agent_cmd+=(--model "${MCP_SMOKE_AGENT_MODEL}")
  fi

  if "${agent_cmd[@]}" >"${stdout_file}" 2>"${stderr_file}"; then
    agent_exit_code=0
  else
    agent_exit_code=$?
  fi

  if [[ "${agent_exit_code}" -ne 0 ]]; then
    echo "mcp-smoke-agent exited with code ${agent_exit_code}" >&2
    echo "--- mcp-smoke-agent stderr ---" >&2
    cat "${stderr_file}" >&2 || true
    echo "--- mcp-smoke-agent stdout ---" >&2
    cat "${stdout_file}" >&2 || true
    return "${agent_exit_code}"
  fi

  MCP_SMOKE_AGENT_STDOUT="${stdout_file}" \
  MCP_SMOKE_AGENT_STDERR="${stderr_file}" \
  python3 <<'PY'
import os
import re

stdout_path = os.environ["MCP_SMOKE_AGENT_STDOUT"]
stderr_path = os.environ["MCP_SMOKE_AGENT_STDERR"]

with open(stdout_path, "r", encoding="utf-8") as fh:
    stdout = fh.read()
with open(stderr_path, "r", encoding="utf-8") as fh:
    stderr = fh.read()

if not re.search(r"^tool>\s+upper\s+", stderr, re.MULTILINE):
    raise AssertionError(f"mcp-smoke-agent did not call upper:\n{stderr}")
if "GOVERNANCE" not in stdout and "GOVERNANCE" not in stderr:
    raise AssertionError(f"mcp-smoke-agent did not produce the expected uppercase result:\nSTDOUT:\n{stdout}\nSTDERR:\n{stderr}")

print("mcp-smoke-agent:")
print("  tool call    upper")
print("  final answer GOVERNANCE")
PY
}

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

  for i in $(seq 1 "${tries}"); do
    if MCP_BASE="${base_url}" \
      MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
      MCP_EXPECT_STATUS="${expected_status}" \
      MCP_EXPECT_BODY_TEXT="${expected_body_text}" \
      MCP_EXPECT_HEADER_NAME="${expected_header_name}" \
      MCP_EXPECT_HEADER_TEXT="${expected_header_text}" \
      MCP_RESULT_FILE="${last_result_file}" \
      python3 <<'PY' >/dev/null 2>&1
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

headers = {
    "content-type": "application/json",
    "accept": "application/json, text/event-stream",
    "Mcp-Protocol-Version": protocol,
}
req = urllib.request.Request(
    base,
    data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}).encode(),
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
      python3 <<'PY' >/dev/null 2>&1
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
    raise SystemExit(1)

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
  local i
  local last_result_file="${WORKDIR}/last-mcp-tool-result.json"

  for i in $(seq 1 "${tries}"); do
    if MCP_BASE="${base_url}" \
      MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
      MCP_TOOL_NAME="${tool_name}" \
      MCP_TOOL_ARGS="${tool_args_json}" \
      MCP_EXPECT_STATUS="${expected_status}" \
      MCP_EXPECT_BODY_TEXT="${expected_body_text}" \
      MCP_RESULT_FILE="${last_result_file}" \
      python3 <<'PY' >/dev/null 2>&1
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]
tool_name = os.environ["MCP_TOOL_NAME"]
tool_args = json.loads(os.environ["MCP_TOOL_ARGS"])
expected_status = int(os.environ["MCP_EXPECT_STATUS"])
expected_body_text = os.environ.get("MCP_EXPECT_BODY_TEXT", "")
result_file = os.environ["MCP_RESULT_FILE"]


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


status, mcp_session_id, body = post({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}})
if status != 200 or not mcp_session_id:
    raise SystemExit(1)

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
if status not in (200, 202):
    raise SystemExit(1)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool_name, "arguments": tool_args}},
    mcp_session_id=mcp_session_id,
)
with open(result_file, "w", encoding="utf-8") as fh:
    json.dump({"status": status, "body": body}, fh)
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
    local gateway_ready
    local policy_ready
    local service_ready
    deployment_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.deploymentReady}' 2>/dev/null || true)"
    gateway_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.gatewayReady}' 2>/dev/null || true)"
    policy_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.policyReady}' 2>/dev/null || true)"
    service_ready="$(kubectl get mcpserver "${server_name}" -n "${namespace}" -o jsonpath='{.status.serviceReady}' 2>/dev/null || true)"
    if [[ "${deployment_ready}" == "true" && "${gateway_ready}" == "true" && "${policy_ready}" == "true" && "${service_ready}" == "true" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for MCPServer ${server_name} to report service/deployment/gateway/policy readiness" >&2
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

publish_image_to_local_registry() {
  local image="$1"
  local target
  target="$(local_registry_target "${image}")"

  echo "[registry] publishing ${image} to ${target}"
  docker tag "${image}" "${target}"
  docker push "${target}"
}

build_and_publish_image() {
  local image="$1"
  local dockerfile="$2"
  local context_dir="$3"

  echo "[image] building ${image}"
  docker build -t "${image}" -f "${dockerfile}" "${context_dir}"
  publish_image_to_local_registry "${image}"
}

mirror_upstream_image() {
  local image="$1"

  echo "[image] mirroring ${image} into ${LOCAL_REGISTRY_NAME}"
  docker pull "${image}"
  publish_image_to_local_registry "${image}"
}

start_local_registry() {
  if docker ps -a --format '{{.Names}}' | grep -qx "${LOCAL_REGISTRY_NAME}"; then
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

cat > "${KIND_CONFIG}" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["http://${LOCAL_REGISTRY_MIRROR_ENDPOINT}"]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry-1.docker.io"]
    endpoint = ["http://${LOCAL_REGISTRY_MIRROR_ENDPOINT}"]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
    endpoint = ["http://registry.registry.svc.cluster.local:5000"]
EOF

start_local_registry

echo "[kind] creating cluster ${CLUSTER_NAME}"
kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
connect_local_registry_to_kind_network
KUBECONFIG_FILE="/tmp/kubeconfig-kind"
kind get kubeconfig --name "${CLUSTER_NAME}" > "${KUBECONFIG_FILE}"
export KUBECONFIG="${KUBECONFIG_FILE}"
kubectl config use-context "kind-${CLUSTER_NAME}"
mkdir -p "${HOME}/.kube"
cp "${KUBECONFIG_FILE}" "${HOME}/.kube/config"

echo "[build] rebuilding CLI"
GOCACHE="${PROJECT_ROOT}/.gocache" go build -o bin/mcp-runtime ./cmd/mcp-runtime

MCP_SMOKE_SOURCE_DIR="$(resolve_mcp_smoke_dir)"
MCP_SMOKE_BIN="${WORKDIR}/mcp-smoke-agent"
MCP_SMOKE_GOPATH="${WORKDIR}/mcp-smoke-gopath"
echo "[build] building mcp-smoke-agent from ${MCP_SMOKE_SOURCE_DIR}"
mkdir -p "${MCP_SMOKE_GOPATH}"
(
  cd "${MCP_SMOKE_SOURCE_DIR}"
  GOPATH="${MCP_SMOKE_GOPATH}" \
  GOMODCACHE="${MCP_SMOKE_GOPATH}/pkg/mod" \
  GOCACHE="${PROJECT_ROOT}/.gocache" \
  go build -o "${MCP_SMOKE_BIN}" ./cmd/mcp-smoke-agent
)

mirror_upstream_image "registry:2.8.3"
mirror_upstream_image "traefik:v2.10"
mirror_upstream_image "traefik:v3.0"
mirror_upstream_image "clickhouse/clickhouse-server:23.8"
mirror_upstream_image "confluentinc/cp-zookeeper:7.5.1"
mirror_upstream_image "confluentinc/cp-kafka:7.5.1"
mirror_upstream_image "prom/prometheus:v2.49.1"
mirror_upstream_image "otel/opentelemetry-collector:0.92.0"
mirror_upstream_image "grafana/tempo:2.3.1"
mirror_upstream_image "grafana/loki:2.9.4"
mirror_upstream_image "grafana/promtail:2.9.4"
mirror_upstream_image "grafana/grafana:10.2.3"
mirror_upstream_image "nginx:1.27-alpine"
build_and_publish_image "docker.io/library/mcp-runtime-operator:latest" "Dockerfile.operator" "."
build_and_publish_image "${TEST_MODE_REGISTRY_IMAGE}" "test/e2e/registry.Dockerfile" "."
build_and_publish_image "docker.io/library/mcp-sentinel-mcp-proxy:latest" "${SENTINEL_ROOT}/services/mcp-proxy/Dockerfile" "${SENTINEL_ROOT}/services/mcp-proxy"
build_and_publish_image "docker.io/library/mcp-sentinel-ingest:latest" "${SENTINEL_ROOT}/services/ingest/Dockerfile" "${SENTINEL_ROOT}/services/ingest"
build_and_publish_image "docker.io/library/mcp-sentinel-api:latest" "${SENTINEL_ROOT}/services/api/Dockerfile" "${SENTINEL_ROOT}"
build_and_publish_image "docker.io/library/mcp-sentinel-processor:latest" "${SENTINEL_ROOT}/services/processor/Dockerfile" "${SENTINEL_ROOT}/services/processor"
build_and_publish_image "docker.io/library/mcp-sentinel-ui:latest" "${SENTINEL_ROOT}/services/ui/Dockerfile" "${SENTINEL_ROOT}/services/ui"

echo "[setup] running platform setup in test mode"
MCP_RUNTIME_REGISTRY_IMAGE_OVERRIDE="${TEST_MODE_REGISTRY_IMAGE}" \
./bin/mcp-runtime setup --test-mode --ingress-manifest config/ingress/overlays/http

echo "[verify] waiting for core platform components"
kubectl rollout status deploy/registry -n registry --timeout=180s
kubectl rollout status deploy/mcp-runtime-operator-controller-manager -n mcp-runtime --timeout=180s
kubectl rollout status deploy/traefik -n traefik --timeout=180s
kubectl rollout status deploy/mcp-sentinel-api -n mcp-sentinel --timeout=180s
kubectl rollout status deploy/mcp-sentinel-gateway -n mcp-sentinel --timeout=180s
kubectl rollout status statefulset/tempo -n mcp-sentinel --timeout=180s
kubectl rollout status statefulset/loki -n mcp-sentinel --timeout=300s

echo "[cli] checking platform status commands"
./bin/mcp-runtime status
./bin/mcp-runtime cluster status
./bin/mcp-runtime registry status
./bin/mcp-runtime registry info

API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.API_KEYS}' | decode_base64 | cut -d',' -f1)"
if [[ -z "${API_KEY}" ]]; then
  echo "[error] failed to resolve mcp-sentinel API key from secret" >&2
  exit 1
fi

METADATA_FILE="${WORKDIR}/metadata.yaml"
MANIFEST_DIR="${WORKDIR}/manifests"
SERVER_IMAGE="docker.io/library/${SERVER_NAME}:latest"
SERVER_SECRET_NAME="${SERVER_NAME}-analytics-creds"

echo "[deploy] creating server-local analytics credentials secret"
kubectl create secret generic "${SERVER_SECRET_NAME}" \
  -n mcp-servers \
  --from-literal=api-key="${API_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

cat > "${METADATA_FILE}" <<EOF
version: v1
servers:
  - name: ${SERVER_NAME}
    route: /${SERVER_NAME}/mcp
    ingressHost: ${SERVER_HOST}
    port: 8090
    namespace: mcp-servers
    envVars:
      - name: PORT
        value: "8090"
      - name: MCP_SENTINEL_INGEST_URL
        value: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      - name: OTEL_EXPORTER_OTLP_ENDPOINT
        value: "http://otel-collector.mcp-sentinel.svc.cluster.local:4318"
      - name: OTEL_SERVICE_NAME
        value: "${SERVER_NAME}"
    secretEnvVars:
      - name: MCP_SENTINEL_API_KEY
        secretKeyRef:
          name: ${SERVER_SECRET_NAME}
          key: api-key
    tools:
      - name: aaa-ping
        requiredTrust: low
      - name: echo
        requiredTrust: low
      - name: upper
        requiredTrust: medium
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
    analytics:
      enabled: true
      ingestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      apiKeySecretRef:
        name: ${SERVER_SECRET_NAME}
        key: api-key
EOF

echo "[cli] building MCP server image via CLI"
./bin/mcp-runtime server build image "${SERVER_NAME}" \
  --metadata-file "${METADATA_FILE}" \
  --dockerfile "${SENTINEL_ROOT}/services/mcp-server/Dockerfile" \
  --registry docker.io/library \
  --tag latest \
  --context "${SENTINEL_ROOT}/services/mcp-server"

publish_image_to_local_registry "${SERVER_IMAGE}"

echo "[cli] generating and deploying MCPServer manifests"
./bin/mcp-runtime pipeline generate --file "${METADATA_FILE}" --output "${MANIFEST_DIR}"
./bin/mcp-runtime pipeline deploy --dir "${MANIFEST_DIR}"

echo "[deploy] waiting for MCP server rollout"
wait_for_deployment_exists mcp-servers "${SERVER_NAME}"
if ! kubectl rollout status "deploy/${SERVER_NAME}" -n mcp-servers --timeout=180s; then
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

echo "[cli] checking server commands"
./bin/mcp-runtime server list --namespace mcp-servers
./bin/mcp-runtime server get "${SERVER_NAME}" --namespace mcp-servers
./bin/mcp-runtime server status --namespace mcp-servers
./bin/mcp-runtime server logs "${SERVER_NAME}" --namespace mcp-servers >"${WORKDIR}/${SERVER_NAME}.logs"

echo "[policy] applying access grant and low-trust session"
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
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
    - name: echo
      decision: allow
    - name: upper
      decision: allow
---
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

wait_for_policy_text "\"name\": \"${SESSION_ID}\""
wait_for_policy_text "\"consented_trust\": \"low\""
print_gateway_policy_debug

if scenario_selected "governance"; then
  echo "[cli] checking access management commands"
  ./bin/mcp-runtime access grant list --namespace mcp-servers >"${WORKDIR}/access-grant-list.txt"
  assert_file_contains "${SERVER_NAME}-grant" "${WORKDIR}/access-grant-list.txt"
  ./bin/mcp-runtime access grant get "${SERVER_NAME}-grant" --namespace mcp-servers >"${WORKDIR}/access-grant-get.yaml"
  assert_file_contains "maxTrust: high" "${WORKDIR}/access-grant-get.yaml"
  ./bin/mcp-runtime access session list --namespace mcp-servers >"${WORKDIR}/access-session-list.txt"
  assert_file_contains "${SESSION_ID}" "${WORKDIR}/access-session-list.txt"
  ./bin/mcp-runtime access session get "${SESSION_ID}" --namespace mcp-servers >"${WORKDIR}/access-session-get.yaml"
  assert_file_contains "consentedTrust: low" "${WORKDIR}/access-session-get.yaml"
fi

echo "[port-forward] exposing ingress and observability services"
port_forward_bg traefik traefik "${TRAEFIK_PORT}" 8000 "${WORKDIR}/traefik-port-forward.log"
port_forward_bg mcp-sentinel mcp-sentinel-gateway "${SENTINEL_PORT}" 8083 "${WORKDIR}/sentinel-port-forward.log"
port_forward_bg mcp-sentinel tempo "${TEMPO_PORT}" 3200 "${WORKDIR}/tempo-port-forward.log"
port_forward_bg mcp-sentinel loki "${LOKI_PORT}" 3100 "${WORKDIR}/loki-port-forward.log"
if scenario_selected "observability"; then
  port_forward_bg mcp-sentinel mcp-sentinel-api "${API_SERVICE_PORT}" 8080 "${WORKDIR}/api-port-forward.log"
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
if scenario_selected "observability"; then
  wait_port "${API_SERVICE_PORT}"
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

echo "[proxy] starting local ingress proxies for mcp-smoke"
start_header_proxy_bg "${MCP_SMOKE_ANON_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-anon-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
start_header_proxy_bg "${MCP_SMOKE_IDENTITY_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-identity-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}"
start_header_proxy_bg "${MCP_SMOKE_SESSION_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-session-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}" \
  --header "X-MCP-Agent-Session=${SESSION_ID}"
start_header_proxy_bg "${MCP_SMOKE_BAD_SESSION_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-bad-session-proxy.log" \
  --host-header "${SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "X-MCP-Human-ID=${HUMAN_ID}" \
  --header "X-MCP-Agent-ID=${AGENT_ID}" \
  --header "X-MCP-Agent-Session=${UNKNOWN_SESSION_ID}"

wait_port "${MCP_SMOKE_ANON_PORT}"
wait_port "${MCP_SMOKE_IDENTITY_PORT}"
wait_port "${MCP_SMOKE_SESSION_PORT}"
wait_port "${MCP_SMOKE_BAD_SESSION_PORT}"

MCP_INGRESS_PATH="/${SERVER_NAME}/mcp"
MCP_DIRECT_URL="http://127.0.0.1:${TRAEFIK_PORT}${MCP_INGRESS_PATH}"
MCP_ANON_URL="http://127.0.0.1:${MCP_SMOKE_ANON_PORT}${MCP_INGRESS_PATH}"
MCP_IDENTITY_URL="http://127.0.0.1:${MCP_SMOKE_IDENTITY_PORT}${MCP_INGRESS_PATH}"
MCP_SESSION_URL="http://127.0.0.1:${MCP_SMOKE_SESSION_PORT}${MCP_INGRESS_PATH}"
MCP_BAD_SESSION_URL="http://127.0.0.1:${MCP_SMOKE_BAD_SESSION_PORT}${MCP_INGRESS_PATH}"

if scenario_selected "smoke-auth"; then
  echo "[mcp] validating raw MCP request edge cases"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"2099-01-01\"}" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    400 \
    "Unsupported protocol version"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"text/plain\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    text \
    '' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    text \
    '{"jsonrpc":' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    text \
    '{"jsonrpc":"2.0","id":1,"params":{}}' \
    403 \
    "rpc_inspection_failed"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\"}" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    chunked-text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    GET \
    "{\"Host\":\"${SERVER_HOST}\",\"accept\":\"text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    none \
    '' \
    400 \
    "GET requires an Mcp-Session-Id header"
  wait_for_http_result \
    "${MCP_DIRECT_URL}" \
    DELETE \
    "{\"Host\":\"${SERVER_HOST}\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\"}" \
    none \
    '' \
    400 \
    "DELETE requires an Mcp-Session-Id header"

  echo "[mcp] running external mcp-smoke smoke checks against ingress"
  run_mcp_smoke_expect "mcp-smoke-missing-identity" "${MCP_ANON_URL}" false "missing_identity"
  run_mcp_smoke_expect "mcp-smoke-missing-session" "${MCP_IDENTITY_URL}" false "missing_session"
  run_mcp_smoke_expect "mcp-smoke-session-not-found" "${MCP_BAD_SESSION_URL}" false "session_not_found"
  echo "[mcp] waiting for session-backed allow policy to reach the gateway"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200
  run_mcp_smoke_expect "mcp-smoke-allow-aaa-ping" "${MCP_SESSION_URL}" true
fi

if scenario_selected "governance"; then
  echo "[policy] revoking access session via CLI"
  ./bin/mcp-runtime access session revoke "${SESSION_ID}" --namespace mcp-servers
  wait_for_policy_text "\"revoked\": true"
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 401 "session_revoked"
  run_mcp_smoke_expect "mcp-smoke-session-revoked" "${MCP_SESSION_URL}" false "session_revoked"

  echo "[policy] restoring access session via CLI"
  ./bin/mcp-runtime access session unrevoke "${SESSION_ID}" --namespace mcp-servers
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200

  echo "[policy] expiring access session via manifest update"
  EXPIRED_AT="$(python3 <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc) - timedelta(minutes=5)).replace(microsecond=0).isoformat().replace("+00:00", "Z"))
PY
)"
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
  consentedTrust: low
  policyVersion: v1
  expiresAt: ${EXPIRED_AT}
EOF
  wait_for_policy_text "\"expires_at\": \"${EXPIRED_AT}\""
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 401 "session_expired"
  run_mcp_smoke_expect "mcp-smoke-session-expired" "${MCP_SESSION_URL}" false "session_expired"

  echo "[policy] restoring non-expired access session"
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
  consentedTrust: low
  policyVersion: v1
EOF
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200

  echo "[policy] disabling access grant via CLI"
  ./bin/mcp-runtime access grant disable "${SERVER_NAME}-grant" --namespace mcp-servers
  wait_for_policy_text "\"disabled\": true"
  print_gateway_policy_debug
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 403 "tool_not_granted"
  run_mcp_smoke_expect "mcp-smoke-grant-disabled" "${MCP_SESSION_URL}" false "tool_not_granted"

  echo "[policy] re-enabling access grant via CLI"
  ./bin/mcp-runtime access grant enable "${SERVER_NAME}-grant" --namespace mcp-servers
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200
fi

if scenario_selected "trust"; then
  echo "[mcp] validating targeted echo and upper tool behavior"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]


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


status, mcp_session_id, body = post({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}})
if status != 200 or not mcp_session_id:
    raise AssertionError(f"initialize failed before trust update: {status} {body}")

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
if status not in (200, 202):
    raise AssertionError(f"notifications/initialized failed: {status} {body}")

status, _, body = post(
    {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "echo", "arguments": {"message": "hello"}}},
    mcp_session_id=mcp_session_id,
)
if status != 200 or "hello" not in body:
    raise AssertionError(f"expected echo to succeed before trust update, got {status}: {body}")
print("echo allow:", body)

status, _, body = post(
    {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "upper", "arguments": {"message": "governance"}}},
    mcp_session_id=mcp_session_id,
)
payload = json.loads(body)
if status != 403 or payload.get("error") != "trust_too_low":
    raise AssertionError(f"expected upper to be denied before trust update, got {status}: {body}")
print("upper deny:", body)
PY

  echo "[policy] raising consented trust to medium"
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
  echo "[mcp] waiting for updated consented trust to reach the gateway"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "upper" '{"message":"governance"}' 200 "GOVERNANCE"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "add" '{"a":2,"b":3}' 403 "tool_not_granted"

  echo "[mcp] validating updated policy allows the higher-trust tool"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]


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


status, mcp_session_id, body = post({"jsonrpc": "2.0", "id": 6, "method": "initialize", "params": {}})
if status != 200 or not mcp_session_id:
    raise AssertionError(f"initialize failed after trust update: {status} {body}")

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
if status not in (200, 202):
    raise AssertionError(f"notifications/initialized failed: {status} {body}")

status, _, body = post(
    {"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": {"name": "upper", "arguments": {"message": "governance"}}},
    mcp_session_id=mcp_session_id,
)
if status != 200:
    raise AssertionError(f"expected upper to succeed after trust update, got {status}: {body}")
if "GOVERNANCE" not in body:
    raise AssertionError(f"expected uppercase result, got {body}")
print("upper allow:", body)
PY

  if should_run_mcp_smoke_agent; then
    echo "[mcp] running optional real-client mcp-smoke agent prompt"
    run_mcp_smoke_agent_prompt "${MCP_SESSION_URL}"
  else
    echo "[mcp] skipping optional real-client mcp-smoke agent prompt (no OPENAI_API_KEY/ANTHROPIC_API_KEY in env or ${MCP_SMOKE_AGENT_ENV_FILE})"
  fi

  echo "[policy] updating access grant to deny aaa-ping and echo"
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
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: deny
    - name: echo
      decision: deny
    - name: upper
      decision: allow
EOF

  wait_for_grant_tool_rule "${SERVER_NAME}-grant" "aaa-ping" "deny"
  wait_for_grant_tool_rule "${SERVER_NAME}-grant" "echo" "deny"
  print_gateway_policy_debug

  echo "[mcp] validating updated access grant denies aaa-ping and echo"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 403 "tool_denied"
  wait_for_mcp_tool_result "${MCP_SESSION_URL}" "echo" '{"message":"analytics"}' 403 "tool_denied"
  run_mcp_smoke_expect "mcp-smoke-aaa-ping-deny" "${MCP_SESSION_URL}" false "tool_denied"
  MCP_BASE="${MCP_SESSION_URL}" \
  MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION}" \
  python3 <<'PY'
import json
import os
import urllib.error
import urllib.request

base = os.environ["MCP_BASE"]
protocol = os.environ["MCP_PROTOCOL_VERSION"]


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
if status != 200 or not mcp_session_id:
    raise AssertionError(f"initialize failed after grant update: {status} {body}")

status, _, body = post({"jsonrpc": "2.0", "method": "notifications/initialized"}, mcp_session_id=mcp_session_id)
if status not in (200, 202):
    raise AssertionError(f"notifications/initialized failed: {status} {body}")

status, _, body = post(
    {"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": {"name": "echo", "arguments": {"message": "analytics"}}},
    mcp_session_id=mcp_session_id,
)
payload = json.loads(body)
if status != 403 or payload.get("error") != "tool_denied":
    raise AssertionError(f"expected echo to be denied after grant update, got {status}: {body}")
print("echo deny:", body)
PY
fi

if scenario_selected "oauth"; then
  OAUTH_FIXTURE_DIR="${WORKDIR}/oauth-fixtures"
  generate_oauth_fixtures "${OAUTH_FIXTURE_DIR}"
  cat >"${OAUTH_FIXTURE_DIR}/default.conf" <<'EOF'
server {
  listen 8080;
  server_name _;

  location / {
    root /usr/share/nginx/html;
    try_files $uri =404;
  }
}
EOF
  OAUTH_VALID_TOKEN="$(tr -d '\n' <"${OAUTH_FIXTURE_DIR}/valid-token.txt")"
  OAUTH_INVALID_TOKEN="$(tr -d '\n' <"${OAUTH_FIXTURE_DIR}/invalid-token.txt")"

  echo "[oauth] deploying mock OAuth issuer"
  kubectl create configmap "${OAUTH_ISSUER_NAME}-files" \
  -n mcp-servers \
  --from-file=oauth-authorization-server="${OAUTH_FIXTURE_DIR}/oauth-authorization-server" \
  --from-file=keys="${OAUTH_FIXTURE_DIR}/keys" \
  --from-file=default.conf="${OAUTH_FIXTURE_DIR}/default.conf" \
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
      containers:
        - name: nginx
          image: docker.io/library/nginx:1.27-alpine
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: files
              mountPath: /usr/share/nginx/html/.well-known/oauth-authorization-server
              subPath: oauth-authorization-server
            - name: files
              mountPath: /usr/share/nginx/html/keys
              subPath: keys
            - name: files
              mountPath: /etc/nginx/conf.d/default.conf
              subPath: default.conf
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
    ingressHost: ${OAUTH_SERVER_HOST}
    port: 8090
    namespace: mcp-servers
    envVars:
      - name: PORT
        value: "8090"
      - name: MCP_SENTINEL_INGEST_URL
        value: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      - name: OTEL_EXPORTER_OTLP_ENDPOINT
        value: "http://otel-collector.mcp-sentinel.svc.cluster.local:4318"
      - name: OTEL_SERVICE_NAME
        value: "${OAUTH_SERVER_NAME}"
    secretEnvVars:
      - name: MCP_SENTINEL_API_KEY
        secretKeyRef:
          name: ${SERVER_SECRET_NAME}
          key: api-key
    tools:
      - name: aaa-ping
        requiredTrust: low
      - name: upper
        requiredTrust: low
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
    analytics:
      enabled: true
      ingestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      apiKeySecretRef:
        name: ${SERVER_SECRET_NAME}
        key: api-key
EOF

  echo "[oauth] deploying OAuth-protected MCP server"
  ./bin/mcp-runtime pipeline generate --file "${OAUTH_METADATA_FILE}" --output "${OAUTH_MANIFEST_DIR}"
  ./bin/mcp-runtime pipeline deploy --dir "${OAUTH_MANIFEST_DIR}"
  wait_for_deployment_exists mcp-servers "${OAUTH_SERVER_NAME}"
  if ! kubectl rollout status "deploy/${OAUTH_SERVER_NAME}" -n mcp-servers --timeout=180s; then
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
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
    - name: upper
      decision: allow
EOF
  
  echo "[oauth] starting local ingress proxies"
  start_header_proxy_bg "${MCP_SMOKE_OAUTH_ANON_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-oauth-anon-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}"
  start_header_proxy_bg "${MCP_SMOKE_OAUTH_INVALID_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-oauth-invalid-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "Authorization=Bearer ${OAUTH_INVALID_TOKEN}"
  start_header_proxy_bg "${MCP_SMOKE_OAUTH_VALID_PORT}" \
  "http://127.0.0.1:${TRAEFIK_PORT}" \
  "${WORKDIR}/mcp-smoke-oauth-valid-proxy.log" \
  --host-header "${OAUTH_SERVER_HOST}" \
  --header "Mcp-Protocol-Version=${MCP_PROTOCOL_VERSION}" \
  --header "Authorization=Bearer ${OAUTH_VALID_TOKEN}"
  
  wait_port "${MCP_SMOKE_OAUTH_ANON_PORT}"
  wait_port "${MCP_SMOKE_OAUTH_INVALID_PORT}"
  wait_port "${MCP_SMOKE_OAUTH_VALID_PORT}"
  if scenario_selected "observability"; then
    port_forward_bg mcp-servers "${OAUTH_SERVER_NAME}" "${OAUTH_PROXY_PORT}" 80 "${WORKDIR}/oauth-proxy-port-forward.log"
    port_forward_resource_bg mcp-servers "deployment/${OAUTH_SERVER_NAME}" "${OAUTH_UPSTREAM_PORT}" 8090 "${WORKDIR}/oauth-upstream-port-forward.log"
    wait_port "${OAUTH_PROXY_PORT}"
    wait_port "${OAUTH_UPSTREAM_PORT}"
  fi

  OAUTH_INGRESS_PATH="/${OAUTH_SERVER_NAME}/mcp"
  MCP_OAUTH_DIRECT_URL="http://127.0.0.1:${TRAEFIK_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_ANON_URL="http://127.0.0.1:${MCP_SMOKE_OAUTH_ANON_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_INVALID_URL="http://127.0.0.1:${MCP_SMOKE_OAUTH_INVALID_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_VALID_URL="http://127.0.0.1:${MCP_SMOKE_OAUTH_VALID_PORT}${OAUTH_INGRESS_PATH}"
  MCP_OAUTH_METADATA_URL="http://127.0.0.1:${MCP_SMOKE_OAUTH_ANON_PORT}/.well-known/oauth-protected-resource${OAUTH_INGRESS_PATH}"

  echo "[oauth] validating protected-resource metadata"
  wait_http "${MCP_OAUTH_METADATA_URL}"
  MCP_OAUTH_METADATA_URL="${MCP_OAUTH_METADATA_URL}" \
  OAUTH_ISSUER_URL="${OAUTH_ISSUER_URL}" \
  OAUTH_RESOURCE_URL="http://${OAUTH_SERVER_HOST}${OAUTH_INGRESS_PATH}" \
  python3 <<'PY'
import json
import os
import urllib.request

req = urllib.request.Request(os.environ["MCP_OAUTH_METADATA_URL"], headers={"accept": "application/json"})
resp = urllib.request.urlopen(req, timeout=10)
doc = json.loads(resp.read().decode())

if resp.status != 200:
    raise AssertionError(f"expected 200 from protected resource metadata, got {resp.status}")
if doc.get("authorization_servers") != [os.environ["OAUTH_ISSUER_URL"]]:
    raise AssertionError(f"unexpected authorization_servers: {doc}")
if doc.get("resource") != os.environ["OAUTH_RESOURCE_URL"]:
    raise AssertionError(f"unexpected resource URL: {doc}")
if "header" not in doc.get("bearer_methods_supported", []):
    raise AssertionError(f"expected bearer_methods_supported to include header, got {doc}")
print("oauth metadata:", json.dumps(doc))
PY

  echo "[oauth] validating missing and invalid bearer token challenges"
  wait_for_mcp_initialize_result "${MCP_OAUTH_ANON_URL}" 401 "missing_bearer_token" "www-authenticate" "resource_metadata="
  wait_for_mcp_initialize_result "${MCP_OAUTH_INVALID_URL}" 401 "invalid_token" "www-authenticate" 'error="invalid_token"'
  wait_for_http_result \
    "${MCP_OAUTH_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${OAUTH_SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\",\"Authorization\":\"${OAUTH_VALID_TOKEN}\"}" \
    text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    401 \
    "missing_bearer_token" \
    "www-authenticate" \
    "resource_metadata="
  run_mcp_smoke_expect "mcp-smoke-oauth-missing-token" "${MCP_OAUTH_ANON_URL}" false "missing_bearer_token"
  run_mcp_smoke_expect "mcp-smoke-oauth-invalid-token" "${MCP_OAUTH_INVALID_URL}" false "invalid_token"

  echo "[oauth] validating valid bearer token MCP flow"
  wait_for_mcp_tool_result "${MCP_OAUTH_VALID_URL}" "aaa-ping" '{}' 200
  wait_for_http_result \
    "${MCP_OAUTH_DIRECT_URL}" \
    POST \
    "{\"Host\":\"${OAUTH_SERVER_HOST}\",\"content-type\":\"application/json\",\"accept\":\"application/json, text/event-stream\",\"Mcp-Protocol-Version\":\"${MCP_PROTOCOL_VERSION}\",\"Authorization\":\"Bearer ${OAUTH_VALID_TOKEN}\"}" \
    chunked-text \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    200
  run_mcp_smoke_expect "mcp-smoke-oauth-valid" "${MCP_OAUTH_VALID_URL}" true
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
  python3 <<'PY'
import json
import os
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
grant_name = f"{server_name}-grant"
oauth_public_base = f"http://{oauth_server_host}"


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
    if got_status != status:
        raise AssertionError(f"{method} {url} returned {got_status}: {got_body}")
    if contains and contains not in got_body:
        raise AssertionError(f"{method} {url} missing {contains!r}: {got_body}")
    return got_body


def expect_json(url, status=200, *, method="GET", headers=None, body=None):
    payload = expect_status(url, status, method=method, headers=headers, body=body)
    return json.loads(payload)


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
        body={"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}},
    )
    if got_status != status:
        raise AssertionError(f"POST {url} initialize returned {got_status}: {got_body}")
    if contains and contains not in got_body:
        raise AssertionError(f"POST {url} initialize missing {contains!r}: {got_body}")
    if got_status == 200:
        doc = json.loads(got_body)
        if "result" not in doc:
            raise AssertionError(f"POST {url} initialize missing result: {doc}")
        header_map = {k.lower(): v for k, v in got_headers.items()}
        if "mcp-session-id" not in header_map:
            raise AssertionError(f"POST {url} initialize missing Mcp-Session-Id: {got_headers}")
    return got_body


auth_headers = {"x-api-key": api_key}

# Gateway-routed UI, API, and example MCP routes.
gateway_summary = expect_json(f"{gateway_base}/api/dashboard/summary", headers=auth_headers)
for key in ("total_events", "active_servers", "active_grants", "active_sessions"):
    if key not in gateway_summary:
        raise AssertionError(f"gateway dashboard summary missing {key}: {gateway_summary}")
expect_status(f"{gateway_base}/ping", 200, contains="OK")
expect_status(f"{gateway_base}/", 200, contains="MCP Sentinel Control Plane")
expect_status(f"{gateway_base}/config.js", 200, contains="window.MCP_API_BASE")
expect_status(f"{gateway_base}/app.js", 200, contains="const apiBase")
expect_status(f"{gateway_base}/styles.css", 200, contains=".canvas")
expect_status(f"{gateway_base}/grafana/api/health", 200, contains="database")
expect_status(f"{gateway_base}/prometheus/-/healthy", 200, contains="Healthy")

# Direct UI service.
expect_status(f"{ui_base}/health", 200, contains='"ok":true')
expect_status(f"{ui_base}/", 200, contains="MCP Sentinel Control Plane")
expect_status(f"{ui_base}/config.js", 200, contains="window.MCP_API_BASE")
expect_status(f"{ui_base}/app.js", 200, contains="const apiBase")
expect_status(f"{ui_base}/styles.css", 200, contains=".canvas")

# Direct MCP proxy and upstream server surfaces.
expect_status(f"{server_proxy_base}/health", 200, contains="ok")
expect_mcp_initialize(
    f"{server_proxy_base}/",
    headers={
        "X-MCP-Human-ID": human_id,
        "X-MCP-Agent-ID": agent_id,
        "X-MCP-Agent-Session": session_id,
    },
)
expect_status(f"{server_upstream_base}/health", 200, contains='"ok":true')
expect_mcp_initialize(f"{server_upstream_base}/")

expect_status(f"{oauth_proxy_base}/health", 200, contains="ok")
oauth_metadata = expect_json(f"{oauth_proxy_base}/.well-known/oauth-protected-resource")
if oauth_metadata.get("authorization_servers") != [oauth_issuer_url]:
    raise AssertionError(f"unexpected oauth metadata authorization servers: {oauth_metadata}")
if oauth_metadata.get("bearer_methods_supported") != ["header"]:
    raise AssertionError(f"unexpected oauth metadata bearer methods: {oauth_metadata}")
if oauth_metadata.get("resource") != f"{oauth_public_base}/":
    raise AssertionError(f"unexpected oauth metadata resource URL: {oauth_metadata}")
oauth_metadata_path = expect_json(
    f"{oauth_proxy_base}/.well-known/oauth-protected-resource/{oauth_server_name}/mcp"
)
if oauth_metadata_path.get("resource") != f"{oauth_public_base}/{oauth_server_name}/mcp":
    raise AssertionError(f"unexpected oauth metadata path resource URL: {oauth_metadata_path}")
expect_mcp_initialize(
    f"{oauth_proxy_base}/",
    headers={"Authorization": f"Bearer {oauth_valid_token}"},
)
expect_status(f"{oauth_upstream_base}/health", 200, contains='"ok":true')
expect_mcp_initialize(f"{oauth_upstream_base}/")

# API service surfaces.
expect_status(f"{api_base}/health", 200, contains='"ok":true')
expect_status(api_metrics_url, 200, contains="# HELP")
events = expect_json(f"{api_base}/api/events?limit=5", headers=auth_headers)
if not events.get("events"):
    raise AssertionError(f"expected /api/events to return events: {events}")
stats = expect_json(f"{api_base}/api/stats", headers=auth_headers)
if int(stats.get("events_total", 0)) < 1:
    raise AssertionError(f"expected /api/stats events_total >= 1: {stats}")
sources = expect_json(f"{api_base}/api/sources", headers=auth_headers)
if not sources.get("sources"):
    raise AssertionError(f"expected /api/sources to return sources: {sources}")
event_types = expect_json(f"{api_base}/api/event-types", headers=auth_headers)
if not event_types.get("event_types"):
    raise AssertionError(f"expected /api/event-types to return event types: {event_types}")
filtered = expect_json(
    f"{api_base}/api/events/filter?server={urllib.parse.quote(server_name)}&limit=5",
    headers=auth_headers,
)
if not filtered.get("events"):
    raise AssertionError(f"expected /api/events/filter to return events: {filtered}")
summary = expect_json(f"{api_base}/api/dashboard/summary", headers=auth_headers)
for key in ("total_events", "active_servers", "active_grants", "active_sessions"):
    if key not in summary:
        raise AssertionError(f"dashboard summary missing {key}: {summary}")
servers = expect_json(f"{api_base}/api/runtime/servers", headers=auth_headers)
server_names = {item.get("name") for item in servers.get("servers", [])}
if server_name not in server_names or oauth_server_name not in server_names:
    raise AssertionError(f"runtime servers missing expected entries: {servers}")
grants = expect_json(f"{api_base}/api/runtime/grants", headers=auth_headers)
grant_names = {item.get("name") for item in grants.get("grants", [])}
if grant_name not in grant_names:
    raise AssertionError(f"runtime grants missing {grant_name}: {grants}")
sessions = expect_json(f"{api_base}/api/runtime/sessions", headers=auth_headers)
session_names = {item.get("name") for item in sessions.get("sessions", [])}
if session_id not in session_names:
    raise AssertionError(f"runtime sessions missing {session_id}: {sessions}")
components = expect_json(f"{api_base}/api/runtime/components", headers=auth_headers)
component_keys = {item.get("key") for item in components.get("components", [])}
if not {"api", "gateway", "ui"}.issubset(component_keys):
    raise AssertionError(f"runtime components missing expected keys: {components}")
policy = expect_json(
    f"{api_base}/api/runtime/policy?namespace=mcp-servers&server={urllib.parse.quote(server_name)}",
    headers=auth_headers,
)
if policy.get("server", {}).get("name") != server_name:
    raise AssertionError(f"runtime policy missing server {server_name}: {policy}")

# Runtime mutation paths through the API.
disable = expect_json(
    f"{api_base}/api/runtime/grants/mcp-servers/{urllib.parse.quote(grant_name)}/disable",
    method="POST",
    headers=auth_headers,
)
if disable.get("disabled") is not True:
    raise AssertionError(f"grant disable response unexpected: {disable}")
enable = expect_json(
    f"{api_base}/api/runtime/grants/mcp-servers/{urllib.parse.quote(grant_name)}/enable",
    method="POST",
    headers=auth_headers,
)
if enable.get("disabled") is not False:
    raise AssertionError(f"grant enable response unexpected: {enable}")
revoke = expect_json(
    f"{api_base}/api/runtime/sessions/mcp-servers/{urllib.parse.quote(session_id)}/revoke",
    method="POST",
    headers=auth_headers,
)
if revoke.get("revoked") is not True:
    raise AssertionError(f"session revoke response unexpected: {revoke}")
unrevoke = expect_json(
    f"{api_base}/api/runtime/sessions/mcp-servers/{urllib.parse.quote(session_id)}/unrevoke",
    method="POST",
    headers=auth_headers,
)
if unrevoke.get("revoked") is not False:
    raise AssertionError(f"session unrevoke response unexpected: {unrevoke}")
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
    headers=auth_headers,
    body={
        "timestamp": "2026-03-29T00:00:00Z",
        "source": "e2e-direct-ingest",
        "event_type": "service.route.check",
        "payload": {"service": "ingest", "route": "/events"},
    },
)
if ingest_event.get("ok") is not True:
    raise AssertionError(f"ingest /events response unexpected: {ingest_event}")
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
  SERVER_NAME="${SERVER_NAME}" \
  OAUTH_SERVER_NAME="${OAUTH_SERVER_NAME}" \
  OAUTH_HUMAN_ID="${OAUTH_HUMAN_ID}" \
  OAUTH_AGENT_ID="${OAUTH_AGENT_ID}" \
  TEMPO_BASE="http://127.0.0.1:${TEMPO_PORT}" \
  LOKI_BASE="http://127.0.0.1:${LOKI_PORT}" \
  python3 <<'PY'
import json
import os
import time
import urllib.parse
import urllib.request

api_base = os.environ["API_BASE"]
api_key = os.environ["API_KEY"]
server_name = os.environ["SERVER_NAME"]
oauth_server_name = os.environ["OAUTH_SERVER_NAME"]
oauth_human_id = os.environ["OAUTH_HUMAN_ID"]
oauth_agent_id = os.environ["OAUTH_AGENT_ID"]
tempo_base = os.environ["TEMPO_BASE"]
loki_base = os.environ["LOKI_BASE"]


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
                return last
        except Exception as exc:
            last_error = exc
        time.sleep(delay)
    if last is not None:
        raise AssertionError(f"timed out waiting for {description}: {json.dumps(last, indent=2)}")
    if last_error is not None:
        raise last_error
    raise AssertionError(f"timed out waiting for {description}")


headers = {"x-api-key": api_key}

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
    lambda doc: {
        payload.get("rpc_method")
        for payload in (
            event.get("payload", {})
            for event in doc.get("events", [])
            if isinstance(event.get("payload"), dict)
        )
        if payload.get("rpc_method")
    } >= {
        "initialize",
        "notifications/initialized",
        "tools/list",
        "prompts/list",
        "resources/list",
        "prompts/get",
        "resources/read",
        "tools/call",
    },
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
    lambda doc: len(doc.get("events", [])) >= 8,
    headers=headers,
    description="server audit events",
).get("events", [])
sources = wait_for_json(
    f"{api_base}/sources",
    lambda doc: all(
        int(item.get("count", 0)) >= 1
        for item in doc.get("sources", [])
        if item.get("source") in {server_name, oauth_server_name, "mcp-example-server"}
    ) and {item.get("source") for item in doc.get("sources", [])} >= {server_name, oauth_server_name, "mcp-example-server"},
    headers=headers,
    description="analytics sources",
).get("sources", [])
event_types = wait_for_json(
    f"{api_base}/event-types",
    lambda doc: {item.get("event_type") for item in doc.get("event_types", [])} >= {"mcp.request", "tool.call", "resource.read", "prompt.render"},
    headers=headers,
    description="analytics event types",
).get("event_types", [])
stats = wait_for_json(
    f"{api_base}/stats",
    lambda doc: int(doc.get("events_total", 0)) >= 8,
    headers=headers,
    description="analytics stats",
)

def payload_dict(event):
    payload = event.get("payload", {})
    return payload if isinstance(payload, dict) else {}


routing_methods = {
    payload.get("rpc_method")
    for payload in (payload_dict(event) for event in all_server_events)
    if payload.get("rpc_method")
}
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
if deny_payload.get("reason") != "trust_too_low":
    raise AssertionError(f"unexpected deny payload: {deny_payload}")
if deny_payload.get("required_trust") != "medium":
    raise AssertionError(f"expected required_trust=medium, got {deny_payload}")
if deny_payload.get("effective_trust") != "low":
    raise AssertionError(f"expected effective_trust=low, got {deny_payload}")
if deny_echo_payload.get("reason") != "tool_denied":
    raise AssertionError(f"unexpected deny echo payload: {deny_echo_payload}")
if allow_payload.get("effective_trust") != "medium":
    raise AssertionError(f"expected effective_trust=medium after update, got {allow_payload}")
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
    if reason not in server_deny_reasons:
        raise AssertionError(f"missing server deny reason {reason}: {server_deny_reasons}")
for reason in ("missing_bearer_token", "invalid_token"):
    if reason not in oauth_deny_reasons:
        raise AssertionError(f"missing oauth deny reason {reason}: {oauth_deny_reasons}")
for rpc_method in (
    "initialize",
    "notifications/initialized",
    "tools/list",
    "prompts/list",
    "resources/list",
    "prompts/get",
    "resources/read",
    "tools/call",
):
    if rpc_method not in routing_methods:
        raise AssertionError(f"missing gateway audit event for {rpc_method}: {routing_methods}")
for rpc_method in (
    "initialize",
    "notifications/initialized",
    "tools/list",
    "prompts/list",
    "resources/list",
    "prompts/get",
    "resources/read",
    "tools/call",
):
    if rpc_method not in oauth_routing_methods:
        raise AssertionError(f"missing oauth gateway audit event for {rpc_method}: {oauth_routing_methods}")
if source_counts.get(server_name, 0) < 1:
    raise AssertionError(f"missing gateway source counts for {server_name}: {source_counts}")
if source_counts.get(oauth_server_name, 0) < 1:
    raise AssertionError(f"missing gateway source counts for {oauth_server_name}: {source_counts}")
if source_counts.get("mcp-example-server", 0) < 1:
    raise AssertionError(f"missing upstream server analytics source: {source_counts}")
for event_type in ("mcp.request", "tool.call", "resource.read", "prompt.render"):
    if event_type_counts.get(event_type, 0) < 1:
        raise AssertionError(f"missing analytics event type {event_type}: {event_type_counts}")
for status in (200, 401, 403):
    if status not in server_statuses:
        raise AssertionError(f"missing server audit status {status}: {server_statuses}")
if int(stats.get("events_total", 0)) < 8:
    raise AssertionError(f"expected at least 8 events after smoke and policy checks, got {stats}")
if oauth_allow_payload.get("human_id") != oauth_human_id or oauth_allow_payload.get("agent_id") != oauth_agent_id:
    raise AssertionError(f"unexpected oauth allow identity payload: {oauth_allow_payload}")

tempo = wait_for_json(
    f"{tempo_base}/api/search?limit=20",
    lambda doc: bool(doc.get("traces", [])),
    retries=60,
    delay=2,
    description="tempo traces",
)
traces = tempo.get("traces", [])

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
    ("analytics.source.server", str(source_counts.get("mcp-example-server", 0))),
    ("analytics.type.mcp.request", str(event_type_counts.get("mcp.request", 0))),
    ("analytics.type.tool.call", str(event_type_counts.get("tool.call", 0))),
    ("analytics.type.resource.read", str(event_type_counts.get("resource.read", 0))),
    ("analytics.type.prompt.render", str(event_type_counts.get("prompt.render", 0))),
    ("traces.tempo_found", str(len(traces))),
    ("logs.loki_streams", str(len(streams))),
]
width = max(len(k) for k, _ in rows)
print(f"{'check':{width}}  value")
print("-" * (width + 8))
for key, value in rows:
    print(f"{key:{width}}  {value}")
PY
fi

echo "[cli] deleting deployed MCP servers"
if scenario_selected "oauth"; then
  ./bin/mcp-runtime server delete "${OAUTH_SERVER_NAME}" --namespace mcp-servers
  kubectl wait --for=delete "mcpserver/${OAUTH_SERVER_NAME}" -n mcp-servers --timeout=120s || true
fi
./bin/mcp-runtime server delete "${SERVER_NAME}" --namespace mcp-servers
kubectl wait --for=delete "mcpserver/${SERVER_NAME}" -n mcp-servers --timeout=120s || true

echo "[done] E2E completed successfully"
