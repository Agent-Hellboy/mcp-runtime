#!/usr/bin/env bash
# Platform-runtime backup and restore for mcpruntime.org k3s deployments.

# shellcheck source=env.sh
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

MCP_TLS_BACKUP_ROOT="${MCP_TLS_BACKUP_DIR:-$HOME/.mcpruntime/backups/mcpruntime-org}"
MCP_TLS_SNAPSHOT_DIR=""
MCP_TLS_DRY_RUN="${MCP_TLS_DRY_RUN:-0}"

mcpruntime_org_backup_ensure_root() {
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    MCP_TLS_SNAPSHOT_DIR="$MCP_TLS_BACKUP_ROOT/dry-run"
    return 0
  fi
  umask 077
  mkdir -p "$MCP_TLS_BACKUP_ROOT"
  chmod 0700 "$MCP_TLS_BACKUP_ROOT"
}

mcpruntime_org_backup_init_snapshot() {
  mcpruntime_org_backup_ensure_root
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    echo "[dry-run] snapshot directory: $MCP_TLS_SNAPSHOT_DIR"
    return 0
  fi
  local stamp
  stamp="$(date -u +%Y-%m-%dT%H%M%SZ)"
  MCP_TLS_SNAPSHOT_DIR="$MCP_TLS_BACKUP_ROOT/$stamp"
  mkdir -p "$MCP_TLS_SNAPSHOT_DIR"
  chmod 0700 "$MCP_TLS_SNAPSHOT_DIR"
  ln -sfn "$stamp" "$MCP_TLS_BACKUP_ROOT/latest"
  echo "platform backup snapshot: $MCP_TLS_SNAPSHOT_DIR"
}

mcpruntime_org_backup_resolve_dir() {
  if [[ -L "$MCP_TLS_BACKUP_ROOT/latest" && -d "$MCP_TLS_BACKUP_ROOT/latest" ]]; then
    MCP_TLS_SNAPSHOT_DIR="$(cd "$MCP_TLS_BACKUP_ROOT/latest" && pwd)"
    return 0
  fi
  if [[ -f "$MCP_TLS_BACKUP_ROOT/registry-tls.yaml" ]]; then
    MCP_TLS_SNAPSHOT_DIR="$MCP_TLS_BACKUP_ROOT"
    return 0
  fi
  return 1
}

mcpruntime_org_backup_is_not_found() {
  local msg="$1"
  [[ "$msg" == *"(NotFound)"* || "$msg" == *"not found"* ]]
}

mcpruntime_org_backup_validate_yaml() {
  local file="$1"
  [[ -s "$file" ]] || return 1
  grep -q '^kind:' "$file" && grep -q '^metadata:' "$file"
}

mcpruntime_org_backup_resource() {
  local file="$1"
  shift
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    echo "[dry-run] would backup: $* -> $file"
    return 0
  fi
  local tmp="${file}.tmp"
  local err
  if err="$(mcpruntime_org_kubectl "$@" -o yaml 2>&1 >"$tmp")"; then
    if mcpruntime_org_backup_validate_yaml "$tmp"; then
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
  if mcpruntime_org_backup_is_not_found "$err"; then
    echo "skip backup $(basename "$file") (resource missing)"
    return 0
  fi
  echo "backup failed for $(basename "$file"): $err" >&2
  return 1
}

mcpruntime_org_backup_platform_auth_env() {
  local auth_file="$MCP_TLS_SNAPSHOT_DIR/platform-auth.env"
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    echo "[dry-run] would export OIDC keys from mcp-sentinel-config -> platform-auth.env"
    return 0
  fi
  if ! mcpruntime_org_kubectl get configmap mcp-sentinel-config -n mcp-sentinel >/dev/null 2>&1; then
    echo "skip platform-auth.env (mcp-sentinel-config missing)"
    return 0
  fi
  {
    echo "# Platform OIDC exports captured before clean ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo "# Merge into config/deployments/mcpruntime-org.env when rerunning setup."
    for key in GOOGLE_CLIENT_ID MCP_GOOGLE_CLIENT_ID OIDC_ISSUER OIDC_AUDIENCE OIDC_JWKS_URL; do
      val="$(mcpruntime_org_kubectl get configmap mcp-sentinel-config -n mcp-sentinel -o "jsonpath={.data.${key}}" 2>/dev/null || true)"
      if [[ -n "$val" ]]; then
        printf 'export %s=%q\n' "$key" "$val"
      fi
    done
  } >"${auth_file}.tmp"
  mv "${auth_file}.tmp" "$auth_file"
  chmod 0600 "$auth_file"
  echo "backed up platform-auth.env"
}

