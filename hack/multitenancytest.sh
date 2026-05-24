#!/usr/bin/env bash
set -euo pipefail

# End-to-end multi-tenant demo setup and verification (platform API only).
#
# This script intentionally does not use kubectl or KUBECONFIG. Admin credentials
# (ADMIN_TOKEN_INPUT or admin email/password) are only used to create teams and
# team users. Each team owner/member logs in with email/password — the same flow
# a normal user follows after signing in through the UI (API keys from the
# dashboard are optional; password login is enough for the CLI).
#
# Default flow:
#   1. admin: create/update Acme, Globex, and TechCorp teams + users
#   2. team users: build, push, and deploy tenant MCP servers via platform API
#   3. acme owner: apply cross-team grants for the cursor agent
#   4. globex/techcorp: adapter MCP calls, event checks, no-kubeconfig smoke
#
# Required environment (no production defaults — set explicitly for your cluster):
#   PLATFORM_URL   platform API base (test-mode port-forward: http://localhost:18080)
#   MCP_URL        public MCP ingress base (test-mode port-forward: http://localhost:18080)
#   REGISTRY_HOST  registry hostname:port for docker login and image push
#
# Optional test-mode admin bootstrap (matches setup --test-mode seed accounts):
#   ADMIN_EMAIL=admin@mcpruntime.org ADMIN_PASSWORD=admin@123
#
# Useful options:
#   RESET=1 hack/multitenancytest.sh       # delete demo resources via platform API
#   SKIP_SETUP=1 hack/multitenancytest.sh  # only run verification

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${BIN:-$ROOT_DIR/bin/mcp-runtime}"

require_env() {
  local name="$1"
  local hint="$2"
  if [[ -z "${!name:-}" ]]; then
    echo "${name} is required (${hint})" >&2
    exit 1
  fi
}

require_env PLATFORM_URL "e.g. http://localhost:18080 for test-mode port-forward"
require_env MCP_URL "e.g. http://localhost:18080 for test-mode port-forward"
require_env REGISTRY_HOST "registry hostname:port for docker login and image push"

PLATFORM_URL="${PLATFORM_URL%/}"
MCP_URL="${MCP_URL%/}"
MCP_HOST="${MCP_URL#https://}"
MCP_HOST="${MCP_HOST#http://}"
MCP_HOST="${MCP_HOST%%/*}"
MCP_RUNTIME_CONFIG_DIR="${MCP_RUNTIME_CONFIG_DIR:-$HOME/.mcpruntime}"
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
ADMIN_TOKEN_INPUT="${ADMIN_TOKEN_INPUT:-}"
ACME_EMAIL="${ACME_EMAIL:-acme-owner@example.com}"
ACME_PASSWORD="${ACME_PASSWORD:-acme-owner-123}"
GLOBEX_EMAIL="${GLOBEX_EMAIL:-globex-user@example.com}"
GLOBEX_PASSWORD="${GLOBEX_PASSWORD:-globex-user-123}"
TECHCORP_EMAIL="${TECHCORP_EMAIL:-techcorp-dev@example.com}"
TECHCORP_PASSWORD="${TECHCORP_PASSWORD:-techcorp-dev-123}"

ADMIN_PROFILE="${ADMIN_PROFILE:-admin}"
ACME_PROFILE="${ACME_PROFILE:-acme-owner}"
GLOBEX_PROFILE="${GLOBEX_PROFILE:-globex-user}"
TECHCORP_PROFILE="${TECHCORP_PROFILE:-techcorp-dev}"

ACME_SLUG="${ACME_SLUG:-acme}"
GLOBEX_SLUG="${GLOBEX_SLUG:-globex}"
TECHCORP_SLUG="${TECHCORP_SLUG:-techcorp}"
ACME_NS="${ACME_NS:-mcp-team-${ACME_SLUG}}"
GLOBEX_NS="${GLOBEX_NS:-mcp-team-${GLOBEX_SLUG}}"
TECHCORP_NS="${TECHCORP_NS:-mcp-team-${TECHCORP_SLUG}}"
ACME_SERVER="${ACME_SERVER:-acme-tools}"
GLOBEX_SERVER="${GLOBEX_SERVER:-globex-tools}"
TECHCORP_SERVER="${TECHCORP_SERVER:-techcorp-tools}"
AGENT_ID="${AGENT_ID:-cursor}"

# Force platform-API paths; never touch the local kubeconfig.
unset KUBECONFIG
export MCP_RUNTIME_CONFIG_DIR

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

