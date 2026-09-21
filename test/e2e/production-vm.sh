#!/usr/bin/env bash
set -Eeuo pipefail

# Disposable production-mode E2E runner. Run this only on the dedicated E2E VM.
# The encrypted backup directory is intentionally outside every cleanup path.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${BIN:-${ROOT_DIR}/bin/mcp-runtime}"
BACKUP_DIR="${E2E_BACKUP_DIR:-/var/lib/mcp-runtime-e2e-backup}"
RUN_ID="${E2E_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
WORK_DIR="${E2E_WORK_DIR:-/var/tmp/mcp-runtime-e2e-${RUN_ID}}"
ARTIFACT_DIR="${BACKUP_DIR}/runs/${RUN_ID}"
KUBECONFIG_FILE="${E2E_KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
PLATFORM_URL="${E2E_PLATFORM_URL:-https://platform.e2e.mcpruntime.org}"
MCP_URL="${E2E_MCP_URL:-https://mcp.e2e.mcpruntime.org}"
REGISTRY_HOST="${E2E_REGISTRY_HOST:-registry.e2e.mcpruntime.org}"
AUTH_URL="${E2E_AUTH_URL:-https://auth.e2e.mcpruntime.org}"

mkdir -p "${ARTIFACT_DIR}" "${WORK_DIR}"
chmod 700 "${BACKUP_DIR}" "${ARTIFACT_DIR}" "${WORK_DIR}"

if [[ -f "${BACKUP_DIR}/e2e.env" ]]; then
  # shellcheck disable=SC1091
  set -a
  source "${BACKUP_DIR}/e2e.env"
  set +a
fi

export KUBECONFIG="${KUBECONFIG_FILE}"
export MCP_PLATFORM_API_URL="${PLATFORM_URL}"
export MCP_PLATFORM_INGRESS_HOST="platform.e2e.mcpruntime.org"
export MCP_REGISTRY_INGRESS_HOST="registry.e2e.mcpruntime.org"
export MCP_MCP_INGRESS_HOST="mcp.e2e.mcpruntime.org"
export MCP_AUTH_INGRESS_HOST="auth.e2e.mcpruntime.org"
export E2E_ARTIFACT_DIR="${ARTIFACT_DIR}"
export MCPRUNTIME_ORG_ROOT="${ROOT_DIR}"
export MCP_TLS_BACKUP_DIR="${BACKUP_DIR}/platform-runtime"

PLATFORM_BACKUP_HELPERS_LOADED=0

