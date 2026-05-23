#!/usr/bin/env bash
set -euo pipefail

# End-to-end multi-tenant demo setup and verification.
#
# Default flow:
#   1. create/update Acme and Globex teams/users
#   2. build, push, generate, and deploy acme-tools and globex-tools
#   3. apply the Acme -> Globex cursor grant
#   4. verify direct public MCP denial, adapter success, dashboard events, and cluster doctor
#
# Useful options:
#   RESET=1 hack/multitenancytest.sh       # delete demo MCP resources before setup
#   SKIP_SETUP=1 hack/multitenancytest.sh  # only run verification against existing resources

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${BIN:-$ROOT_DIR/bin/mcp-runtime}"
PLATFORM_URL="${PLATFORM_URL:-https://platform.mcpruntime.org}"
MCP_URL="${MCP_URL:-https://mcp.mcpruntime.org}"
PLATFORM_URL="${PLATFORM_URL%/}"
MCP_URL="${MCP_URL%/}"
MCP_HOST="${MCP_URL#https://}"
MCP_HOST="${MCP_HOST#http://}"
MCP_HOST="${MCP_HOST%%/*}"
MCP_RUNTIME_CONFIG_DIR="${MCP_RUNTIME_CONFIG_DIR:-$HOME/.mcpruntime}"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
CREDS="${MCP_RUNTIME_CONFIG_DIR}/config.json"
TMP_ROOT="${TMPDIR:-/tmp}"
TMP_ROOT="${TMP_ROOT%/}"
WORK_DIR="${WORK_DIR:-$TMP_ROOT/mcp-runtime-multitenancy}"
TAG="${TAG:-v0.1.0}"
ADAPTER_LISTEN="${ADAPTER_LISTEN:-127.0.0.1:8299}"

SERVER_CONTEXT="${SERVER_CONTEXT:-$ROOT_DIR/examples/workspace-assistant-mcp}"
SERVER_DOCKERFILE="${SERVER_DOCKERFILE:-$SERVER_CONTEXT/Dockerfile}"

ADMIN_EMAIL="${ADMIN_EMAIL:-admin@mcpruntime.org}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin@123}"
ACME_EMAIL="${ACME_EMAIL:-acme-owner@example.com}"
ACME_PASSWORD="${ACME_PASSWORD:-acme123}"
GLOBEX_EMAIL="${GLOBEX_EMAIL:-globex-user@example.com}"
GLOBEX_PASSWORD="${GLOBEX_PASSWORD:-globex123}"

ADMIN_PROFILE="${ADMIN_PROFILE:-admin}"
ACME_PROFILE="${ACME_PROFILE:-acme-owner}"
GLOBEX_PROFILE="${GLOBEX_PROFILE:-globex-user}"

ACME_SLUG="${ACME_SLUG:-acme}"
GLOBEX_SLUG="${GLOBEX_SLUG:-globex}"
ACME_NS="${ACME_NS:-mcp-team-${ACME_SLUG}}"
GLOBEX_NS="${GLOBEX_NS:-mcp-team-${GLOBEX_SLUG}}"
ACME_SERVER="${ACME_SERVER:-acme-tools}"
GLOBEX_SERVER="${GLOBEX_SERVER:-globex-tools}"
AGENT_ID="${AGENT_ID:-cursor}"

export KUBECONFIG
export MCP_RUNTIME_CONFIG_DIR

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

run_as() {
  local profile="$1"
  shift
  MCP_PLATFORM_API_PROFILE="$profile" "$BIN" "$@"
}

json_escape() {
  jq -Rn --arg v "$1" '$v'
}

profile_token() {
  local profile="$1"
  jq -er --arg profile "$profile" '.accounts[$profile].token // (select(.current == $profile) | .token) // empty' "$CREDS"
}

team_id() {
  local slug="$1"
  local token="$2"
  curl -fsS \
    -H "x-api-key: ${token}" \
    -H "authorization: Bearer ${token}" \
    "${PLATFORM_URL}/api/runtime/teams/${slug}" | jq -er '.team.id'
}

create_or_update_team() {
  local slug="$1"
  local name="$2"
  if ! run_as "$ADMIN_PROFILE" team create "$slug" --name "$name"; then
    echo "team ${slug} may already exist; continuing" >&2
  fi
}

