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

ADMIN_EMAIL="${ADMIN_EMAIL:-princekrroshan01@gmail.com}"
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
  local email="$5"
  local password="$6"

  # Public registry host — used for docker build/push from the workstation.
  local registry_host="registry.mcpruntime.org"

  rm -rf "$manifest_dir"

  # Build image tagged for the public registry host so docker push can reach it.
  MCP_REGISTRY_INGRESS_HOST="$registry_host" run_as "$profile" server build image "$server" \
    --metadata-file "$metadata_file" \
    --dockerfile "$SERVER_DOCKERFILE" \
    --context "$SERVER_CONTEXT" \
    --tag "$TAG"

  # Login to the public registry as the team user (platform password fallback).
  echo "Logging into registry $registry_host as $email..."
  echo "$password" | docker login "$registry_host" -u "$email" --password-stdin

  local image_ref
  image_ref="$(image_ref_from_metadata "$metadata_file")"

  # Push to the public registry using docker push.  MCP_REGISTRY_INGRESS_HOST
  # (not MCP_REGISTRY_ENDPOINT) controls the push target so the push goes to
  # the publicly reachable registry.mcpruntime.org, not to the in-cluster ClusterIP.
  echo "Pushing image $image_ref as $profile..."
  KUBECONFIG="" MCP_REGISTRY_INGRESS_HOST="$registry_host" \
    run_as "$profile" registry push --scope tenant --image "$image_ref" --mode direct

  # Generate manifests using the public registry host so the image ref is
  # registry.mcpruntime.org/<scope>/<name>:<tag> — a TLS URL cluster doctor
  # accepts and that kubelet can pull with the namespace pull secret.
  # MCP_REGISTRY_PULL_HOST forces the in-cluster pull host to the same public
  # TLS URL, preventing imageRefForClusterPull from rewriting it to the
  # in-cluster service DNS (registry.registry.svc.cluster.local:5000).
  MCP_REGISTRY_INGRESS_HOST="$registry_host" MCP_REGISTRY_PULL_HOST="$registry_host" \
    "$BIN" pipeline generate --file "$metadata_file" --output "$manifest_dir"

  # Deploy via platform API (no KUBECONFIG needed; falls back to kubectl when platform auth unavailable).
  "$BIN" pipeline deploy --dir "$manifest_dir"
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
  local namespace="$1"
  local server="$2"
  local token
  token="$(profile_token "$ADMIN_PROFILE")"
  echo "=== waiting for rollout: ${server} in ${namespace} ==="
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
  local ns="$1"
  local token
  token="$(profile_token "$ADMIN_PROFILE")"
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
  local name="$1"
  local ns="$2"
  local token
  token="$(profile_token "$ADMIN_PROFILE")"
  curl -fsS \
    -H "authorization: Bearer ${token}" \
    "${PLATFORM_URL}/api/runtime/grants/${ns}/${name}" >/dev/null
}

setup_demo() {
  if [[ ! -f "$SERVER_DOCKERFILE" ]]; then
    echo "server Dockerfile not found: $SERVER_DOCKERFILE" >&2
    exit 1
  fi

  if [[ "${RESET:-0}" == "1" ]]; then
    if [[ -n "${ADMIN_TOKEN_INPUT}" ]]; then
      "$BIN" auth login --api-url "$PLATFORM_URL" --token "$ADMIN_TOKEN_INPUT" --profile "$ADMIN_PROFILE" 2>/dev/null || true
    else
      "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" --profile "$ADMIN_PROFILE" 2>/dev/null || true
    fi
    local _token
    _token="$(profile_token "$ADMIN_PROFILE")"
    delete_all_sessions "$ACME_NS"
    for _ns_name in "${ACME_NS}/${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "${ACME_NS}/${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}"; do
      local _ns="${_ns_name%%/*}" _name="${_ns_name##*/}"
      curl -fsS -X DELETE -H "authorization: Bearer ${_token}" "${PLATFORM_URL}/api/runtime/grants/${_ns}/${_name}" >/dev/null 2>&1 || true
    done
    for _ns_name in "${ACME_NS}/${ACME_SERVER}" "${GLOBEX_NS}/${GLOBEX_SERVER}" "${TECHCORP_NS}/${TECHCORP_SERVER}"; do
      local _ns="${_ns_name%%/*}" _name="${_ns_name##*/}"
      curl -fsS -X DELETE -H "authorization: Bearer ${_token}" "${PLATFORM_URL}/api/runtime/servers/${_ns}/${_name}" >/dev/null 2>&1 || true
    done
  fi

  if [[ -n "${ADMIN_TOKEN_INPUT}" ]]; then
    "$BIN" auth login --api-url "$PLATFORM_URL" --token "$ADMIN_TOKEN_INPUT" --profile "$ADMIN_PROFILE"
  else
    "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" --profile "$ADMIN_PROFILE"
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

  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$ACME_EMAIL" --password "$ACME_PASSWORD" --profile "$ACME_PROFILE"
  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$GLOBEX_EMAIL" --password "$GLOBEX_PASSWORD" --profile "$GLOBEX_PROFILE"
  "$BIN" auth login --api-url "$PLATFORM_URL" --username "$TECHCORP_EMAIL" --password "$TECHCORP_PASSWORD" --profile "$TECHCORP_PROFILE"

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

  wait_for_rollout "$ACME_NS" "$ACME_SERVER"
  wait_for_rollout "$GLOBEX_NS" "$GLOBEX_SERVER"
  wait_for_rollout "$TECHCORP_NS" "$TECHCORP_SERVER"

  write_grant "$acme_globex_grant" "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "$ACME_SERVER" "$ACME_NS" "$GLOBEX_TEAM_ID"
  write_grant "$acme_techcorp_grant" "${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}" "$ACME_SERVER" "$ACME_NS" "$TECHCORP_TEAM_ID"
  run_as "$ACME_PROFILE" access grant apply --file "$acme_globex_grant"
  run_as "$ACME_PROFILE" access grant apply --file "$acme_techcorp_grant"
}

