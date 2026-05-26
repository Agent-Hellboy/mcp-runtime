#!/usr/bin/env bash
# Shared env and kubectl helpers for mcpruntime.org k3s hack scripts.

mcpruntime_org_repo_root() {
  if [[ -n "${MCPRUNTIME_ORG_ROOT:-}" ]]; then
    printf '%s' "$MCPRUNTIME_ORG_ROOT"
    return 0
  fi
  MCPRUNTIME_ORG_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
  printf '%s' "$MCPRUNTIME_ORG_ROOT"
}

mcpruntime_org_load_dotenv() {
  local root
  root="$(mcpruntime_org_repo_root)"
  if [[ -f "$root/.env" ]]; then
    # shellcheck disable=SC1091
    set -a && source "$root/.env" && set +a
  fi
}

# Usage: mcpruntime_org_load_env [strict]
# strict=1 (default for clean): require config/deployments/mcpruntime-org.env
# strict=0 (setup/rollout): fall back to mcpruntime-org.env.example
mcpruntime_org_load_env() {
  local strict="${1:-0}"
  mcpruntime_org_load_dotenv

  MCP_DEPLOY_ENV_FILE="${MCP_DEPLOY_ENV:-config/deployments/mcpruntime-org.env}"
  MCP_DEPLOY_ENV_EXAMPLE="config/deployments/mcpruntime-org.env.example"
  local root
  root="$(mcpruntime_org_repo_root)"
  cd "$root"

  if [[ -f "$MCP_DEPLOY_ENV_FILE" ]]; then
    # shellcheck disable=SC1090
    set -a && source "$MCP_DEPLOY_ENV_FILE" && set +a
  elif [[ "$strict" == "0" && -f "$MCP_DEPLOY_ENV_EXAMPLE" ]]; then
    echo "warning: $MCP_DEPLOY_ENV_FILE not found; using $MCP_DEPLOY_ENV_EXAMPLE"
    echo "         copy $MCP_DEPLOY_ENV_EXAMPLE to $MCP_DEPLOY_ENV_FILE to persist local overrides"
    # shellcheck disable=SC1090
    set -a && source "$MCP_DEPLOY_ENV_EXAMPLE" && set +a
  else
    echo "error: $MCP_DEPLOY_ENV_FILE is required for production cluster operations" >&2
    echo "       copy $MCP_DEPLOY_ENV_EXAMPLE and customize" >&2
    exit 1
  fi

  KUBECONFIG="${MCP_SETUP_KUBECONFIG:-${KUBECONFIG:-}}"
  : "${KUBECONFIG:?set KUBECONFIG or MCP_SETUP_KUBECONFIG in $MCP_DEPLOY_ENV_FILE}"
  export KUBECONFIG MCP_DEPLOY_ENV_FILE
}

mcpruntime_org_kubectl() {
  kubectl --kubeconfig "$KUBECONFIG" "$@"
}

mcpruntime_org_require_cluster() {
  if ! mcpruntime_org_kubectl get nodes >/dev/null 2>&1; then
    echo "error: cannot reach cluster with kubeconfig $KUBECONFIG" >&2
    exit 1
  fi
}

mcpruntime_org_registry_host() {
  if [[ -n "${MCP_REGISTRY_INGRESS_HOST:-}" ]]; then
    printf '%s' "$MCP_REGISTRY_INGRESS_HOST"
    return 0
  fi
  if [[ -n "${MCP_PLATFORM_DOMAIN:-}" ]]; then
    printf 'registry.%s' "$MCP_PLATFORM_DOMAIN"
    return 0
  fi
  printf '%s' "registry.mcpruntime.org"
}

mcpruntime_org_has_platform_backup() {
  local backup_root="${1:-${MCP_TLS_BACKUP_DIR:-$HOME/.mcpruntime/backups/mcpruntime-org}}"
  if [[ -L "$backup_root/latest" && -f "$backup_root/latest/registry-tls.yaml" ]]; then
    return 0
  fi
  [[ -f "$backup_root/registry-tls.yaml" ]]
}