stop_listen_port() {
  local listen="$1"
  local port="${listen##*:}"
  if [[ -z "$port" || "$port" == "$listen" ]]; then
    return 0
  fi
  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi
  local pids
  pids="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null || true)"
  if [[ -n "$pids" ]]; then
    kill $pids >/dev/null 2>&1 || true
    sleep 0.5
  fi
}

wait_for_adapter_proxy() {
  local listen="$1"
  local log_file="$2"
  for _ in {1..60}; do
    if [[ -f "$log_file" ]] && grep -q "listening on ${listen}" "$log_file"; then
      return 0
    fi
    sleep 0.25
  done
  echo "adapter proxy did not start on ${listen}" >&2
  [[ -f "$log_file" ]] && cat "$log_file" >&2
  return 1
}

run_as() {
  local profile="$1"
  shift
  KUBECONFIG="" MCP_PLATFORM_API_PROFILE="$profile" "$BIN" "$@"
}

json_escape() {
  jq -Rn --arg v "$1" '$v'
}

profile_token() {
  local profile="$1"
  jq -er --arg profile "$profile" '.accounts[$profile].token // (select(.current == $profile) | .token) // empty' "$CREDS"
}

admin_login() {
  if [[ -n "${ADMIN_TOKEN_INPUT}" ]]; then
    KUBECONFIG="" "$BIN" auth login --api-url "$PLATFORM_URL" --token "$ADMIN_TOKEN_INPUT" --profile "$ADMIN_PROFILE"
  else
    KUBECONFIG="" "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" --profile "$ADMIN_PROFILE"
  fi
}

team_user_login() {
  local profile="$1"
  local email="$2"
  local password="$3"
  KUBECONFIG="" "$BIN" auth login --api-url "$PLATFORM_URL" --username "$email" --password "$password" --profile "$profile"
}

team_id() {
  local slug="$1"
  local token="$2"
  curl -fsS \
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
  local email="$5"
  local password="$6"

  rm -rf "$manifest_dir"

  MCP_REGISTRY_INGRESS_HOST="$REGISTRY_HOST" run_as "$profile" server build image "$server" \
    --metadata-file "$metadata_file" \
    --dockerfile "$SERVER_DOCKERFILE" \
    --context "$SERVER_CONTEXT" \
    --tag "$TAG"

  echo "Logging into registry ${REGISTRY_HOST} as ${email}..."
  echo "$password" | docker login "$REGISTRY_HOST" -u "$email" --password-stdin

  local image_ref
  image_ref="$(image_ref_from_metadata "$metadata_file")"

  echo "Pushing image ${image_ref} as ${profile}..."
  KUBECONFIG="" MCP_REGISTRY_INGRESS_HOST="$REGISTRY_HOST" \
    run_as "$profile" registry push --scope tenant --image "$image_ref" --mode direct

  MCP_REGISTRY_INGRESS_HOST="$REGISTRY_HOST" MCP_REGISTRY_PULL_HOST="$REGISTRY_HOST" \
    KUBECONFIG="" "$BIN" pipeline generate --file "$metadata_file" --output "$manifest_dir"

  run_as "$profile" pipeline deploy --dir "$manifest_dir"
}

write_grant() {
  local file="$1"
  local grant_name="$2"
  local server_name="$3"
  local server_ns="$4"
  local subject_team_id="$5"
  cat >"$file" <<YAML
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${grant_name}
  namespace: ${server_ns}
spec:
  serverRef: {name: ${server_name}, namespace: ${server_ns}}
  subject: {teamID: "${subject_team_id}", agentID: ${AGENT_ID}}
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
  local profile="$1"
  local namespace="$2"
  local server="$3"
  local token
  token="$(profile_token "$profile")"
  echo "=== waiting for rollout: ${server} in ${namespace} (profile ${profile}) ==="
  local deadline=$(( $(date +%s) + 180 ))
  while true; do
    local body
    body="$(curl -fsS \
      -H "authorization: Bearer ${token}" \
      "${PLATFORM_URL}/api/runtime/servers/${namespace}/${server}" 2>/dev/null || echo '{}')"
    local ready_str
    ready_str="$(echo "$body" | jq -r '.server.ready // "0/0"' 2>/dev/null || echo "0/0")"
    local ready_count total_count
    ready_count="${ready_str%%/*}"
    total_count="${ready_str##*/}"
    if [[ "$ready_count" =~ ^[0-9]+$ && "$total_count" =~ ^[0-9]+$ && "$total_count" -ge 1 && "$ready_count" -ge "$total_count" ]]; then
      echo "rollout complete: ${server}"
      return 0
    fi
    if [[ $(date +%s) -gt $deadline ]]; then
      echo "timeout waiting for rollout: ${server}" >&2
      echo "last response: $body" >&2
      return 1
    fi
    sleep 5
  done
}