write_metadata() {
  local file="$1"
  local server="$2"
  local namespace="$3"
  local team_id_value="$4"
  local route="/${server}/mcp"

  cat >"$file" <<YAML
version: v1
servers:
  - name: ${server}
    scope: tenant
    namespace: ${namespace}
    teamID: ${team_id_value}
    image: ${server}
    imageTag: ${TAG}
    route: ${route}
    publicPathPrefix: ${server}
    ingressHost: ${MCP_HOST}
    port: 8088
    envVars:
      - name: MCP_PATH
        value: ${route}
    tools:
      - {name: aaa-ping, requiredTrust: low, sideEffect: read}
      - {name: add, requiredTrust: low, sideEffect: read}
      - {name: upper, requiredTrust: low, sideEffect: read}
      - {name: lower, requiredTrust: low, sideEffect: read}
      - {name: echo, requiredTrust: low, sideEffect: read}
      - {name: slugify, requiredTrust: low, sideEffect: read}
    auth:
      mode: header
      humanIDHeader: X-MCP-Human-ID
      agentIDHeader: X-MCP-Agent-ID
      teamIDHeader: X-MCP-Team-ID
      sessionIDHeader: X-MCP-Agent-Session
    policy: {mode: allow-list, defaultDecision: deny, enforceOn: call_tool, policyVersion: v1}
    session: {required: true, store: kubernetes, headerName: X-MCP-Agent-Session}
    gateway: {enabled: true}
YAML
}

image_ref_from_metadata() {
  awk '$1=="image:"{i=$2} $1=="imageTag:"{t=$2} END{if(i=="" || t=="") exit 1; print i ":" t}' "$1"
}

publish_server() {
  local profile="$1"
  local server="$2"
  local metadata_file="$3"
  local manifest_dir="$4"

  rm -rf "$manifest_dir"
  run_as "$profile" server build image "$server" \
    --metadata-file "$metadata_file" \
    --dockerfile "$SERVER_DOCKERFILE" \
    --context "$SERVER_CONTEXT" \
    --tag "$TAG"

  local image_ref
  image_ref="$(image_ref_from_metadata "$metadata_file")"
  run_as "$profile" registry push --scope tenant --image "$image_ref"
  "$BIN" pipeline generate --file "$metadata_file" --output "$manifest_dir"
  "$BIN" pipeline deploy --dir "$manifest_dir"
}

write_grant() {
  local file="$1"
  cat >"$file" <<YAML
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}
  namespace: ${ACME_NS}
spec:
  serverRef: {name: ${ACME_SERVER}, namespace: ${ACME_NS}}
  subject: {teamID: "${GLOBEX_TEAM_ID}", agentID: ${AGENT_ID}}
  maxTrust: low
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - {name: aaa-ping, decision: allow, requiredTrust: low}
    - {name: add, decision: allow, requiredTrust: low}
    - {name: upper, decision: allow, requiredTrust: low}
    - {name: lower, decision: allow, requiredTrust: low}
    - {name: echo, decision: allow, requiredTrust: low}
    - {name: slugify, decision: allow, requiredTrust: low}
YAML
}

wait_for_rollout() {
  local namespace="$1"
  local server="$2"
  kubectl rollout status "deployment/${server}" -n "$namespace" --timeout=180s
}

