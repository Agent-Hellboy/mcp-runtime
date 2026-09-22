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

# The workflow passes a GitHub boolean input, which arrives as "true"/"false",
# while older callers pass 1/0. Accept both: testing only for "1" silently
# skipped the multi-team suite on every run.
e2e_flag_enabled() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1 | true | yes | on) return 0 ;;
    *) return 1 ;;
  esac
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

# Certificates are the only snapshot material the E2E needs: losing them means
# re-issuing from Let's Encrypt on every run, and its rate limits make repeat
# runs fail. Credentials are deliberately excluded. Setup generates fresh ones
# and runs syncPostgresPasswordClientGo against the new database, so re-applying
# an older mcp-sentinel-secrets afterwards would leave the API pods holding a
# password the database no longer accepts.
E2E_TLS_SNAPSHOT_FILES=(
  letsencrypt-prod-clusterissuer.yaml
  registry-tls.yaml
  registry-cert.yaml
  mcp-sentinel-platform-tls.yaml
  mcp-sentinel-platform-cert.yaml
)
LOCAL_SNAPSHOT_DIR="${WORK_DIR}/platform-runtime"
VM_SNAPSHOT_DIR="${VM_BACKUP_DIR}/platform-runtime/tls"

snapshot_tls_state() {
  kubectl get nodes >/dev/null 2>&1 || {
    log "skipping TLS snapshot because the Kubernetes API is unavailable"
    return 0
  }
  mkdir -p "${LOCAL_SNAPSHOT_DIR}"
  local captured=0
  _grab() {
    local file="$1"
    shift
    if kubectl "$@" -o yaml >"${LOCAL_SNAPSHOT_DIR}/${file}" 2>/dev/null &&
      [[ -s "${LOCAL_SNAPSHOT_DIR}/${file}" ]]; then
      captured=$((captured + 1))
      return 0
    fi
    rm -f "${LOCAL_SNAPSHOT_DIR}/${file}"
  }
  _grab letsencrypt-prod-clusterissuer.yaml get clusterissuer letsencrypt-prod
  _grab registry-tls.yaml get secret registry-tls -n registry
  _grab registry-cert.yaml get certificate registry-cert -n registry
  _grab mcp-sentinel-platform-tls.yaml get secret mcp-sentinel-platform-tls -n mcp-sentinel
  _grab mcp-sentinel-platform-cert.yaml get certificate mcp-sentinel-platform-tls -n mcp-sentinel

  # A failed run can reach this with nothing issued yet. Publishing that would
  # replace a usable snapshot with an empty one, so only ship a capture that
  # actually holds the public certificates.
  if [[ ! -s "${LOCAL_SNAPSHOT_DIR}/registry-tls.yaml" ||
    ! -s "${LOCAL_SNAPSHOT_DIR}/mcp-sentinel-platform-tls.yaml" ]]; then
    log "TLS snapshot incomplete (${captured} object(s)); keeping the previous snapshot"
    return 0
  fi
  vm_ssh "install -d -m 700 '${VM_SNAPSHOT_DIR}.new'" >/dev/null 2>&1 || return 0
  if tar -C "${LOCAL_SNAPSHOT_DIR}" -czf - . |
    vm_ssh "tar -C '${VM_SNAPSHOT_DIR}.new' -xzf - && rm -rf '${VM_SNAPSHOT_DIR}' && mv '${VM_SNAPSHOT_DIR}.new' '${VM_SNAPSHOT_DIR}'"; then
    log "stored TLS snapshot (${captured} objects) on ${VM_HOST}"
  else
    log "WARNING: could not store the TLS snapshot on ${VM_HOST}"
  fi
}

restore_tls_state() {
  mkdir -p "${LOCAL_SNAPSHOT_DIR}"
  if ! vm_ssh "test -d '${VM_SNAPSHOT_DIR}'" >/dev/null 2>&1; then
    log "no TLS snapshot on ${VM_HOST}; certificates will be issued fresh"
    return 0
  fi
  vm_ssh "tar -C '${VM_SNAPSHOT_DIR}' -czf - ." | tar -C "${LOCAL_SNAPSHOT_DIR}" -xzf - 2>/dev/null || {
    log "WARNING: could not fetch the TLS snapshot from ${VM_HOST}"
    return 0
  }
  # This runs before setup on purpose. cert-manager only skips issuance when a
  # valid secret is already present when it reconciles the Certificate, so
  # restoring afterwards would overwrite a cert ACME had just issued rather than
  # avoiding the request. The secrets need namespaces, which setup has not
  # created yet.
  local namespace
  for namespace in registry mcp-sentinel; do
    kubectl create namespace "${namespace}" --dry-run=client -o yaml 2>/dev/null | kubectl apply -f - >/dev/null 2>&1 || true
  done

  load_platform_backup_helpers
  local file
  for file in "${E2E_TLS_SNAPSHOT_FILES[@]}"; do
    [[ -s "${LOCAL_SNAPSHOT_DIR}/${file}" ]] || continue
    mcpruntime_org_backup_strip_and_apply "${LOCAL_SNAPSHOT_DIR}/${file}" "${file%.yaml}" || true
  done
  log "restored issued certificates; cert-manager will reuse them instead of asking ACME"
}