delete_all_sessions() {
  local profile="$1"
  local ns="$2"
  local token
  token="$(profile_token "$profile")"
  local sessions_body
  sessions_body="$(curl -fsS \
    -H "authorization: Bearer ${token}" \
    "${PLATFORM_URL}/api/runtime/sessions?namespace=${ns}" 2>/dev/null || echo '{"sessions":[]}')"
  local names
  names="$(echo "$sessions_body" | jq -r '(.sessions // .) | .[].name' 2>/dev/null || true)"
  for name in $names; do
    [[ -z "$name" ]] && continue
    curl -fsS -X DELETE \
      -H "authorization: Bearer ${token}" \
      "${PLATFORM_URL}/api/runtime/sessions/${ns}/${name}" >/dev/null 2>&1 || true
  done
}

verify_grant_exists() {
  local profile="$1"
  local name="$2"
  local ns="$3"
  local token
  token="$(profile_token "$profile")"
  curl -fsS \
    -H "authorization: Bearer ${token}" \
    "${PLATFORM_URL}/api/runtime/grants/${ns}/${name}" >/dev/null
}

setup_demo() {
  if [[ ! -f "$SERVER_DOCKERFILE" ]]; then
    echo "server Dockerfile not found: $SERVER_DOCKERFILE" >&2
    exit 1
  fi

  admin_login

  if [[ "${RESET:-0}" == "1" ]]; then
    local _token
    _token="$(profile_token "$ADMIN_PROFILE")"
    delete_all_sessions "$ADMIN_PROFILE" "$ACME_NS"
    for _ns_name in "${ACME_NS}/${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "${ACME_NS}/${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}"; do
      local _ns="${_ns_name%%/*}" _name="${_ns_name##*/}"
      curl -fsS -X DELETE -H "authorization: Bearer ${_token}" "${PLATFORM_URL}/api/runtime/grants/${_ns}/${_name}" >/dev/null 2>&1 || true
    done
    for _ns_name in "${ACME_NS}/${ACME_SERVER}" "${GLOBEX_NS}/${GLOBEX_SERVER}" "${TECHCORP_NS}/${TECHCORP_SERVER}"; do
      local _ns="${_ns_name%%/*}" _name="${_ns_name##*/}"
      curl -fsS -X DELETE -H "authorization: Bearer ${_token}" "${PLATFORM_URL}/api/runtime/servers/${_ns}/${_name}" >/dev/null 2>&1 || true
    done
  fi

  create_or_update_team "$ACME_SLUG" "Acme"
  create_or_update_team "$GLOBEX_SLUG" "Globex"
  create_or_update_team "$TECHCORP_SLUG" "TechCorp"
  run_as "$ADMIN_PROFILE" team user create "$ACME_SLUG" --username "$ACME_EMAIL" --password "$ACME_PASSWORD" --role owner
  run_as "$ADMIN_PROFILE" team user create "$GLOBEX_SLUG" --username "$GLOBEX_EMAIL" --password "$GLOBEX_PASSWORD" --role member
  run_as "$ADMIN_PROFILE" team user create "$TECHCORP_SLUG" --username "$TECHCORP_EMAIL" --password "$TECHCORP_PASSWORD" --role member

  ADMIN_TOKEN="$(profile_token "$ADMIN_PROFILE")"
  ACME_TEAM_ID="$(team_id "$ACME_SLUG" "$ADMIN_TOKEN")"
  GLOBEX_TEAM_ID="$(team_id "$GLOBEX_SLUG" "$ADMIN_TOKEN")"
  TECHCORP_TEAM_ID="$(team_id "$TECHCORP_SLUG" "$ADMIN_TOKEN")"
  printf "acme=%s\nglobex=%s\ntechcorp=%s\n" "$ACME_TEAM_ID" "$GLOBEX_TEAM_ID" "$TECHCORP_TEAM_ID"

  team_user_login "$ACME_PROFILE" "$ACME_EMAIL" "$ACME_PASSWORD"
  team_user_login "$GLOBEX_PROFILE" "$GLOBEX_EMAIL" "$GLOBEX_PASSWORD"
  team_user_login "$TECHCORP_PROFILE" "$TECHCORP_EMAIL" "$TECHCORP_PASSWORD"

  local acme_metadata="$WORK_DIR/${ACME_SERVER}.metadata.yaml"
  local globex_metadata="$WORK_DIR/${GLOBEX_SERVER}.metadata.yaml"
  local techcorp_metadata="$WORK_DIR/${TECHCORP_SERVER}.metadata.yaml"
  local acme_manifests="$WORK_DIR/${ACME_SERVER}-manifests"
  local globex_manifests="$WORK_DIR/${GLOBEX_SERVER}-manifests"
  local techcorp_manifests="$WORK_DIR/${TECHCORP_SERVER}-manifests"
  local acme_globex_grant="$WORK_DIR/${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}.yaml"
  local acme_techcorp_grant="$WORK_DIR/${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}.yaml"

  write_metadata "$acme_metadata" "$ACME_SERVER" "$ACME_NS" "$ACME_TEAM_ID"
  write_metadata "$globex_metadata" "$GLOBEX_SERVER" "$GLOBEX_NS" "$GLOBEX_TEAM_ID"
  write_metadata "$techcorp_metadata" "$TECHCORP_SERVER" "$TECHCORP_NS" "$TECHCORP_TEAM_ID"

  publish_server "$ACME_PROFILE" "$ACME_SERVER" "$acme_metadata" "$acme_manifests" "$ACME_EMAIL" "$ACME_PASSWORD"
  publish_server "$GLOBEX_PROFILE" "$GLOBEX_SERVER" "$globex_metadata" "$globex_manifests" "$GLOBEX_EMAIL" "$GLOBEX_PASSWORD"
  publish_server "$TECHCORP_PROFILE" "$TECHCORP_SERVER" "$techcorp_metadata" "$techcorp_manifests" "$TECHCORP_EMAIL" "$TECHCORP_PASSWORD"

  wait_for_rollout "$ACME_PROFILE" "$ACME_NS" "$ACME_SERVER"
  wait_for_rollout "$GLOBEX_PROFILE" "$GLOBEX_NS" "$GLOBEX_SERVER"
  wait_for_rollout "$TECHCORP_PROFILE" "$TECHCORP_NS" "$TECHCORP_SERVER"

  write_grant "$acme_globex_grant" "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "$ACME_SERVER" "$ACME_NS" "$GLOBEX_TEAM_ID"
  write_grant "$acme_techcorp_grant" "${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}" "$ACME_SERVER" "$ACME_NS" "$TECHCORP_TEAM_ID"
  run_as "$ACME_PROFILE" access grant apply --file "$acme_globex_grant"
  run_as "$ACME_PROFILE" access grant apply --file "$acme_techcorp_grant"
}

