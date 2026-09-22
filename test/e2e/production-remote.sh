#!/usr/bin/env bash
set -Eeuo pipefail

# Production-mode E2E driven against a REMOTE disposable cluster.
#
# This models the way an operator actually installs MCP Runtime: the CLI runs on
# a workstation or CI runner and talks to the cluster through a kubeconfig. The
# VM is used only to host k3s; every CLI invocation, API call, and assertion
# happens locally.
#
# Contrast with production-vm.sh, which ships the repo to the VM and runs the
# CLI there. Running locally removes a whole class of environment problems: no
# repo tarball (so no missing .git), no login-shell working directory, and the
# runner's own Go toolchain and Docker instead of whatever the VM happens to
# have installed.
#
# Image pushes still work without any registry reachability from here: setup
# does `docker save` locally and runs a helper pod inside the cluster that
# pushes into the internal registry, so only the Kubernetes API must be
# reachable.
#
# Required:
#   E2E_VM_HOST        public address of the disposable VM
#   E2E_ACME_EMAIL     ACME contact for Let's Encrypt
# Auth (one of):
#   SSH agent/key for E2E_VM_USER (default root), or SSHPASS with sshpass.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${BIN:-${ROOT_DIR}/bin/mcp-runtime}"
RUN_ID="${E2E_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-${ROOT_DIR}/e2e-artifacts/${RUN_ID}}"
WORK_DIR="${E2E_WORK_DIR:-$(mktemp -d)}"

VM_HOST="${E2E_VM_HOST:?set E2E_VM_HOST to the disposable VM address}"
VM_USER="${E2E_VM_USER:-root}"
VM_BACKUP_DIR="${E2E_BACKUP_DIR:-/var/lib/mcp-runtime-e2e-backup}"
KUBECONFIG_FILE="${WORK_DIR}/kubeconfig"

PLATFORM_URL="${E2E_PLATFORM_URL:-https://platform.e2e.mcpruntime.org}"
MCP_URL="${E2E_MCP_URL:-https://mcp.e2e.mcpruntime.org}"
REGISTRY_HOST="${E2E_REGISTRY_HOST:-registry.e2e.mcpruntime.org}"
AUTH_URL="${E2E_AUTH_URL:-https://auth.e2e.mcpruntime.org}"

mkdir -p "${ARTIFACT_DIR}" "${WORK_DIR}"
cd "${ROOT_DIR}"

log() { printf '[remote-e2e] %s\n' "$*"; }
fail() {
  log "ERROR: $*" >&2
  return 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

# shellcheck source=test/e2e/lib/cluster-wait.sh
source "${ROOT_DIR}/test/e2e/lib/cluster-wait.sh"

# BatchMode is deliberately kept out of this array: the two auth paths need
# opposite values, and slicing it back out by index is easy to get wrong.
ssh_opts=(
  -o StrictHostKeyChecking="${E2E_SSH_STRICT_HOST_KEY:-yes}"
  -o ConnectTimeout=15
  -o ServerAliveInterval=30
  -o ServerAliveCountMax=20
)
if [[ -n "${E2E_VM_KNOWN_HOSTS:-}" ]]; then
  ssh_opts+=(-o UserKnownHostsFile="${E2E_VM_KNOWN_HOSTS}")
fi

# Password auth is only used when SSHPASS is exported, and sshpass needs
# BatchMode off to answer the prompt. Otherwise an agent or key file is
# expected, and BatchMode keeps a missing key from hanging on one.
vm_ssh() {
  # shellcheck disable=SC2029 # the remote command is composed locally on purpose
  if [[ -n "${SSHPASS:-}" ]]; then
    sshpass -e ssh -o BatchMode=no "${ssh_opts[@]}" "${VM_USER}@${VM_HOST}" "$@"
  else
    ssh -o BatchMode="${E2E_SSH_BATCH:-yes}" "${ssh_opts[@]}" "${VM_USER}@${VM_HOST}" "$@"
  fi
}

# production-vm.sh sources the VM's e2e.env directly because it runs there.
# Do the equivalent from here, so a local run needs no more configuration than
# an on-VM one. Values already in the environment win, which keeps CI secrets
# authoritative over whatever the VM happens to remember.
load_vm_env() {
  local line key
  while IFS= read -r line; do
    case "${line}" in
      E2E_*=*) ;;
      *) continue ;;
    esac
    key="${line%%=*}"
    if [[ -z "${!key:-}" ]]; then
      export "${key}=${line#*=}"
    fi
  done < <(vm_ssh "cat '${VM_BACKUP_DIR}/e2e.env' 2>/dev/null" 2>/dev/null || true)
}