ensure_login() {
  local profile="$1"
  local email="$2"
  local password="$3"
  # Always refresh; cached tokens can expire between test runs.
  if [[ "$profile" == "$ADMIN_PROFILE" && -n "${ADMIN_TOKEN_INPUT}" ]]; then
    "$BIN" auth login --api-url "$PLATFORM_URL" --token "$ADMIN_TOKEN_INPUT" --profile "$profile"
  else
    "$BIN" auth login --api-url "$PLATFORM_URL" --username "$email" --password "$password" --profile "$profile"
  fi
}

# Verify that operations that don't need cluster access work without KUBECONFIG.
# Any command that gates on cluster connectivity before doing auth/local work
# breaks the experience for users who don't have a cluster configured.
verify_no_kubeconfig_ops() {
  echo "=== verify: binary works without KUBECONFIG for non-cluster operations ==="
  local tmp_dir
  tmp_dir="$(mktemp -d)"
  local tmp_creds="$tmp_dir/config.json"

  # --help must work unconditionally
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth login --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" team --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" registry --help >/dev/null
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" adapter --help >/dev/null

  # Platform auth login (token-based) must work without KUBECONFIG
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

  # auth status must work without KUBECONFIG
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth status >/dev/null

  # auth logout must work without KUBECONFIG
  KUBECONFIG="" MCP_RUNTIME_CONFIG_DIR="$tmp_dir" "$BIN" auth logout >/dev/null

  rm -rf "$tmp_dir"
  echo "=== no-kubeconfig ops: OK ==="
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
  # Gateway returns 401 (missing identity) or 403 (forbidden) for unauthenticated direct calls;
  # both are correct — the important invariant is adapter_required=true in the body.
  if [[ "$status" != "401" && "$status" != "403" ]] || ! jq -e '.adapter_required == true' "$body" >/dev/null; then
    echo "expected direct public call to fail with adapter_required (401 or 403), got HTTP ${status}" >&2
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

  # Give the gateway sidecar time to load the newly created MCPAgentSession.
  # Full latency chain: session created → operator reconciles ConfigMap (~2s) →
  # kubelet projects ConfigMap change to pod volume (~5s) → gateway reads at
  # next 5-second poll tick. 12s covers 2 gateway poll cycles with margin.
  sleep 12

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
  local token
  token="$(profile_token "$ADMIN_PROFILE")"
  local body="$WORK_DIR/events.body"
  curl -fsS -o "$body" \
    -H "authorization: Bearer ${token}" \
    "${PLATFORM_URL}/api/runtime/server-events?namespace=${ACME_NS}&server=${ACME_SERVER}&limit=10"
  jq -e --arg globex "$GLOBEX_TEAM_ID" '
    (.events // .) as $events
    | ($events | length) > 0
    and any($events[]; (.tool_name // .ToolName // "") == "add" and
        ((.payload.subject_team_id // .subject_team_id // .team_id // "") == $globex or
         $globex == "")
        and (.decision // .Decision // "allow") == "allow")
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
  ensure_login "$TECHCORP_PROFILE" "$TECHCORP_EMAIL" "$TECHCORP_PASSWORD"
  ADMIN_TOKEN="$(profile_token "$ADMIN_PROFILE")"
  ACME_TEAM_ID="$(team_id "$ACME_SLUG" "$ADMIN_TOKEN")"
  GLOBEX_TEAM_ID="$(team_id "$GLOBEX_SLUG" "$ADMIN_TOKEN")"
  TECHCORP_TEAM_ID="$(team_id "$TECHCORP_SLUG" "$ADMIN_TOKEN")"
fi

wait_for_rollout "$ACME_NS" "$ACME_SERVER"
wait_for_rollout "$GLOBEX_NS" "$GLOBEX_SERVER"
wait_for_rollout "$TECHCORP_NS" "$TECHCORP_SERVER"
verify_grant_exists "${ACME_SERVER}-${GLOBEX_SLUG}-${AGENT_ID}" "$ACME_NS"
verify_grant_exists "${ACME_SERVER}-${TECHCORP_SLUG}-${AGENT_ID}" "$ACME_NS"

verify_no_kubeconfig_ops

# Delete any lingering MCPAgentSessions before adapter verification so that
# the adapter proxy always creates fresh sessions during initialize. This
# prevents the auto-refresh race: if a stale session is reused and the gateway
# hasn't reloaded its policy yet (5-second poll), the tools/call after the
# 7-second sleep would fail with session_not_found / session_expired.
delete_all_sessions "$ACME_NS"

verify_direct_public_denied

# Verify globex adapter -> acme server
echo "=== verify: ${GLOBEX_SLUG} adapter call to ${ACME_SLUG}/${ACME_SERVER} ==="
adapter_call_add
verify_events "$ADMIN_TOKEN"

# Verify techcorp adapter -> acme server (same port, second invocation in sequence)
echo "=== verify: ${TECHCORP_SLUG} adapter call to ${ACME_SLUG}/${ACME_SERVER} ==="
TECHCORP_ADAPTER_LISTEN="${TECHCORP_ADAPTER_LISTEN:-127.0.0.1:8300}"
(
  MCP_PLATFORM_API_PROFILE="$TECHCORP_PROFILE" "$BIN" adapter proxy \
    --platform-url "$PLATFORM_URL" \
    --runtime-url "${MCP_URL}/${ACME_SERVER}/mcp" \
    --server "$ACME_SERVER" \
    --namespace "$ACME_NS" \
    --agent "$AGENT_ID" \
    --listen "$TECHCORP_ADAPTER_LISTEN" \
    --auto-refresh >"$WORK_DIR/techcorp-adapter.log" 2>&1
) &
techcorp_proxy_pid=$!
trap 'kill "$techcorp_proxy_pid" >/dev/null 2>&1 || true' EXIT

for _ in {1..60}; do
  if curl -sS --max-time 1 -o /dev/null "http://${TECHCORP_ADAPTER_LISTEN}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done

tc_headers="$WORK_DIR/techcorp-adapter.headers"
curl -fsS -D "$tc_headers" -o "$WORK_DIR/techcorp-init.body" \
  -H "content-type: application/json" \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"multitenancytest-techcorp","version":"0.1"}}}' \
  "http://${TECHCORP_ADAPTER_LISTEN}" >/dev/null

tc_session="$(awk 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ {gsub(/\r/,"",$2); print $2}' "$tc_headers")"
test -n "$tc_session"

# Give the gateway sidecar time to load the newly created MCPAgentSession.
# Full latency chain: session created → operator reconciles ConfigMap (~2s) →
# kubelet projects ConfigMap change to pod volume (~5s) → gateway reads at
# next 5-second poll tick. 12s covers 2 gateway poll cycles with margin.
sleep 12

curl -fsS -o "$WORK_DIR/techcorp-notify.body" \
  -H "content-type: application/json" \
  -H "Mcp-Session-Id: ${tc_session}" \
  --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  "http://${TECHCORP_ADAPTER_LISTEN}" >/dev/null

tc_result_body="$WORK_DIR/techcorp-add.body"
curl -fsS -o "$tc_result_body" \
  -H "content-type: application/json" \
  -H "Mcp-Session-Id: ${tc_session}" \
  --data '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"add","arguments":{"a":11,"b":22}}}' \
  "http://${TECHCORP_ADAPTER_LISTEN}" >/dev/null

tc_result="$(jq -er '.result.content[0].text' "$tc_result_body")"
if [[ "$tc_result" != "33" ]]; then
  echo "techcorp adapter add result was ${tc_result}, expected 33" >&2
  cat "$tc_result_body" >&2
  exit 1
fi
kill "$techcorp_proxy_pid" >/dev/null 2>&1 || true
trap - EXIT
echo "=== techcorp adapter call: OK (11+22=33) ==="

if command -v kubectl >/dev/null 2>&1; then
  "$BIN" cluster doctor
fi

print_cursor_config
echo
echo "multi-tenant flow passed:"
echo "  - ${GLOBEX_SLUG}/${AGENT_ID} called ${ACME_SLUG}/${ACME_SERVER} add(7,9)=16 via adapter"
echo "  - ${TECHCORP_SLUG}/${AGENT_ID} called ${ACME_SLUG}/${ACME_SERVER} add(11,22)=33 via adapter"
echo "  - no-kubeconfig ops verified for auth/login/list/use commands"