verify_no_kubeconfig_ops() {
  echo "=== verify: binary works without KUBECONFIG for non-cluster operations ==="
  local tmp_dir
  tmp_dir="$(mktemp -d)"
  local tmp_creds="$tmp_dir/config.json"

  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth login --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" team --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" registry --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" adapter --help >/dev/null

  local login_ok=0
  if [[ -n "${ADMIN_TOKEN_INPUT}" ]]; then
    KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" \
      "$BIN" auth login --api-url "$PLATFORM_URL" --token "$ADMIN_TOKEN_INPUT" --profile "nokubeconfig-admin" && login_ok=1
  else
    KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" \
      "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" \
      --profile "nokubeconfig-admin" && login_ok=1
  fi
  if [[ "$login_ok" != "1" ]]; then
    echo "auth login without KUBECONFIG failed" >&2
    exit 1
  fi

  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth status >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth logout >/dev/null

  rm -rf "$tmp_dir"
  echo "=== no-kubeconfig ops: OK ==="
}

precreate_adapter_session() {
  local profile="$1"
  local token body
  token="$(profile_token "$profile")"
  body="$(curl -fsS \
    -H "authorization: Bearer ${token}" \
    -H "content-type: application/json" \
    --data "{\"serverName\":\"${ACME_SERVER}\",\"namespace\":\"${ACME_NS}\",\"agentID\":\"${AGENT_ID}\"}" \
    "${PLATFORM_URL}/api/runtime/adapter/sessions")"
  jq -er '.name' <<<"$body" >/dev/null
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
  if [[ "$status" != "401" && "$status" != "403" ]] || ! jq -e '.adapter_required == true' "$body" >/dev/null; then
    echo "expected direct public call to fail with adapter_required (401 or 403), got HTTP ${status}" >&2
    cat "$body" >&2
    exit 1
  fi
}