persist_vm_env() {
  local key="$1" value="$2"
  vm_ssh "set -eu
    install -d -m 700 '${VM_BACKUP_DIR}'
    touch '${VM_BACKUP_DIR}/e2e.env'
    chmod 600 '${VM_BACKUP_DIR}/e2e.env'
    printf '%s=%s\n' '${key}' '${value}' >>'${VM_BACKUP_DIR}/e2e.env'"
}

capture_cluster_state() {
  [[ -f "${KUBECONFIG_FILE}" ]] || return 0
  kubectl get nodes -o wide >"${ARTIFACT_DIR}/nodes.txt" 2>&1 || true
  kubectl get events -A --sort-by=.lastTimestamp >"${ARTIFACT_DIR}/events.txt" 2>&1 || true
  kubectl get pods -A -o wide >"${ARTIFACT_DIR}/pods.txt" 2>&1 || true
  kubectl get deploy -A >"${ARTIFACT_DIR}/deployments.txt" 2>&1 || true
  kubectl get ingress -A >"${ARTIFACT_DIR}/ingresses.txt" 2>&1 || true
  kubectl get mcpservers -A -o yaml >"${ARTIFACT_DIR}/mcpservers.yaml" 2>&1 || true
  for namespace in mcp-sentinel mcp-servers registry mcp-auth; do
    kubectl -n "${namespace}" get pods -o wide >"${ARTIFACT_DIR}/${namespace}-pods.txt" 2>&1 || true
    kubectl -n "${namespace}" logs --all-containers --prefix --tail=300 \
      -l app=mcp-runtime-api >"${ARTIFACT_DIR}/${namespace}-runtime-api.log" 2>&1 || true
  done
}

cleanup() {
  local status=$?
  if [[ ${status} -ne 0 ]]; then
    log "failure detected; collecting diagnostics under ${ARTIFACT_DIR}"
    capture_cluster_state
  fi
  if [[ "${E2E_CLEANUP:-1}" == "1" ]]; then
    log "wiping disposable state on ${VM_HOST}; preserving only ${VM_BACKUP_DIR}"
    # Everything except the backup directory goes: k3s and its CNI/kubelet
    # state, every Docker image, container, volume and build cache, and any
    # repo or scratch directory an earlier on-VM run left behind. Leftovers are
    # what filled the disk and got pods evicted for ephemeral storage.
    vm_ssh "set -u
      if [ -x /usr/local/bin/k3s-uninstall.sh ]; then /usr/local/bin/k3s-uninstall.sh || true; fi
      rm -rf /etc/rancher /var/lib/rancher /var/lib/kubelet /var/lib/cni /etc/cni /run/k3s /run/flannel
      if command -v docker >/dev/null 2>&1; then docker system prune -af --volumes || true; fi
      rm -rf /opt/mcp-runtime-e2e /var/tmp/mcp-runtime-e2e-* /tmp/mcp-runtime-e2e.tgz /tmp/mcp-img-*.tar
      df -h / | awk 'NR==2 {print \"free after cleanup: \" \$4 \" (\" \$5 \" used)\"}'" \
      >"${ARTIFACT_DIR}/vm-cleanup.log" 2>&1 || true
    tail -1 "${ARTIFACT_DIR}/vm-cleanup.log" 2>/dev/null | sed 's/^/[remote-e2e] /' || true
  fi
  rm -rf "${WORK_DIR}"
  log "run ${RUN_ID} finished with status ${status}; artifacts in ${ARTIFACT_DIR}"
  exit "${status}"
}
trap cleanup EXIT

