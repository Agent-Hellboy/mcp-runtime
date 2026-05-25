#!/usr/bin/env bash
# Safe application-namespace wipe for the mcpruntime.org k3s cluster.
#
# Backs up platform-runtime material only (TLS, cert-manager ownership, platform
# ConfigMap/Secret bootstrap keys), then deletes MCP Runtime app namespaces.
# Intentionally does NOT preserve tenant/user data: platform Postgres identity
# store, MCPServer/grant/session CRs, team namespaces, registry images, or
# analytics history. Use this for a fresh platform reinstall / integration test.
#
# Preserved cluster infrastructure: kube-system (k3s Traefik), cert-manager.
#
# Typical fresh reinstall:
#   hack/clean-k3s-mcpruntime-org.sh --yes
#   hack/setup-k3s-mcpruntime-org.sh          # auto-restores platform backup when present
#
# Code-only iteration (no wipe): hack/rollout-k3s-mcpruntime-org.sh
#
# Set GOOGLE_CLIENT_ID (and OIDC_*) in config/deployments/mcpruntime-org.env before
# setup when browser sign-in must match the previous deployment.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ENV_FILE="${MCP_DEPLOY_ENV:-config/deployments/mcpruntime-org.env}"

source_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "error: $ENV_FILE is required for production cluster operations" >&2
    echo "       copy config/deployments/mcpruntime-org.env.example and customize" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  set -a && source "$ENV_FILE" && set +a
  : "${KUBECONFIG:?set KUBECONFIG in $ENV_FILE}"
}

usage() {
  cat <<'EOF'
Usage: hack/clean-k3s-mcpruntime-org.sh --yes [--restore-platform] [--no-backup] [--dry-run] [--wait]

  --yes               Required for destructive clean (namespace wipe).
  --restore-platform  Re-apply platform-runtime backups after setup (TLS, certs, config).
  --restore-tls       Alias for --restore-platform.
  --no-backup         Skip backup (not recommended on production TLS clusters).
  --dry-run           Print actions without deleting or writing backups.
  --wait              Wait for namespace deletion to finish (default: fire-and-forget).

Platform backup scope (platform functioning only):
  - TLS Secrets + cert-manager Certificate CRs + letsencrypt-prod ClusterIssuer
  - mcp-sentinel-config (OIDC, Traefik namespace, registry host, platform flags)
  - mcp-sentinel-secrets bootstrap keys (API/UI/ingest/Grafana/DB passwords)

NOT backed up (intentional reset on clean):
  - Tenant teams, platform users, issued API keys in Postgres
  - MCPServer / MCPAccessGrant / MCPAgentSession CRs
  - Registry image blobs, ClickHouse/Tempo/Prometheus history

Environment (via config/deployments/mcpruntime-org.env):
  KUBECONFIG            cluster kubeconfig path (required)
  MCP_TLS_BACKUP_DIR    backup root (default: ~/.mcpruntime/backups/mcpruntime-org)
EOF
}

BACKUP_ROOT="${MCP_TLS_BACKUP_DIR:-$HOME/.mcpruntime/backups/mcpruntime-org}"
SNAPSHOT_DIR=""
DO_BACKUP=1
DRY_RUN=0
RESTORE_PLATFORM=0
CONFIRMED=0
WAIT_NS=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes)
      CONFIRMED=1
      shift
      ;;
    --restore-platform|--restore-tls)
      RESTORE_PLATFORM=1
      shift
      ;;
    --no-backup)
      DO_BACKUP=0
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --wait)
      WAIT_NS=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

source_env

kubectl_cmd() {
  kubectl --kubeconfig "$KUBECONFIG" "$@"
}

is_preserved_namespace() {
  case "$1" in
    kube-system|kube-public|kube-node-lease|default|cert-manager)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

ensure_backup_root() {
  if [[ "$DRY_RUN" == "1" ]]; then
    SNAPSHOT_DIR="$BACKUP_ROOT/dry-run"
    return 0
  fi
  umask 077
  mkdir -p "$BACKUP_ROOT"
  chmod 0700 "$BACKUP_ROOT"
}

init_backup_snapshot() {
  ensure_backup_root
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] snapshot directory: $SNAPSHOT_DIR"
    return 0
  fi
  local stamp
  stamp="$(date -u +%Y-%m-%dT%H%M%SZ)"
  SNAPSHOT_DIR="$BACKUP_ROOT/$stamp"
  mkdir -p "$SNAPSHOT_DIR"
  chmod 0700 "$SNAPSHOT_DIR"
  ln -sfn "$stamp" "$BACKUP_ROOT/latest"
  echo "platform backup snapshot: $SNAPSHOT_DIR"
}

resolve_restore_dir() {
  if [[ -L "$BACKUP_ROOT/latest" && -d "$BACKUP_ROOT/latest" ]]; then
    SNAPSHOT_DIR="$(cd "$BACKUP_ROOT/latest" && pwd)"
    return 0
  fi
  if [[ -f "$BACKUP_ROOT/registry-tls.yaml" ]]; then
    SNAPSHOT_DIR="$BACKUP_ROOT"
    return 0
  fi
  return 1
}