adapter_call_add_for() {
  local profile="$1"
  local listen="$2"
  local log_file="$3"
  local headers_file="$4"
  local init_body="$5"
  local notify_body="$6"
  local result_body="$7"
  local arg_a="${8}"
  local arg_b="${9}"
  local expected="${10}"
  local adapter_url="http://${listen}"

  stop_listen_port "$listen"
  : >"$log_file"
  MCP_PLATFORM_API_PROFILE="$profile" KUBECONFIG="" "$BIN" adapter proxy \
    --platform-url "$PLATFORM_URL" \
    --runtime-url "${MCP_URL}/${ACME_SERVER}/mcp" \
    --server "$ACME_SERVER" \
    --namespace "$ACME_NS" \
    --agent "$AGENT_ID" \
    --listen "$listen" \
    --auto-refresh >>"$log_file" 2>&1 &
  local proxy_pid=$!

  wait_for_adapter_proxy "$listen" "$log_file"

  curl -fsS -D "$headers_file" -o "$init_body" \
    -H "content-type: application/json" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"multitenancytest","version":"0.1"}}}' \
    "$adapter_url"

  local mcp_session_id
  mcp_session_id="$(awk 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ {gsub(/\r/,"",$2); print $2}' "$headers_file")"
  if [[ -z "$mcp_session_id" ]]; then
    echo "initialize did not return Mcp-Session-Id for profile ${profile}" >&2
    cat "$init_body" >&2
    kill "$proxy_pid" >/dev/null 2>&1 || true
    stop_listen_port "$listen"
    return 1
  fi

  sleep 10

  local status deadline
  status="$(
    curl -sS -o "$notify_body" -w '%{http_code}' \
      -H "content-type: application/json" \
      -H "Mcp-Session-Id: ${mcp_session_id}" \
      --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
      "$adapter_url"
  )"
  if [[ "$status" != "200" && "$status" != "202" && "$status" != "204" ]]; then
    echo "notifications/initialized returned HTTP ${status} for profile ${profile}" >&2
    cat "$notify_body" >&2
    kill "$proxy_pid" >/dev/null 2>&1 || true
    stop_listen_port "$listen"
    return 1
  fi

  deadline=$(( $(date +%s) + 90 ))
  while true; do
    status="$(
      curl -sS -o "$result_body" -w '%{http_code}' \
        -H "content-type: application/json" \
        -H "Mcp-Session-Id: ${mcp_session_id}" \
        --data "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"tools/call\",\"params\":{\"name\":\"add\",\"arguments\":{\"a\":${arg_a},\"b\":${arg_b}}}}" \
        "$adapter_url"
    )"
    if [[ "$status" == "200" ]]; then
      break
    fi
    if grep -qE 'session_not_found|session_expired' "$result_body" 2>/dev/null; then
      if [[ $(date +%s) -gt $deadline ]]; then
        echo "tools/call timed out waiting for gateway session policy for profile ${profile}" >&2
        cat "$result_body" >&2
        kill "$proxy_pid" >/dev/null 2>&1 || true
        stop_listen_port "$listen"
        return 1
      fi
      sleep 3
      continue
    fi
    echo "tools/call returned HTTP ${status} for profile ${profile}" >&2
    cat "$result_body" >&2
    kill "$proxy_pid" >/dev/null 2>&1 || true
    stop_listen_port "$listen"
    return 1
  done

  local result
  result="$(jq -er '.result.content[0].text' "$result_body")"
  if [[ "$result" != "$expected" ]]; then
    echo "adapter add result was ${result}, expected ${expected} for profile ${profile}" >&2
    cat "$result_body" >&2
    kill "$proxy_pid" >/dev/null 2>&1 || true
    stop_listen_port "$listen"
    return 1
  fi

  kill "$proxy_pid" >/dev/null 2>&1 || true
  stop_listen_port "$listen"
}

adapter_call_add() {
  adapter_call_add_for "$GLOBEX_PROFILE" "$ADAPTER_LISTEN" \
    "$WORK_DIR/adapter.log" "$WORK_DIR/adapter.headers" \
    "$WORK_DIR/adapter-init.body" "$WORK_DIR/adapter-notify.body" "$WORK_DIR/adapter-add.body" \
    7 9 16
}