# Everything the CLI needs runs here, so check it before touching the VM.
require_command ssh
require_command kubectl
require_command docker
require_command curl
require_command jq
if [[ -n "${SSHPASS:-}" ]]; then
  require_command sshpass
fi

# Fill any gaps from the VM's preserved env file, then require what setup needs.
load_vm_env
: "${E2E_ACME_EMAIL:?set E2E_ACME_EMAIL, or record it in ${VM_BACKUP_DIR}/e2e.env on the VM}"

if [[ ! -x "${BIN}" ]]; then
  require_command go
  log "building the CLI"
  go build -o "${BIN}" ./cmd/mcp-runtime
fi

for host in "platform.e2e.mcpruntime.org" "registry.e2e.mcpruntime.org" \
  "mcp.e2e.mcpruntime.org" "auth.e2e.mcpruntime.org"; do
  getent hosts "${host}" >/dev/null 2>&1 ||
    host "${host}" >/dev/null 2>&1 ||
    fail "DNS does not resolve ${host}"
done

log "provisioning k3s on ${VM_HOST}"
# k3s already adds the node's public IP to the API server certificate SANs, so
# the kubeconfig only needs its loopback server URL rewritten to reach it.
vm_ssh "set -eu
  # Builds from earlier on-VM runs leave images behind, and the cluster's
  # ephemeral storage shares this disk. A full disk evicts pods and surfaces as
  # an unexplained deployment timeout. k3s uses containerd, so pruning Docker
  # never touches running workloads.
  command -v docker >/dev/null 2>&1 && docker system prune -af >/dev/null 2>&1 || true
  if [ ! -f /etc/rancher/k3s/k3s.yaml ]; then
    curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644 --tls-san '${VM_HOST}'
  fi
  for _ in \$(seq 1 60); do
    [ -f /etc/rancher/k3s/k3s.yaml ] && break
    sleep 2
  done
  install -d -m 700 '${VM_BACKUP_DIR}'" 2>&1 | tee "${ARTIFACT_DIR}/k3s-install.log"

log "fetching kubeconfig and pointing it at ${VM_HOST}"
vm_ssh 'cat /etc/rancher/k3s/k3s.yaml' >"${KUBECONFIG_FILE}" 2>/dev/null ||
  fail "could not read the k3s kubeconfig from ${VM_HOST}"
[[ -s "${KUBECONFIG_FILE}" ]] || fail "the kubeconfig fetched from ${VM_HOST} is empty"
sed -i.bak "s#server: https://127.0.0.1:6443#server: https://${VM_HOST}:6443#" "${KUBECONFIG_FILE}"
rm -f "${KUBECONFIG_FILE}.bak"
chmod 600 "${KUBECONFIG_FILE}"

export KUBECONFIG="${KUBECONFIG_FILE}"
export MCP_PLATFORM_API_URL="${PLATFORM_URL}"
export MCP_PLATFORM_INGRESS_HOST="platform.e2e.mcpruntime.org"
export MCP_REGISTRY_INGRESS_HOST="registry.e2e.mcpruntime.org"
export MCP_MCP_INGRESS_HOST="mcp.e2e.mcpruntime.org"
export MCP_AUTH_INGRESS_HOST="auth.e2e.mcpruntime.org"
export E2E_ARTIFACT_DIR="${ARTIFACT_DIR}"
export MCPRUNTIME_ORG_ROOT="${ROOT_DIR}"