PLATFORM_BACKUP_HELPERS_LOADED=0
load_platform_backup_helpers() {
  [[ "${PLATFORM_BACKUP_HELPERS_LOADED}" == "1" ]] && return 0
  # backup.sh only defines functions; it never loads the deployment dotenv.
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/hack/deploy/mcpruntime-org/lib/backup.sh"
  PLATFORM_BACKUP_HELPERS_LOADED=1
}

# Let's Encrypt allows five certificates per exact set of identifiers per week,
# so a production-CA run can only succeed five times before every further run
# dies at Step 3 with a 429 and a retry-after roughly a day out. Staging has far
# higher limits and exercises the identical ACME order, HTTP-01 challenge and
# cert-manager path; only the signing CA differs. Its roots are not publicly
# trusted, so teach this process to trust them before anything calls the hosts,
# rather than weakening the checks with curl -k.
trust_acme_staging_roots() {
  local bundle="${WORK_DIR}/acme-staging-ca.pem" url system
  : >"${bundle}"
  for url in https://letsencrypt.org/certs/staging/letsencrypt-stg-root-x1.pem \
    https://letsencrypt.org/certs/staging/letsencrypt-stg-root-x2.pem; do
    if ! curl -fsSL "${url}" >>"${bundle}"; then
      log "WARNING: could not fetch ${url}; staging certificates will not verify"
      return 0
    fi
  done
  # The bundle replaces the default trust store rather than adding to it, so it
  # must carry the platform roots as well or every other HTTPS call this run
  # makes would stop verifying. If no system bundle is found, leave the default
  # trust alone and say so instead of shipping a staging-only bundle.
  local found=""
  for system in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
    if [[ -r "${system}" ]]; then
      cat "${system}" >>"${bundle}"
      found="${system}"
      break
    fi
  done
  if [[ -z "${found}" ]]; then
    log "WARNING: no system CA bundle found; leaving default trust in place, so staging certificates will not verify"
    return 0
  fi
  export CURL_CA_BUNDLE="${bundle}"
  export SSL_CERT_FILE="${bundle}"
  log "trusting Let's Encrypt staging roots for this run"
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
    snapshot_tls_state
    log "wiping disposable state on ${VM_HOST}; preserving only ${VM_BACKUP_DIR}"
    # Reclaim everything that does not require tearing down the network first,
    # because uninstalling k3s drops CNI and resets this very SSH session --
    # anything sequenced after it is simply lost.
    vm_ssh "set -u
      rm -rf /opt/mcp-runtime-e2e /var/tmp/mcp-runtime-e2e-* /tmp/mcp-runtime-e2e.tgz /tmp/mcp-img-*.tar
      if command -v docker >/dev/null 2>&1; then docker system prune -af --volumes || true; fi" \
      >"${ARTIFACT_DIR}/vm-cleanup.log" 2>&1 || true

    # Detach the k3s teardown so it survives the connection it kills, then
    # reconnect to confirm rather than trusting a command whose output cannot
    # come back.
    vm_ssh "setsid nohup sh -c '
      if [ -x /usr/local/bin/k3s-uninstall.sh ]; then /usr/local/bin/k3s-uninstall.sh; fi
      rm -rf /etc/rancher /var/lib/rancher /var/lib/kubelet /var/lib/cni /etc/cni /run/k3s /run/flannel
    ' >/tmp/k3s-uninstall.log 2>&1 </dev/null &" >>"${ARTIFACT_DIR}/vm-cleanup.log" 2>&1 || true

    local waited=0
    while ((waited < 180)); do
      if vm_ssh "test ! -d /etc/rancher && test ! -d /var/lib/rancher" >/dev/null 2>&1; then
        log "VM teardown confirmed"
        break
      fi
      sleep 10
      waited=$((waited + 10))
    done
    if ((waited >= 180)); then
      log "WARNING: VM teardown not confirmed within ${waited}s; check ${VM_BACKUP_DIR} host state"
    fi
    vm_ssh "df -h / | awk 'NR==2 {print \"free after cleanup: \" \$4 \" (\" \$5 \" used)\"}'" 2>/dev/null \
      | sed 's/^/[remote-e2e] /' || true
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

# kubelet verifies the registry certificate against the node's own trust store,
# not the runner's. With the staging CA the registry certificate is signed by a
# root no node trusts, so every image pull fails with "x509: certificate signed
# by unknown authority" and the operator never becomes ready. Install the roots
# before k3s exists, so containerd has them from its first start -- Go caches the
# system pool per process, so adding them afterwards needs a restart.
install_staging_roots_on_vm() {
  log "installing Let's Encrypt staging roots on ${VM_HOST}"
  if ! vm_ssh "set -eu
    install -d -m 755 /usr/local/share/ca-certificates
    curl -fsSL https://letsencrypt.org/certs/staging/letsencrypt-stg-root-x1.pem \
      -o /usr/local/share/ca-certificates/le-staging-x1.crt
    curl -fsSL https://letsencrypt.org/certs/staging/letsencrypt-stg-root-x2.pem \
      -o /usr/local/share/ca-certificates/le-staging-x2.crt
    update-ca-certificates >/dev/null 2>&1
    if systemctl is-active --quiet k3s; then systemctl restart k3s; fi"; then
    log "WARNING: could not install staging roots on ${VM_HOST}; image pulls will fail certificate verification"
  fi
}

if e2e_flag_enabled "${E2E_ACME_STAGING:-1}"; then
  install_staging_roots_on_vm
fi

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

# kubelet resolves names through the node's resolver, not CoreDNS, so it cannot
# reach the default in-cluster pull host registry.registry.svc.cluster.local and
# every platform pod lands in ImagePullBackOff with "lookup ...: Try again".
# For a bundled-HTTPS public install the supported endpoint is the public
# registry hostname: the node resolves it through public DNS, its Let's Encrypt
# certificate is already trusted, and setup provisions the matching pull secret
# and attaches it to the operator. The in-cluster skopeo helper rewrites this
# back to Service DNS when pushing, because the registry stores images by
# repository path and is reachable under either name.
export MCP_REGISTRY_ENDPOINT="${E2E_REGISTRY_ENDPOINT:-${REGISTRY_HOST}}"

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
# Default to the staging CA so repeat runs are not capped at five a week. Set
# E2E_ACME_STAGING=0 for an occasional run against the production CA.
if e2e_flag_enabled "${E2E_ACME_STAGING:-1}"; then
  SETUP_ARGS+=(--acme-staging)
  trust_acme_staging_roots
fi
if [[ "${E2E_WITH_MCP_AUTH:-0}" == "1" ]]; then
  SETUP_ARGS+=(--with-mcp-auth-server)
  [[ -n "${E2E_MCP_AUTH_CONNECTORS_FILE:-}" ]] && SETUP_ARGS+=(--mcp-auth-connectors-file "${E2E_MCP_AUTH_CONNECTORS_FILE}")
  [[ -n "${E2E_MCP_AUTH_CONNECTOR:-}" ]] && SETUP_ARGS+=(--mcp-auth-connector "${E2E_MCP_AUTH_CONNECTOR}")
  [[ -n "${E2E_MCP_AUTH_ISSUER_URL:-}" ]] && SETUP_ARGS+=(--mcp-auth-issuer-url "${E2E_MCP_AUTH_ISSUER_URL}")
  [[ -n "${E2E_MCP_AUTH_TLS_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-tls-secret "${E2E_MCP_AUTH_TLS_SECRET}")
  [[ -n "${E2E_MCP_AUTH_SIGNING_KEY_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-signing-key-secret "${E2E_MCP_AUTH_SIGNING_KEY_SECRET}")
fi

restore_tls_state

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

if e2e_flag_enabled "${E2E_RUN_MULTITENANCY:-1}"; then
  PLATFORM_URL="${PLATFORM_URL}" MCP_URL="${MCP_URL}" REGISTRY_HOST="${REGISTRY_HOST}" \
    ADMIN_TOKEN_INPUT="${E2E_PLATFORM_API_TOKEN}" \
    BIN="${BIN}" WORK_DIR="${WORK_DIR}/multitenancy" \
    bash "${ROOT_DIR}/hack/deploy/mcpruntime-org/multitenancy-test.sh" |
    tee "${ARTIFACT_DIR}/multitenancy.log"
fi

log "remote production-mode E2E passed"