setup_demo() {
  if [[ ! -f "$SERVER_DOCKERFILE" ]]; then
    echo "server Dockerfile not found: $SERVER_DOCKERFILE" >&2
    exit 1
  fi

  if [[ "${RESET:-0}" == "1" ]]; then
    kubectl delete mcpagentsession -n "$ACME_NS" --all --ignore-not-found
    kubectl delete mcpaccessgrant "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" -n "$ACME_NS" --ignore-not-found
    kubectl delete mcpserver "$ACME_SERVER" -n "$ACME_NS" --ignore-not-found
    kubectl delete mcpserver "$GLOBEX_SERVER" -n "$GLOBEX_NS" --ignore-not-found
    kubectl delete secret "${ACME_SERVER}-analytics-creds" -n "$ACME_NS" --ignore-not-found
    kubectl delete secret "${GLOBEX_SERVER}-analytics-creds" -n "$GLOBEX_NS" --ignore-not-found
  fi

  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" --profile "$ADMIN_PROFILE"
  create_or_update_team "$ACME_SLUG" "Acme"
  create_or_update_team "$GLOBEX_SLUG" "Globex"
  run_as "$ADMIN_PROFILE" team user create "$ACME_SLUG" --username "$ACME_EMAIL" --password "$ACME_PASSWORD" --role owner
  run_as "$ADMIN_PROFILE" team user create "$GLOBEX_SLUG" --username "$GLOBEX_EMAIL" --password "$GLOBEX_PASSWORD" --role member

  ADMIN_TOKEN="$(profile_token "$ADMIN_PROFILE")"
  ACME_TEAM_ID="$(team_id "$ACME_SLUG" "$ADMIN_TOKEN")"
  GLOBEX_TEAM_ID="$(team_id "$GLOBEX_SLUG" "$ADMIN_TOKEN")"
  printf "acme=%s\nglobex=%s\n" "$ACME_TEAM_ID" "$GLOBEX_TEAM_ID"

  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ACME_EMAIL" --password "$ACME_PASSWORD" --profile "$ACME_PROFILE"
  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$GLOBEX_EMAIL" --password "$GLOBEX_PASSWORD" --profile "$GLOBEX_PROFILE"

  local acme_metadata="$WORK_DIR/${ACME_SERVER}.metadata.yaml"
  local globex_metadata="$WORK_DIR/${GLOBEX_SERVER}.metadata.yaml"
  local acme_manifests="$WORK_DIR/${ACME_SERVER}-manifests"
  local globex_manifests="$WORK_DIR/${GLOBEX_SERVER}-manifests"
  local grant_file="$WORK_DIR/${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}.yaml"

  write_metadata "$acme_metadata" "$ACME_SERVER" "$ACME_NS" "$ACME_TEAM_ID"
  write_metadata "$globex_metadata" "$GLOBEX_SERVER" "$GLOBEX_NS" "$GLOBEX_TEAM_ID"

  publish_server "$ACME_PROFILE" "$ACME_SERVER" "$acme_metadata" "$acme_manifests"
  publish_server "$GLOBEX_PROFILE" "$GLOBEX_SERVER" "$globex_metadata" "$globex_manifests"

  wait_for_rollout "$ACME_NS" "$ACME_SERVER"
  wait_for_rollout "$GLOBEX_NS" "$GLOBEX_SERVER"

  write_grant "$grant_file"
  run_as "$ACME_PROFILE" access grant apply --file "$grant_file"
}

ensure_login() {
  local profile="$1"
  local email="$2"
  local password="$3"
  if [[ ! -f "$CREDS" ]] || ! jq -e --arg profile "$profile" '.accounts[$profile].token // empty' "$CREDS" >/dev/null 2>&1; then
    "$BIN" auth login --api-url "$PLATFORM_URL" --username "$email" --password "$password" --profile "$profile"
  fi
}

verify_direct_public_denied() {
  local body="$WORK_DIR/direct-public.body"
  local status
  status="$(
    curl -ksS -o "$body" -w '%{http_code}' \
      -H "content-type: application/json" \
      --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aaa-ping","arguments":{"note":"direct-public-deny-check"}}}' \
      "${MCP_URL}/${ACME_SERVER}/mcp"
  )"
  if [[ "$status" != "403" ]] || ! jq -e '.adapter_required == true' "$body" >/dev/null; then
    echo "expected direct public call to fail with adapter_required, got HTTP ${status}" >&2
    cat "$body" >&2
    exit 1
  fi
}

adapter_call_add() {
  local headers="$WORK_DIR/adapter.headers"
  local result_body="$WORK_DIR/adapter-add.body"
  local adapter_url="http://${ADAPTER_LISTEN}"

  MCP_PLATFORM_API_PROFILE="$GLOBEX_PROFILE" "$BIN" adapter proxy \
    --platform-url "$PLATFORM_URL" \
    --runtime-url "${MCP_URL}/${ACME_SERVER}/mcp" \
    --server "$ACME_SERVER" \
    --namespace "$ACME_NS" \
    --agent "$AGENT_ID" \
    --listen "$ADAPTER_LISTEN" \
    --auto-refresh >"$WORK_DIR/adapter.log" 2>&1 &
  local proxy_pid=$!
  trap 'kill "$proxy_pid" >/dev/null 2>&1 || true' RETURN

  for _ in {1..60}; do
    if curl -sS --max-time 1 -o /dev/null "$adapter_url" >/dev/null 2>&1; then
      break
    fi
    sleep 0.25
  done

  curl -fsS -D "$headers" -o "$WORK_DIR/adapter-init.body" \
    -H "content-type: application/json" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"multitenancytest","version":"0.1"}}}' \
    "$adapter_url" >/dev/null

  local mcp_session_id
  mcp_session_id="$(awk 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ {gsub(/\r/,"",$2); print $2}' "$headers")"
  test -n "$mcp_session_id"

  curl -fsS -o "$WORK_DIR/adapter-notify.body" \
    -H "content-type: application/json" \
    -H "Mcp-Session-Id: ${mcp_session_id}" \
    --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
    "$adapter_url" >/dev/null

  curl -fsS -o "$result_body" \
    -H "content-type: application/json" \
    -H "Mcp-Session-Id: ${mcp_session_id}" \
    --data '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"add","arguments":{"a":7,"b":9}}}' \
    "$adapter_url" >/dev/null

  local result
  result="$(jq -er '.result.content[0].text' "$result_body")"
  if [[ "$result" != "16" ]]; then
    echo "adapter add result was ${result}, expected 16" >&2
    cat "$result_body" >&2
    exit 1
  fi

  kill "$proxy_pid" >/dev/null 2>&1 || true
  trap - RETURN
}