# The kubelet evicts pods once ephemeral storage runs low, which setup reports
# only as "deployment timeout" ten minutes later. Say so up front instead.
require_node_disk() {
  local min_gib="${1:-6}" avail
  avail="$(vm_ssh "df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9'" 2>/dev/null || true)"
  if [[ -z "${avail}" ]]; then
    log "WARNING: could not determine free disk on ${VM_HOST}"
    return 0
  fi
  if ((avail < min_gib)); then
    fail "only ${avail}GiB free on ${VM_HOST}; setup needs about ${min_gib}GiB and the kubelet evicts pods under ephemeral-storage pressure"
    return 1
  fi
  log "${avail}GiB free on ${VM_HOST}"
}

require_node_disk "${E2E_MIN_DISK_GIB:-6}"

wait_for_node_registration 180
kubectl wait --for=condition=Ready nodes --all --timeout=180s
wait_for_traefik 300

# The admin credential lives on the VM in the preserved backup directory so
# repeat runs reuse the same account even though this runner is ephemeral.
ensure_platform_admin_config() {
  local email password
  email="${E2E_PLATFORM_ADMIN_EMAIL:-${E2E_ACME_EMAIL}}"
  password="${E2E_PLATFORM_ADMIN_PASSWORD:-}"

  if [[ -z "${password}" ]]; then
    local raw
    raw="$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')"
    password="${raw:0:32}"
    [[ ${#password} -eq 32 ]] || fail "failed to generate a platform admin password"
    persist_vm_env E2E_PLATFORM_ADMIN_PASSWORD "${password}"
    log "generated a platform admin password and stored it on the VM"
  fi

  export MCP_PLATFORM_ADMIN_EMAIL="${email}"
  export MCP_PLATFORM_ADMIN_PASSWORD="${password}"
  log "platform admin configured for ${email}"
}

# Advisory: a cluster that has not been set up yet cannot satisfy checks that
# cover components setup installs. Post-setup diagnostics is the gate.
log "running pre-setup cluster doctor"
if ! "${BIN}" cluster doctor 2>&1 | tee "${ARTIFACT_DIR}/doctor-before.log"; then
  log "pre-setup doctor reported unmet requirements; continuing because setup provisions them"
fi

ensure_platform_admin_config

SETUP_ARGS=(
  setup
  --strict-prod
  --platform-mode "${E2E_PLATFORM_MODE:-tenant}"
  --ingress traefik
  --with-tls
  --acme-email "${E2E_ACME_EMAIL}"
  --registry-mode "${E2E_REGISTRY_MODE:-bundled-https}"
  --kubeconfig "${KUBECONFIG_FILE}"
)
if [[ "${E2E_WITH_MCP_AUTH:-0}" == "1" ]]; then
  SETUP_ARGS+=(--with-mcp-auth-server)
  [[ -n "${E2E_MCP_AUTH_CONNECTORS_FILE:-}" ]] && SETUP_ARGS+=(--mcp-auth-connectors-file "${E2E_MCP_AUTH_CONNECTORS_FILE}")
  [[ -n "${E2E_MCP_AUTH_CONNECTOR:-}" ]] && SETUP_ARGS+=(--mcp-auth-connector "${E2E_MCP_AUTH_CONNECTOR}")
  [[ -n "${E2E_MCP_AUTH_ISSUER_URL:-}" ]] && SETUP_ARGS+=(--mcp-auth-issuer-url "${E2E_MCP_AUTH_ISSUER_URL}")
  [[ -n "${E2E_MCP_AUTH_TLS_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-tls-secret "${E2E_MCP_AUTH_TLS_SECRET}")
  [[ -n "${E2E_MCP_AUTH_SIGNING_KEY_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-signing-key-secret "${E2E_MCP_AUTH_SIGNING_KEY_SECRET}")
fi

log "running production-style setup against ${VM_HOST}"
"${BIN}" "${SETUP_ARGS[@]}" 2>&1 | tee "${ARTIFACT_DIR}/setup.log"

log "running post-setup diagnostics"
"${BIN}" cluster diagnostics | tee "${ARTIFACT_DIR}/diagnostics-after.log"

log "checking CLI command surfaces"
for command in auth bootstrap cluster catalog registry server access adapter admin setup status sentinel team; do
  "${BIN}" "${command}" --help >"${ARTIFACT_DIR}/help-${command}.txt"
done

resolve_platform_token() {
  if [[ -n "${E2E_PLATFORM_API_TOKEN:-}" ]] && curl --fail --silent --show-error \
    -H "x-api-key: ${E2E_PLATFORM_API_TOKEN}" \
    -H "authorization: Bearer ${E2E_PLATFORM_API_TOKEN}" \
    "${PLATFORM_URL}/api/v1/auth/me" >/dev/null 2>&1; then
    return 0
  fi

  local encoded generated
  encoded="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
    -o jsonpath='{.data.ADMIN_API_KEYS}' 2>/dev/null || true)"
  generated="$(printf '%s' "${encoded}" | base64 --decode 2>/dev/null | cut -d',' -f1 | tr -d '\r\n')"
  [[ -n "${generated}" ]] || fail "setup did not produce an ADMIN_API_KEYS value"
  export E2E_PLATFORM_API_TOKEN="${generated}"
  log "using the first generated admin API key for this run"
}

resolve_platform_token
printf '%s' "${E2E_PLATFORM_API_TOKEN}" |
  "${BIN}" auth login --api-url "${PLATFORM_URL}" --profile e2e --token-stdin
MCP_PLATFORM_API_PROFILE=e2e "${BIN}" status | tee "${ARTIFACT_DIR}/cli-status.txt"
MCP_PLATFORM_API_PROFILE=e2e "${BIN}" server list | tee "${ARTIFACT_DIR}/cli-server-list.txt"
MCP_PLATFORM_API_PROFILE=e2e "${BIN}" registry info | tee "${ARTIFACT_DIR}/cli-registry-info.txt"

api_check() {
  local name="$1" url="$2"
  curl --fail --silent --show-error \
    -H "x-api-key: ${E2E_PLATFORM_API_TOKEN}" \
    -H "authorization: Bearer ${E2E_PLATFORM_API_TOKEN}" \
    "${url}" >"${ARTIFACT_DIR}/api-${name}.json"
}

api_check auth-me "${PLATFORM_URL}/api/v1/auth/me"
api_check servers "${PLATFORM_URL}/api/v1/runtime/servers"
api_check teams "${PLATFORM_URL}/api/v1/runtime/teams"
api_check components "${PLATFORM_URL}/api/v1/runtime/components"
curl --fail --silent --show-error "${PLATFORM_URL}/" >"${ARTIFACT_DIR}/ui-index.html"

if [[ "${E2E_WITH_MCP_AUTH:-0}" == "1" ]]; then
  curl --fail --silent --show-error "${AUTH_URL}/.well-known/openid-configuration" \
    >"${ARTIFACT_DIR}/auth-oidc-discovery.json"
  curl --fail --silent --show-error "${AUTH_URL}/.well-known/oauth-authorization-server" \
    >"${ARTIFACT_DIR}/auth-server-metadata.json"
fi

# The workflow passes a GitHub boolean input, which arrives as "true"/"false",
# while older callers pass 1/0. Accept both: testing only for "1" silently
# skipped the multi-team suite on every run.
e2e_flag_enabled() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1 | true | yes | on) return 0 ;;
    *) return 1 ;;
  esac
}

if e2e_flag_enabled "${E2E_RUN_MULTITENANCY:-1}"; then
  PLATFORM_URL="${PLATFORM_URL}" MCP_URL="${MCP_URL}" REGISTRY_HOST="${REGISTRY_HOST}" \
    ADMIN_TOKEN_INPUT="${E2E_PLATFORM_API_TOKEN}" \
    BIN="${BIN}" WORK_DIR="${WORK_DIR}/multitenancy" \
    bash "${ROOT_DIR}/hack/deploy/mcpruntime-org/multitenancy-test.sh" |
    tee "${ARTIFACT_DIR}/multitenancy.log"
fi

log "remote production-mode E2E passed"