mcpruntime_org_backup_platform_runtime() {
  mcpruntime_org_backup_init_snapshot
  echo "Platform-runtime backup root: $MCP_TLS_BACKUP_ROOT"
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/registry-tls.yaml" get secret registry-tls -n registry
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-platform-tls.yaml" get secret mcp-sentinel-platform-tls -n mcp-sentinel
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/registry-cert.yaml" get certificate registry-cert -n registry
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-platform-cert.yaml" get certificate mcp-sentinel-platform-tls -n mcp-sentinel
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/letsencrypt-prod-clusterissuer.yaml" get clusterissuer letsencrypt-prod
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-config.yaml" get configmap mcp-sentinel-config -n mcp-sentinel
  mcpruntime_org_backup_resource "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-secrets.yaml" get secret mcp-sentinel-secrets -n mcp-sentinel
  mcpruntime_org_backup_platform_auth_env
}

mcpruntime_org_backup_strip_and_apply() {
  local file="$1"
  local label="$2"
  if [[ ! -f "$file" ]]; then
    echo "skip restore $label (backup missing)"
    return 0
  fi
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    echo "[dry-run] would apply $file"
    return 0
  fi
  mcpruntime_org_kubectl create --dry-run=client -f "$file" -o json \
    | jq 'del(
        .metadata.uid,
        .metadata.resourceVersion,
        .metadata.creationTimestamp,
        .metadata.generation,
        .metadata.managedFields,
        .metadata.selfLink,
        .metadata.ownerReferences,
        .metadata.annotations["kubectl.kubernetes.io/last-applied-configuration"],
        .status
      )' \
    | mcpruntime_org_kubectl apply --server-side --force-conflicts -f -
  echo "restored $label"
}

mcpruntime_org_backup_warn_certificates() {
  if [[ "$MCP_TLS_DRY_RUN" == "1" ]]; then
    return 0
  fi
  local had_tls_backup=0
  for f in registry-tls.yaml mcp-sentinel-platform-tls.yaml; do
    if [[ -f "$MCP_TLS_SNAPSHOT_DIR/$f" ]]; then
      had_tls_backup=1
    fi
  done
  if [[ "$had_tls_backup" == "0" ]]; then
    echo "warning: no TLS secret backups found; setup may request new Let's Encrypt certificates" >&2
    echo "         LE rate limit is 5 duplicate certs / 7 days per domain set" >&2
    return 0
  fi
  local cert ns name ready
  for cert in registry/registry-cert mcp-sentinel/mcp-sentinel-platform-tls; do
    ns="${cert%%/*}"
    name="${cert##*/}"
    if ! mcpruntime_org_kubectl get certificate "$name" -n "$ns" >/dev/null 2>&1; then
      continue
    fi
    ready="$(mcpruntime_org_kubectl get certificate "$name" -n "$ns" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
    if [[ "$ready" != "True" ]]; then
      echo "warning: certificate ${ns}/${name} is not Ready (status=${ready:-unknown})" >&2
    fi
  done
}

mcpruntime_org_restore_platform_runtime() {
  if ! mcpruntime_org_backup_resolve_dir; then
    echo "error: no platform backup found under $MCP_TLS_BACKUP_ROOT" >&2
    echo "       expected $MCP_TLS_BACKUP_ROOT/latest or flat registry-tls.yaml" >&2
    exit 1
  fi
  echo "Restoring platform-runtime backups from $MCP_TLS_SNAPSHOT_DIR"

  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/letsencrypt-prod-clusterissuer.yaml" "letsencrypt-prod ClusterIssuer"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/registry-tls.yaml" "registry TLS secret"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-platform-tls.yaml" "platform UI TLS secret"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/registry-cert.yaml" "registry Certificate"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-platform-cert.yaml" "platform UI Certificate"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-config.yaml" "mcp-sentinel-config"
  mcpruntime_org_backup_strip_and_apply "$MCP_TLS_SNAPSHOT_DIR/mcp-sentinel-secrets.yaml" "mcp-sentinel-secrets"

  mcpruntime_org_backup_warn_certificates

  if [[ -f "$MCP_TLS_SNAPSHOT_DIR/platform-auth.env" ]]; then
    echo ""
    echo "OIDC exports: $MCP_TLS_SNAPSHOT_DIR/platform-auth.env"
    echo "Ensure config/deployments/mcpruntime-org.env includes GOOGLE_CLIENT_ID / OIDC_* when needed."
  fi

  if [[ "$MCP_TLS_DRY_RUN" != "1" ]]; then
    echo ""
    echo "Restart Sentinel API/UI so restored platform secrets and config take effect:"
    echo "  kubectl --kubeconfig \"$KUBECONFIG\" rollout restart deployment/mcp-platform-api deployment/mcp-runtime-api deployment/mcp-analytics-api deployment/mcp-sentinel-ui -n mcp-sentinel"
  fi
}