verify_events() {
  local admin_token="$1"
  local body="$WORK_DIR/events.body"
  curl -fsS -o "$body" \
    -H "x-api-key: ${admin_token}" \
    -H "authorization: Bearer ${admin_token}" \
    "${PLATFORM_URL}/api/events/filter?server=${ACME_SERVER}&namespace=${ACME_NS}&tool_name=add&agent_id=${AGENT_ID}&limit=5"
  jq -e --arg globex "$GLOBEX_TEAM_ID" '
    (.events // .) as $events
    | ($events | length) > 0
    and any($events[]; .tool_name == "add" and (.payload.subject_team_id // .team_id // "") == $globex)
  ' "$body" >/dev/null
}

print_cursor_config() {
  local bin_json platform_json runtime_json profile_json server_json ns_json agent_json
  bin_json="$(json_escape "$BIN")"
  platform_json="$(json_escape "$PLATFORM_URL")"
  runtime_json="$(json_escape "${MCP_URL}/${ACME_SERVER}/mcp")"
  profile_json="$(json_escape "$GLOBEX_PROFILE")"
  server_json="$(json_escape "$ACME_SERVER")"
  ns_json="$(json_escape "$ACME_NS")"
  agent_json="$(json_escape "$AGENT_ID")"

  cat <<JSON

Cursor stdio config:
{
  "mcpServers": {
    "${ACME_SERVER}": {
      "command": ${bin_json},
      "args": [
        "adapter",
        "stdio",
        "--platform-url", ${platform_json},
        "--runtime-url", ${runtime_json},
        "--server", ${server_json},
        "--namespace", ${ns_json},
        "--agent", ${agent_json},
        "--auto-refresh"
      ],
      "env": {
        "MCP_PLATFORM_API_PROFILE": ${profile_json}
      }
    }
  }
}

HTTP adapter alternative:
MCP_PLATFORM_API_PROFILE=${GLOBEX_PROFILE} ${BIN} adapter proxy \\
  --platform-url ${PLATFORM_URL} \\
  --runtime-url ${MCP_URL}/${ACME_SERVER}/mcp \\
  --server ${ACME_SERVER} \\
  --namespace ${ACME_NS} \\
  --agent ${AGENT_ID} \\
  --listen ${ADAPTER_LISTEN} \\
  --auto-refresh

{
  "mcpServers": {
    "${ACME_SERVER}": {
      "type": "http",
      "url": "http://${ADAPTER_LISTEN}"
    }
  }
}
JSON
}

need curl
need docker
need jq
need kubectl

if [[ ! -x "$BIN" ]]; then
  echo "missing binary: $BIN" >&2
  echo "build it first with: go build -o bin/mcp-runtime ./cmd/mcp-runtime" >&2
  exit 1
fi

mkdir -p "$MCP_RUNTIME_CONFIG_DIR" "$WORK_DIR"

if [[ "${SKIP_SETUP:-0}" != "1" ]]; then
  setup_demo
else
  ensure_login "$ADMIN_PROFILE" "$ADMIN_EMAIL" "$ADMIN_PASSWORD"
  ensure_login "$GLOBEX_PROFILE" "$GLOBEX_EMAIL" "$GLOBEX_PASSWORD"
  ADMIN_TOKEN="$(profile_token "$ADMIN_PROFILE")"
  ACME_TEAM_ID="$(team_id "$ACME_SLUG" "$ADMIN_TOKEN")"
  GLOBEX_TEAM_ID="$(team_id "$GLOBEX_SLUG" "$ADMIN_TOKEN")"
fi

wait_for_rollout "$ACME_NS" "$ACME_SERVER"
wait_for_rollout "$GLOBEX_NS" "$GLOBEX_SERVER"
kubectl get mcpaccessgrant "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" -n "$ACME_NS" >/dev/null

verify_direct_public_denied
adapter_call_add
verify_events "$ADMIN_TOKEN"
"$BIN" cluster doctor

print_cursor_config
echo
echo "multi-tenant flow passed: ${GLOBEX_SLUG}/${AGENT_ID} called ${ACME_SLUG}/${ACME_SERVER} add(7,9)=16 through the adapter"