is_kubectl_not_found() {
  local msg="$1"
  [[ "$msg" == *"(NotFound)"* || "$msg" == *"not found"* ]]
}

validate_backup_yaml() {
  local file="$1"
  [[ -s "$file" ]] || return 1
  grep -q '^kind:' "$file" && grep -q '^metadata:' "$file"
}

backup_resource() {
  local file="$1"
  shift
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would backup: $* -> $file"
    return 0
  fi
  local tmp="${file}.tmp"
  local err
  if err="$(kubectl_cmd "$@" -o yaml 2>&1 >"$tmp")"; then
    if validate_backup_yaml "$tmp"; then
      mv "$tmp" "$file"
      chmod 0600 "$file"
      echo "backed up $(basename "$file")"
      return 0
    fi
    rm -f "$tmp"
    echo "skip backup $(basename "$file") (empty or invalid YAML)" >&2
    return 0
  fi
  rm -f "$tmp"
  if is_kubectl_not_found "$err"; then
    echo "skip backup $(basename "$file") (resource missing)"
    return 0
  fi
  echo "backup failed for $(basename "$file"): $err" >&2
  return 1
}

backup_platform_auth_env() {
  local auth_file="$SNAPSHOT_DIR/platform-auth.env"
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would export OIDC keys from mcp-sentinel-config -> platform-auth.env"
    return 0
  fi
  if ! kubectl_cmd get configmap mcp-sentinel-config -n mcp-sentinel >/dev/null 2>&1; then
    echo "skip platform-auth.env (mcp-sentinel-config missing)"
    return 0
  fi
  {
    echo "# Platform OIDC exports captured before clean ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo "# Merge into config/deployments/mcpruntime-org.env when rerunning setup."
    for key in GOOGLE_CLIENT_ID MCP_GOOGLE_CLIENT_ID OIDC_ISSUER OIDC_AUDIENCE OIDC_JWKS_URL; do
      val="$(kubectl_cmd get configmap mcp-sentinel-config -n mcp-sentinel -o "jsonpath={.data.${key}}" 2>/dev/null || true)"
      if [[ -n "$val" ]]; then
        printf 'export %s=%q\n' "$key" "$val"
      fi
    done
  } >"${auth_file}.tmp"
  mv "${auth_file}.tmp" "$auth_file"
  chmod 0600 "$auth_file"
  echo "backed up platform-auth.env"
}

backup_platform_runtime() {
  init_backup_snapshot
  echo "Platform-runtime backup root: $BACKUP_ROOT"
  backup_resource "$SNAPSHOT_DIR/registry-tls.yaml" get secret registry-tls -n registry
  backup_resource "$SNAPSHOT_DIR/mcp-sentinel-platform-tls.yaml" get secret mcp-sentinel-platform-tls -n mcp-sentinel
  backup_resource "$SNAPSHOT_DIR/registry-cert.yaml" get certificate registry-cert -n registry
  backup_resource "$SNAPSHOT_DIR/mcp-sentinel-platform-cert.yaml" get certificate mcp-sentinel-platform-tls -n mcp-sentinel
  backup_resource "$SNAPSHOT_DIR/letsencrypt-prod-clusterissuer.yaml" get clusterissuer letsencrypt-prod
  backup_resource "$SNAPSHOT_DIR/mcp-sentinel-config.yaml" get configmap mcp-sentinel-config -n mcp-sentinel
  backup_resource "$SNAPSHOT_DIR/mcp-sentinel-secrets.yaml" get secret mcp-sentinel-secrets -n mcp-sentinel
  backup_platform_auth_env
}

strip_and_apply() {
  local file="$1"
  local label="$2"
  if [[ ! -f "$file" ]]; then
    echo "skip restore $label (backup missing)"
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would apply $file"
    return 0
  fi
  kubectl_cmd create --dry-run=client -f "$file" -o yaml | kubectl_cmd apply -f -
  echo "restored $label"
}