verify_events() {
  local token
  token="$(profile_token "$ACME_PROFILE")"
  local body="$WORK_DIR/events.body"
  local deadline=$(( $(date +%s) + 60 ))
  while true; do
    curl -fsS -o "$body" \
      -H "authorization: Bearer ${token}" \
      "${PLATFORM_URL}/api/runtime/server-events?namespace=${ACME_NS}&server=${ACME_SERVER}&limit=10"
    if jq -e --arg globex "$GLOBEX_TEAM_ID" '
      (.events // .) as $events
      | ($events | length) > 0
      and any($events[]; (.tool_name // .ToolName // "") == "add" and
          ((.payload.subject_team_id // .subject_team_id // .team_id // "") == $globex)
          and (.decision // .Decision // "allow") == "allow")
    ' "$body" >/dev/null 2>&1; then
      return 0
    fi
    if [[ $(date +%s) -gt $deadline ]]; then
      echo "expected allow add event for globex team in server events" >&2
      cat "$body" >&2
      return 1
    fi
    sleep 3
  done
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

if [[ ! -x "$BIN" ]]; then
  echo "missing binary: $BIN" >&2
  echo "build it first with: go build -o bin/mcp-runtime ./cmd/mcp-runtime" >&2
  exit 1
fi

mkdir -p "$MCP_RUNTIME_CONFIG_DIR" "$WORK_DIR"
stop_listen_port "$ADAPTER_LISTEN"
stop_listen_port "${TECHCORP_ADAPTER_LISTEN:-127.0.0.1:8300}"

if [[ "${SKIP_SETUP:-0}" != "1" ]]; then
  setup_demo
else
  admin_login
  team_user_login "$ACME_PROFILE" "$ACME_EMAIL" "$ACME_PASSWORD"
  team_user_login "$GLOBEX_PROFILE" "$GLOBEX_EMAIL" "$GLOBEX_PASSWORD"
  team_user_login "$TECHCORP_PROFILE" "$TECHCORP_EMAIL" "$TECHCORP_PASSWORD"
  ADMIN_TOKEN="$(profile_token "$ADMIN_PROFILE")"
  ACME_TEAM_ID="$(team_id "$ACME_SLUG" "$ADMIN_TOKEN")"
  GLOBEX_TEAM_ID="$(team_id "$GLOBEX_SLUG" "$ADMIN_TOKEN")"
  TECHCORP_TEAM_ID="$(team_id "$TECHCORP_SLUG" "$ADMIN_TOKEN")"
fi

wait_for_rollout "$ACME_PROFILE" "$ACME_NS" "$ACME_SERVER"
wait_for_rollout "$GLOBEX_PROFILE" "$GLOBEX_NS" "$GLOBEX_SERVER"
wait_for_rollout "$TECHCORP_PROFILE" "$TECHCORP_NS" "$TECHCORP_SERVER"
verify_grant_exists "$ACME_PROFILE" "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "$ACME_NS"
verify_grant_exists "$ACME_PROFILE" "${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}" "$ACME_NS"

verify_no_kubeconfig_ops

delete_all_sessions "$ACME_PROFILE" "$ACME_NS"

verify_direct_public_denied

echo "=== verify: ${GLOBEX_SLUG} adapter call to ${ACME_SLUG}/${ACME_SERVER} ==="
precreate_adapter_session "$GLOBEX_PROFILE"
adapter_call_add
verify_events

echo "=== verify: ${TECHCORP_SLUG} adapter call to ${ACME_SLUG}/${ACME_SERVER} ==="
precreate_adapter_session "$TECHCORP_PROFILE"
sleep 10
TECHCORP_ADAPTER_LISTEN="${TECHCORP_ADAPTER_LISTEN:-127.0.0.1:8300}"
adapter_call_add_for "$TECHCORP_PROFILE" "$TECHCORP_ADAPTER_LISTEN" \
  "$WORK_DIR/techcorp-adapter.log" "$WORK_DIR/techcorp-adapter.headers" \
  "$WORK_DIR/techcorp-init.body" "$WORK_DIR/techcorp-notify.body" "$WORK_DIR/techcorp-add.body" \
  11 22 33
echo "=== techcorp adapter call: OK (11+22=33) ==="

print_cursor_config
echo
echo "multi-tenant flow passed (platform API only, no kubectl/kubeconfig):"
echo "  - ${GLOBEX_SLUG}/${AGENT_ID} called ${ACME_SLUG}/${ACME_SERVER} add(7,9)=16 via adapter"
echo "  - ${TECHCORP_SLUG}/${AGENT_ID} called ${ACME_SLUG}/${ACME_SERVER} add(11,22)=33 via adapter"
echo "  - admin credentials used only for team bootstrap; tenant flows used email/password login"