log() { printf '[prod-e2e] %s\n' "$*"; }
fail() { log "ERROR: $*" >&2; return 1; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

# k3s writes the kubeconfig before the apiserver serves traffic and before the
# kubelet registers its Node object. `kubectl wait --all` does not wait for a
# resource to appear: it exits non-zero with "no matching resources found" the
# moment the selector matches nothing. Poll for registration first.
wait_for_node_registration() {
  local timeout="${1:-180}"
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if kubectl get nodes -o name 2>/dev/null | grep -q .; then
      return 0
    fi
    sleep 3
  done
  fail "no Kubernetes node registered within ${timeout}s"
}

# k3s installs its bundled Traefik through helm-controller only after the node
# goes Ready, so a doctor run that starts the moment the node registers sees no
# IngressClass, no deployment, and no web entrypoint. Wait for the ingress to
# converge before any preflight that asserts on it.
traefik_namespace() {
  local candidate
  for candidate in kube-system traefik; do
    if kubectl -n "${candidate}" get deploy traefik >/dev/null 2>&1; then
      printf '%s' "${candidate}"
      return 0
    fi
  done
  return 1
}

# Mirrors the doctor "traefik service exposure" check, which accepts either a
# LoadBalancer address or a NodePort for the web entrypoint.
traefik_is_exposed() {
  local namespace="$1" address ports
  address="$(kubectl -n "${namespace}" get svc traefik \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)"
  [[ -n "${address}" ]] && return 0
  ports="$(kubectl -n "${namespace}" get svc traefik -o jsonpath='{.spec.ports[*].nodePort}' 2>/dev/null)"
  [[ -n "${ports// /}" ]]
}

wait_for_traefik() {
  local timeout="${1:-300}"
  local deadline=$((SECONDS + timeout))
  local namespace=""
  log "waiting for the bundled Traefik ingress to become ready"
  while ((SECONDS < deadline)); do
    if namespace="$(traefik_namespace)" &&
      kubectl -n "${namespace}" rollout status deploy/traefik --timeout=30s >/dev/null 2>&1; then
      break
    fi
    namespace=""
    sleep 5
  done
  if [[ -z "${namespace}" ]]; then
    fail "bundled Traefik did not become ready within ${timeout}s"
    return 1
  fi
  log "Traefik deployment is ready in namespace ${namespace}"

  # Exposure is what ACME HTTP-01 needs. Warn rather than abort: setup still
  # gets a chance to wire ingress, and post-setup diagnostics is the real gate.
  while ((SECONDS < deadline)); do
    if traefik_is_exposed "${namespace}"; then
      log "Traefik web entrypoint is externally exposed"
      return 0
    fi
    sleep 5
  done
  log "WARNING: Traefik has no LoadBalancer address or NodePort yet; continuing into setup"
}

capture_cluster_state() {
  [[ -f "${KUBECONFIG}" ]] || return 0
  kubectl get nodes -o wide >"${ARTIFACT_DIR}/nodes.txt" 2>&1 || true
  kubectl get events -A --sort-by=.lastTimestamp >"${ARTIFACT_DIR}/events.txt" 2>&1 || true
  kubectl get pods -A -o wide >"${ARTIFACT_DIR}/pods.txt" 2>&1 || true
  kubectl get deploy -A >"${ARTIFACT_DIR}/deployments.txt" 2>&1 || true
  kubectl get ingress -A >"${ARTIFACT_DIR}/ingresses.txt" 2>&1 || true
  kubectl get pvc -A >"${ARTIFACT_DIR}/pvcs.txt" 2>&1 || true
  kubectl get mcpservers -A -o yaml >"${ARTIFACT_DIR}/mcpservers.yaml" 2>&1 || true
  for namespace in mcp-sentinel mcp-servers registry mcp-auth; do
    kubectl -n "${namespace}" get pods -o wide >"${ARTIFACT_DIR}/${namespace}-pods.txt" 2>&1 || true
    kubectl -n "${namespace}" logs --all-containers --prefix --tail=300 -l app=mcp-runtime-api >"${ARTIFACT_DIR}/${namespace}-runtime-api.log" 2>&1 || true
  done
}

load_platform_backup_helpers() {
  if [[ "${PLATFORM_BACKUP_HELPERS_LOADED}" == "1" ]]; then
    return 0
  fi
  # Reuse the deployment backup implementation so the VM-side snapshot covers
  # TLS, platform credentials, auth-server secrets, and OIDC configuration.
  # Do not load the deployment dotenv here: the E2E runner owns its environment.
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/hack/deploy/mcpruntime-org/lib/backup.sh"
  PLATFORM_BACKUP_HELPERS_LOADED=1
}

backup_platform_runtime() {
  [[ -f "${KUBECONFIG}" ]] || return 0
  if ! kubectl get nodes >/dev/null 2>&1; then
    log "skipping platform backup because the Kubernetes API is unavailable"
    return 0
  fi
  load_platform_backup_helpers
  log "capturing platform runtime backup from the VM before cluster cleanup"
  if ! mcpruntime_org_backup_platform_runtime; then
    log "WARNING: platform runtime backup failed; preserving any previous snapshot"
  fi
}

install_dependencies() {
  local package_manager=""
  if command -v apt-get >/dev/null 2>&1; then
    package_manager=apt
  elif command -v dnf >/dev/null 2>&1; then
    package_manager=dnf
  elif command -v apk >/dev/null 2>&1; then
    package_manager=apk
  fi

  case "${package_manager}" in
    apt)
      apt-get update
      apt-get install -y ca-certificates curl git jq openssh-client
      ;;
    dnf)
      dnf install -y ca-certificates curl git jq openssh-clients
      ;;
    apk)
      apk add --no-cache ca-certificates curl git jq openssh-client
      ;;
    *)
      log "no supported package manager found; assuming curl, git, jq, and kubectl are preinstalled"
      ;;
  esac
}

restore_backup_state() {
  # The backup is intentionally outside WORK_DIR and every cleanup target.
  # Legacy hooks/manifests may provision prerequisites before setup.
  if [[ -x "${BACKUP_DIR}/restore.sh" ]]; then
    log "restoring E2E certificates and credentials through backup hook"
    E2E_BACKUP_DIR="${BACKUP_DIR}" KUBECONFIG="${KUBECONFIG}" \
      bash "${BACKUP_DIR}/restore.sh"
  elif [[ -d "${BACKUP_DIR}/manifests" ]]; then
    log "restoring E2E Kubernetes backup manifests"
    kubectl apply -R -f "${BACKUP_DIR}/manifests"
  else
    log "no Kubernetes backup restore hook/manifests found; setup will provision fresh TLS state"
  fi
}

