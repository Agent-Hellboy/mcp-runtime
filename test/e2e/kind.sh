#!/usr/bin/env bash
set -euo pipefail

# End-to-end check on a fresh kind cluster:
# - build the CLI and publish runtime/sentinel images to a local docker mirror registry
# - run `mcp-runtime setup --test-mode`
# - deploy a policy-enabled MCP server through the CLI pipeline flow
# - exercise the deployed server through `mcp-smoke-agent` plus targeted MCP requests
# - verify audit events plus trace/log backends

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"
echo "[info] Running from: ${PROJECT_ROOT}"

git config --global --add safe.directory "${PROJECT_ROOT}" >/dev/null 2>&1 || true

CLUSTER_NAME="${CLUSTER_NAME:-mcp-e2e}"
SERVER_NAME="${SERVER_NAME:-policy-mcp-server}"
SERVER_HOST="${SERVER_HOST:-policy.example.local}"
HUMAN_ID="${HUMAN_ID:-user-123}"
AGENT_ID="${AGENT_ID:-ops-agent}"
SESSION_ID="${SESSION_ID:-sess-ops-agent}"
TRAEFIK_PORT="${TRAEFIK_PORT:-18080}"
SENTINEL_PORT="${SENTINEL_PORT:-18083}"
TEMPO_PORT="${TEMPO_PORT:-13200}"
LOKI_PORT="${LOKI_PORT:-13100}"
MCP_SMOKE_DIR="${MCP_SMOKE_DIR:-}"
MCP_SMOKE_REF="${MCP_SMOKE_REF:-v0.3.0}"
MCP_SMOKE_REPO_URL="${MCP_SMOKE_REPO_URL:-https://github.com/Agent-Hellboy/mcp-smoke}"
MCP_SMOKE_TIMEOUT="${MCP_SMOKE_TIMEOUT:-20s}"
MCP_SMOKE_ANON_PORT="${MCP_SMOKE_ANON_PORT:-18084}"
MCP_SMOKE_IDENTITY_PORT="${MCP_SMOKE_IDENTITY_PORT:-18085}"
MCP_SMOKE_SESSION_PORT="${MCP_SMOKE_SESSION_PORT:-18086}"
MCP_PROTOCOL_VERSION="${MCP_PROTOCOL_VERSION:-2025-06-18}"
MCP_POLICY_WAIT_TRIES="${MCP_POLICY_WAIT_TRIES:-90}"
MCP_SMOKE_AGENT_ENV_FILE="${MCP_SMOKE_AGENT_ENV_FILE:-.env}"
MCP_SMOKE_AGENT_PROMPT="${MCP_SMOKE_AGENT_PROMPT:-Use the MCP upper tool to convert the exact word governance to uppercase. Reply with only the uppercase result.}"
MCP_SMOKE_AGENT_PROVIDER="${MCP_SMOKE_AGENT_PROVIDER:-}"
MCP_SMOKE_AGENT_MODEL="${MCP_SMOKE_AGENT_MODEL:-}"
MCP_SMOKE_AGENT_TIMEOUT="${MCP_SMOKE_AGENT_TIMEOUT:-90s}"
TEST_MODE_REGISTRY_IMAGE="${TEST_MODE_REGISTRY_IMAGE:-docker.io/library/mcp-runtime-registry:latest}"
LOCAL_REGISTRY_NAME="${LOCAL_REGISTRY_NAME:-${CLUSTER_NAME}-dockerhub-mirror}"
LOCAL_REGISTRY_PORT="${LOCAL_REGISTRY_PORT:-5001}"
LOCAL_REGISTRY_PUSH_HOST="${LOCAL_REGISTRY_PUSH_HOST:-127.0.0.1:${LOCAL_REGISTRY_PORT}}"
LOCAL_REGISTRY_MIRROR_ENDPOINT="${LOCAL_REGISTRY_NAME}:5000"
E2E_ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-}"

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
    tool_step = steps["tools/call"]
    if tool_step.get("ok"):
        raise AssertionError(f"{name}: expected tools/call to fail: {json.dumps(tool_step, indent=2)}")
    if expected_tool_error and expected_tool_error not in tool_step.get("error", ""):
        raise AssertionError(
            f"{name}: expected tools/call error to contain {expected_tool_error!r}, got {tool_step.get('error')!r}"
        )
    for step_name in ("prompts/get", "resources/read"):
        step = steps[step_name]
        if not step.get("ok") and not step.get("skipped"):
            raise AssertionError(f"{name}: expected {step_name} to succeed or skip: {json.dumps(step, indent=2)}")

