#!/usr/bin/env bash
# Re-apply platform-runtime backups (TLS, certs, OIDC config) after setup.
#
# Typical use: called automatically by hack/deploy/mcpruntime-org/setup.sh when a
# backup snapshot exists. Can also be run manually after setup.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/backup.sh
source "$SCRIPT_DIR/lib/backup.sh"

DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: hack/deploy/mcpruntime-org/restore.sh [--dry-run]

  --dry-run   Print restore actions without applying resources.

Environment (via config/deployments/mcpruntime-org.env):
  KUBECONFIG            cluster kubeconfig path (required)
  MCP_TLS_BACKUP_DIR    backup root (default: ~/.mcpruntime/backups/mcpruntime-org)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
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

mcpruntime_org_load_env 1
MCP_TLS_DRY_RUN="$DRY_RUN"
mcpruntime_org_require_cluster
mcpruntime_org_restore_platform_runtime