# Production setup refuses to run without a platform admin identity, and the
# secret builder clears the admin pair unless BOTH the address and the password
# are present. Default the address to the ACME contact, which the workflow
# already supplies, and mint a password on first run so the credential never
# lives in the repo. It is persisted in the 0600 env file inside the preserved
# backup directory so repeat runs keep the same admin account.
ensure_platform_admin_config() {
  local env_file="${BACKUP_DIR}/e2e.env"
  local email="${E2E_PLATFORM_ADMIN_EMAIL:-${E2E_ACME_EMAIL}}"
  local password="${E2E_PLATFORM_ADMIN_PASSWORD:-}"

  if [[ -z "${password}" ]]; then
    # `tr </dev/urandom | head -c` dies of SIGPIPE, which pipefail turns into a
    # script abort, so bound the randomness upstream and slice it in bash.
    local raw
    raw="$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')"
    password="${raw:0:32}"
    [[ ${#password} -eq 32 ]] || fail "failed to generate a platform admin password"
    touch "${env_file}"
    chmod 600 "${env_file}"
    printf 'E2E_PLATFORM_ADMIN_PASSWORD=%s\n' "${password}" >>"${env_file}"
    log "generated a platform admin password and stored it in ${env_file}"
  fi

  export MCP_PLATFORM_ADMIN_EMAIL="${email}"
  export MCP_PLATFORM_ADMIN_PASSWORD="${password}"
  export E2E_PLATFORM_ADMIN_EMAIL="${email}"
  export E2E_PLATFORM_ADMIN_PASSWORD="${password}"
  log "platform admin configured for ${email}"
}

restore_platform_runtime_after_setup() {
  if [[ ! -L "${BACKUP_DIR}/platform-runtime/latest" || ! -d "${BACKUP_DIR}/platform-runtime/latest" ]]; then
    return 0
  fi
  load_platform_backup_helpers
  log "restoring platform runtime snapshot captured on the VM"
  if ! mcpruntime_org_restore_platform_runtime; then
    log "WARNING: platform runtime snapshot restore failed"
  fi
}

cleanup() {
  local status=$?
  if [[ ${status} -ne 0 ]]; then
    log "failure detected; collecting diagnostics under ${ARTIFACT_DIR}"
    capture_cluster_state
  fi
  if [[ "${E2E_CLEANUP:-1}" == "1" ]]; then
    log "cleaning disposable Kubernetes/VM state; preserving ${BACKUP_DIR}"
    backup_platform_runtime
    if [[ -x /usr/local/bin/k3s-uninstall.sh ]]; then
      /usr/local/bin/k3s-uninstall.sh >"${ARTIFACT_DIR}/k3s-uninstall.log" 2>&1 || true
    fi
    rm -rf /etc/rancher/k3s /var/lib/rancher/k3s "${WORK_DIR}"
    rm -f "${ROOT_DIR}/bin/mcp-runtime"
  fi
  log "E2E run ${RUN_ID} finished with status ${status}; backup preserved at ${BACKUP_DIR}"
  exit "${status}"
}
trap cleanup EXIT

: "${E2E_ACME_EMAIL:?set E2E_ACME_EMAIL in ${BACKUP_DIR}/e2e.env}"

install_dependencies
require_command curl
require_command git
require_command jq

for host in "platform.e2e.mcpruntime.org" "registry.e2e.mcpruntime.org" "mcp.e2e.mcpruntime.org" "auth.e2e.mcpruntime.org"; do
  getent hosts "${host}" >/dev/null || fail "DNS does not resolve ${host}"
done

if [[ ! -f "${KUBECONFIG}" ]]; then
  log "installing k3s on the disposable VM"
  curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644
  for _ in {1..60}; do
    [[ -f "${KUBECONFIG}" ]] && break
    sleep 2
  done
fi

[[ -f "${KUBECONFIG}" ]] || fail "k3s kubeconfig was not created"
require_command kubectl
wait_for_node_registration 180
kubectl wait --for=condition=Ready nodes --all --timeout=180s

restore_backup_state

if [[ ! -x "${BIN}" ]]; then
  require_command go
  go build -o "${BIN}" ./cmd/mcp-runtime
fi

wait_for_traefik 300

# Advisory only: this is a baseline snapshot of a cluster that has not been set
# up yet, so checks covering components setup installs are expected to be unmet.
# The post-setup `cluster diagnostics` run below is the gate.
log "running pre-setup cluster doctor"
if ! "${BIN}" cluster doctor 2>&1 | tee "${ARTIFACT_DIR}/doctor-before.log"; then
  log "pre-setup doctor reported unmet requirements; continuing because setup provisions them"
fi

SETUP_ARGS=(
  setup
  --strict-prod
  --platform-mode "${E2E_PLATFORM_MODE:-tenant}"
  --ingress traefik
  --with-tls
  --acme-email "${E2E_ACME_EMAIL}"
  --registry-mode "${E2E_REGISTRY_MODE:-bundled-https}"
  --kubeconfig "${KUBECONFIG}"
)
if [[ "${E2E_WITH_MCP_AUTH:-0}" == "1" ]]; then
  SETUP_ARGS+=(--with-mcp-auth-server)
  [[ -n "${E2E_MCP_AUTH_CONNECTORS_FILE:-}" ]] && SETUP_ARGS+=(--mcp-auth-connectors-file "${E2E_MCP_AUTH_CONNECTORS_FILE}")
  [[ -n "${E2E_MCP_AUTH_CONNECTOR:-}" ]] && SETUP_ARGS+=(--mcp-auth-connector "${E2E_MCP_AUTH_CONNECTOR}")
  [[ -n "${E2E_MCP_AUTH_ISSUER_URL:-}" ]] && SETUP_ARGS+=(--mcp-auth-issuer-url "${E2E_MCP_AUTH_ISSUER_URL}")
  [[ -n "${E2E_MCP_AUTH_TLS_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-tls-secret "${E2E_MCP_AUTH_TLS_SECRET}")
  [[ -n "${E2E_MCP_AUTH_SIGNING_KEY_SECRET:-}" ]] && SETUP_ARGS+=(--mcp-auth-signing-key-secret "${E2E_MCP_AUTH_SIGNING_KEY_SECRET}")
fi

ensure_platform_admin_config

log "running production-style setup"
"${BIN}" "${SETUP_ARGS[@]}" 2>&1 | tee "${ARTIFACT_DIR}/setup.log"

restore_platform_runtime_after_setup

log "running post-setup diagnostics"
"${BIN}" cluster diagnostics | tee "${ARTIFACT_DIR}/diagnostics-after.log"

resolve_platform_token() {
  if [[ -n "${E2E_PLATFORM_API_TOKEN:-}" ]] && curl --fail --silent --show-error \
    -H "x-api-key: ${E2E_PLATFORM_API_TOKEN}" \
    -H "authorization: Bearer ${E2E_PLATFORM_API_TOKEN}" \
    "${PLATFORM_URL}/api/v1/auth/me" >/dev/null 2>&1; then
    return 0
  fi

  local encoded generated
  # Tolerate a missing secret here so the explicit message below is what the
  # run reports, instead of set -e aborting on the assignment with no context.
  encoded="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
    -o jsonpath='{.data.ADMIN_API_KEYS}' 2>/dev/null || true)"
  generated="$(printf '%s' "${encoded}" | base64 --decode | cut -d',' -f1 | tr -d '\r\n')"
  [[ -n "${generated}" ]] || fail "E2E_PLATFORM_API_TOKEN was rejected and setup did not produce an ADMIN_API_KEYS value"
  export E2E_PLATFORM_API_TOKEN="${generated}"
  log "using the first generated admin API key from mcp-sentinel-secrets for this run"
}

log "checking CLI command surfaces"
for command in auth bootstrap cluster catalog registry server access adapter admin setup status sentinel team; do
  "${BIN}" "${command}" --help >"${ARTIFACT_DIR}/help-${command}.txt"
done
"${BIN}" cluster doctor --help >"${ARTIFACT_DIR}/help-cluster-doctor.txt"
"${BIN}" cluster diagnostics --help >"${ARTIFACT_DIR}/help-cluster-diagnostics.txt"
"${BIN}" server push --help >"${ARTIFACT_DIR}/help-server-push.txt"

resolve_platform_token
printf '%s' "${E2E_PLATFORM_API_TOKEN}" | "${BIN}" auth login --api-url "${PLATFORM_URL}" --profile e2e --token-stdin
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
  curl --fail --silent --show-error "${AUTH_URL}/.well-known/openid-configuration" >"${ARTIFACT_DIR}/auth-oidc-discovery.json"
  curl --fail --silent --show-error "${AUTH_URL}/.well-known/oauth-authorization-server" >"${ARTIFACT_DIR}/auth-server-metadata.json"
fi

if [[ "${E2E_RUN_MULTITENANCY:-1}" == "1" ]]; then
  PLATFORM_URL="${PLATFORM_URL}" MCP_URL="${MCP_URL}" REGISTRY_HOST="${REGISTRY_HOST}" \
    ADMIN_TOKEN_INPUT="${E2E_PLATFORM_API_TOKEN}" \
    BIN="${BIN}" WORK_DIR="${WORK_DIR}/multitenancy" \
    bash "${ROOT_DIR}/hack/deploy/mcpruntime-org/multitenancy-test.sh" | tee "${ARTIFACT_DIR}/multitenancy.log"
fi

log "production-mode CLI/API E2E passed"