restore_platform_runtime() {
  if ! resolve_restore_dir; then
    echo "error: no platform backup found under $BACKUP_ROOT" >&2
    echo "       expected $BACKUP_ROOT/latest or flat registry-tls.yaml" >&2
    exit 1
  fi
  echo "Restoring platform-runtime backups from $SNAPSHOT_DIR"

  local restored=0
  strip_and_apply "$SNAPSHOT_DIR/letsencrypt-prod-clusterissuer.yaml" "letsencrypt-prod ClusterIssuer"
  strip_and_apply "$SNAPSHOT_DIR/registry-tls.yaml" "registry TLS secret"
  strip_and_apply "$SNAPSHOT_DIR/mcp-sentinel-platform-tls.yaml" "platform UI TLS secret"
  strip_and_apply "$SNAPSHOT_DIR/registry-cert.yaml" "registry Certificate"
  strip_and_apply "$SNAPSHOT_DIR/mcp-sentinel-platform-cert.yaml" "platform UI Certificate"
  strip_and_apply "$SNAPSHOT_DIR/mcp-sentinel-config.yaml" "mcp-sentinel-config"
  strip_and_apply "$SNAPSHOT_DIR/mcp-sentinel-secrets.yaml" "mcp-sentinel-secrets"

  for f in registry-tls.yaml mcp-sentinel-platform-tls.yaml; do
    if [[ -f "$SNAPSHOT_DIR/$f" ]]; then
      restored=1
    fi
  done

  if [[ "$restored" == "0" && "$DRY_RUN" != "1" ]]; then
    echo "warning: no TLS backups found; setup may have requested new Let's Encrypt certificates" >&2
    echo "         LE rate limit is 5 duplicate certs / 7 days per domain set" >&2
  fi

  if [[ -f "$SNAPSHOT_DIR/platform-auth.env" ]]; then
    echo ""
    echo "OIDC exports: $SNAPSHOT_DIR/platform-auth.env"
    echo "Ensure config/deployments/mcpruntime-org.env includes GOOGLE_CLIENT_ID / OIDC_* when needed."
  fi

  if [[ "$DRY_RUN" != "1" ]]; then
    echo ""
    echo "Restart Sentinel API/UI so restored platform secrets and config take effect:"
    echo "  kubectl rollout restart deployment/mcp-sentinel-api deployment/mcp-sentinel-ui -n mcp-sentinel"
  fi
}

clean_cluster_scoped_mcp() {
  echo "Deleting cluster-scoped MCP access objects (tenant/user workload metadata)..."
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would delete mcpserver,mcpaccessgrant,mcpagentsession --all -A"
    echo "[dry-run] would delete clusterrole,clusterrolebinding -l app.kubernetes.io/managed-by=mcp-runtime"
    return 0
  fi
  kubectl_cmd delete mcpserver,mcpaccessgrant,mcpagentsession --all -A --ignore-not-found --wait=false 2>/dev/null || true
  kubectl_cmd delete clusterrole,clusterrolebinding \
    -l app.kubernetes.io/managed-by=mcp-runtime --ignore-not-found 2>/dev/null || true
}

clean_app_namespaces() {
  local to_delete=()
  while IFS= read -r ns; do
    [[ -z "$ns" ]] && continue
    if is_preserved_namespace "$ns"; then
      continue
    fi
    to_delete+=("$ns")
  done < <(kubectl_cmd get ns --no-headers 2>/dev/null | awk '{print $1}')
  if [[ ${#to_delete[@]} -eq 0 ]]; then
    echo "no application namespaces to delete"
    return 0
  fi
  echo "Deleting application namespaces (tenant/user data in these namespaces will be lost):"
  printf '  %s\n' "${to_delete[@]}"
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would delete namespaces above"
    return 0
  fi
  local wait_flag=(--wait=false)
  if [[ "$WAIT_NS" == "1" ]]; then
    wait_flag=(--wait=true --timeout=15m)
  fi
  kubectl_cmd delete ns "${to_delete[@]}" --ignore-not-found "${wait_flag[@]}"
  if [[ "$WAIT_NS" == "1" ]]; then
    echo "namespace deletion finished"
  else
    echo "namespace deletion started (may still be Terminating; use --wait or poll before setup)"
  fi
}

if [[ "$RESTORE_PLATFORM" == "1" ]]; then
  restore_platform_runtime
  exit 0
fi

if [[ "$CONFIRMED" != "1" && "$DRY_RUN" != "1" ]]; then
  echo "error: destructive clean requires --yes (or use --dry-run to preview)" >&2
  usage >&2
  exit 1
fi

echo "kubeconfig: $KUBECONFIG"
if ! kubectl_cmd get nodes >/dev/null 2>&1; then
  echo "error: cannot reach cluster" >&2
  exit 1
fi

cat <<'WARN'

WARNING: This removes all MCP Runtime app namespaces and tenant/user workload data.
Platform-runtime backups (TLS, OIDC, bootstrap secrets) are saved when backup is enabled.
Registry images, Postgres platform users/teams, and MCP CRs are NOT preserved.

WARN

if [[ "$DO_BACKUP" == "1" ]]; then
  backup_platform_runtime
else
  echo "skipping backup (--no-backup)"
fi

clean_cluster_scoped_mcp
clean_app_namespaces

cat <<EOF

Clean complete.

Next steps:
  1. Wait until no app namespaces are Terminating (or rerun clean with --wait).
  2. Ensure config/deployments/mcpruntime-org.env exports GOOGLE_CLIENT_ID (and OIDC_* if used).
  3. hack/setup-k3s-mcpruntime-org.sh
     (restores platform-runtime backup automatically when $BACKUP_ROOT/latest exists)
  4. kubectl rollout restart deployment/mcp-sentinel-api deployment/mcp-sentinel-ui -n mcp-sentinel
  5. hack/multitenancytest.sh   # PLATFORM_URL / MCP_URL / REGISTRY_HOST — fresh tenant data

For code-only changes without a wipe: hack/rollout-k3s-mcpruntime-org.sh

EOF