rows = []
for step_name in required_steps:
    step = steps[step_name]
    status = "ok" if step.get("ok") else "skip" if step.get("skipped") else "fail"
    error = step.get("error", "")
    if error:
        status = f"{status} ({error})"
    rows.append((step_name, status))

width = max(len(step_name) for step_name, _ in rows)
print(f"{name}:")
print(f"  exit code{' ' * (width - len('exit code'))}  {smoke_exit_code}")
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
  local tries="${1:-60}"
  local i
  for i in $(seq 1 "${tries}"); do
    local deployment_ready
    local gateway_ready
    local policy_ready
    local service_ready
    deployment_ready="$(kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o jsonpath='{.status.deploymentReady}' 2>/dev/null || true)"
    gateway_ready="$(kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o jsonpath='{.status.gatewayReady}' 2>/dev/null || true)"
    policy_ready="$(kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o jsonpath='{.status.policyReady}' 2>/dev/null || true)"
    service_ready="$(kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o jsonpath='{.status.serviceReady}' 2>/dev/null || true)"
    if [[ "${deployment_ready}" == "true" && "${gateway_ready}" == "true" && "${policy_ready}" == "true" && "${service_ready}" == "true" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for MCPServer ${SERVER_NAME} to report service/deployment/gateway/policy readiness" >&2
  kubectl get mcpserver "${SERVER_NAME}" -n mcp-servers -o yaml || true
  return 1
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
build_and_publish_image "docker.io/library/mcp-runtime-operator:latest" "Dockerfile.operator" "."
build_and_publish_image "${TEST_MODE_REGISTRY_IMAGE}" "test/e2e/registry.Dockerfile" "."
build_and_publish_image "docker.io/library/mcp-sentinel-mcp-proxy:latest" "mcp-sentinel/services/mcp-proxy/Dockerfile" "mcp-sentinel/services/mcp-proxy"
build_and_publish_image "docker.io/library/mcp-sentinel-ingest:latest" "mcp-sentinel/services/ingest/Dockerfile" "mcp-sentinel/services/ingest"
build_and_publish_image "docker.io/library/mcp-sentinel-api:latest" "mcp-sentinel/services/api/Dockerfile" "mcp-sentinel/services/api"
build_and_publish_image "docker.io/library/mcp-sentinel-processor:latest" "mcp-sentinel/services/processor/Dockerfile" "mcp-sentinel/services/processor"
build_and_publish_image "docker.io/library/mcp-sentinel-ui:latest" "mcp-sentinel/services/ui/Dockerfile" "mcp-sentinel/services/ui"

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
  --dockerfile mcp-sentinel/services/mcp-server/Dockerfile \
  --registry docker.io/library \
  --tag latest \
  --context mcp-sentinel/services/mcp-server

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

echo "[port-forward] exposing ingress and observability services"
port_forward_bg traefik traefik "${TRAEFIK_PORT}" 8000 "${WORKDIR}/traefik-port-forward.log"
port_forward_bg mcp-sentinel mcp-sentinel-gateway "${SENTINEL_PORT}" 8083 "${WORKDIR}/sentinel-port-forward.log"
port_forward_bg mcp-sentinel tempo "${TEMPO_PORT}" 3200 "${WORKDIR}/tempo-port-forward.log"
port_forward_bg mcp-sentinel loki "${LOKI_PORT}" 3100 "${WORKDIR}/loki-port-forward.log"

wait_port "${TRAEFIK_PORT}"
wait_port "${SENTINEL_PORT}"
wait_port "${TEMPO_PORT}"
wait_port "${LOKI_PORT}"
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

wait_port "${MCP_SMOKE_ANON_PORT}"
wait_port "${MCP_SMOKE_IDENTITY_PORT}"
wait_port "${MCP_SMOKE_SESSION_PORT}"

MCP_INGRESS_PATH="/${SERVER_NAME}/mcp"
MCP_ANON_URL="http://127.0.0.1:${MCP_SMOKE_ANON_PORT}${MCP_INGRESS_PATH}"
MCP_IDENTITY_URL="http://127.0.0.1:${MCP_SMOKE_IDENTITY_PORT}${MCP_INGRESS_PATH}"
MCP_SESSION_URL="http://127.0.0.1:${MCP_SMOKE_SESSION_PORT}${MCP_INGRESS_PATH}"

echo "[mcp] running external mcp-smoke smoke checks against ingress"
run_mcp_smoke_expect "mcp-smoke-missing-identity" "${MCP_ANON_URL}" false "missing_identity"
run_mcp_smoke_expect "mcp-smoke-missing-session" "${MCP_IDENTITY_URL}" false "missing_session"
echo "[mcp] waiting for session-backed allow policy to reach the gateway"
wait_for_mcp_tool_result "${MCP_SESSION_URL}" "aaa-ping" '{}' 200
run_mcp_smoke_expect "mcp-smoke-allow-aaa-ping" "${MCP_SESSION_URL}" true

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

echo "[observe] validating audit, traces, and logs"
API_BASE="http://127.0.0.1:${SENTINEL_PORT}/api" \
API_KEY="${API_KEY}" \
SERVER_NAME="${SERVER_NAME}" \
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
    f"{api_base}/events/filter?server={server_name}&decision=deny&tool_name=aaa-ping&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="deny audit event for aaa-ping",
).get("events", [])
allow_upper = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&decision=allow&tool_name=upper&limit=20",
    lambda doc: bool(doc.get("events", [])),
    headers=headers,
    description="allow audit event for upper",
).get("events", [])
all_server_events = wait_for_json(
    f"{api_base}/events/filter?server={server_name}&limit=250",
    lambda doc: len(doc.get("events", [])) >= 8,
    headers=headers,
    description="server audit events",
).get("events", [])
sources = wait_for_json(
    f"{api_base}/sources",
    lambda doc: all(
        int(item.get("count", 0)) >= 1
        for item in doc.get("sources", [])
        if item.get("source") in {server_name, "mcp-example-server"}
    ) and {item.get("source") for item in doc.get("sources", [])} >= {server_name, "mcp-example-server"},
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

deny_payload = deny_upper[0].get("payload", {})
deny_echo_payload = deny_echo[0].get("payload", {})
allow_payload = allow_upper[0].get("payload", {})
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
for rpc_method in ("initialize", "tools/list", "tools/call"):
    if rpc_method not in routing_methods:
        raise AssertionError(f"missing gateway audit event for {rpc_method}: {routing_methods}")
if source_counts.get(server_name, 0) < 1:
    raise AssertionError(f"missing gateway source counts for {server_name}: {source_counts}")
if source_counts.get("mcp-example-server", 0) < 1:
    raise AssertionError(f"missing upstream server analytics source: {source_counts}")
for event_type in ("mcp.request", "tool.call", "resource.read", "prompt.render"):
    if event_type_counts.get(event_type, 0) < 1:
        raise AssertionError(f"missing analytics event type {event_type}: {event_type_counts}")
if int(stats.get("events_total", 0)) < 8:
    raise AssertionError(f"expected at least 8 events after smoke and policy checks, got {stats}")

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
    ("audit.rpc_methods", str(len(routing_methods))),
    ("analytics.source.gateway", str(source_counts.get(server_name, 0))),
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

echo "[cli] deleting deployed MCP server"
./bin/mcp-runtime server delete "${SERVER_NAME}" --namespace mcp-servers
kubectl wait --for=delete "mcpserver/${SERVER_NAME}" -n mcp-servers --timeout=120s || true

echo "[done] E2E completed successfully"
