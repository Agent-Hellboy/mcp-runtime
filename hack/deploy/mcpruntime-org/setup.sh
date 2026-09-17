#!/usr/bin/env bash
# Run MCP Runtime setup for the mcpruntime.org k3s public deployment.
# Sources config/deployments/mcpruntime-org.env when present (copy from .example).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/env.sh
source "$SCRIPT_DIR/lib/env.sh"
ROOT="$(mcpruntime_org_repo_root)"

mcpruntime_org_load_env 0

: "${MCP_PLATFORM_DOMAIN:?MCP_PLATFORM_DOMAIN is required}"
MCP_SETUP_KUBECONFIG="${MCP_SETUP_KUBECONFIG:-$KUBECONFIG}"

cd "$ROOT"
echo "building ./bin/mcp-runtime ..."
go build -o bin/mcp-runtime ./cmd/mcp-runtime

mcpruntime_org_require_cluster

# Reuse existing Let's Encrypt certs — do not pass --acme-email on reruns.
SETUP_ARGS=(
  setup
  --kubeconfig "$MCP_SETUP_KUBECONFIG"
  --with-tls
  --tls-cluster-issuer "${MCP_SETUP_TLS_CLUSTER_ISSUER:-letsencrypt-prod}"
  --registry-mode "${MCP_SETUP_REGISTRY_MODE:-bundled-https}"
  --platform-mode "${MCP_SETUP_PLATFORM_MODE:-tenant}"
  --ingress "${MCP_SETUP_INGRESS:-none}"
)

if [[ "${MCP_SETUP_SKIP_CERT_MANAGER_INSTALL:-}" == "1" ]]; then
  SETUP_ARGS+=(--skip-cert-manager-install)
fi

if [[ "${MCP_SETUP_WITH_MCP_AUTH_SERVER:-}" == "1" ]]; then
  : "${MCP_SETUP_MCP_AUTH_ISSUER_URL:?MCP_SETUP_MCP_AUTH_ISSUER_URL is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  : "${MCP_SETUP_MCP_AUTH_RESOURCE_URL:?MCP_SETUP_MCP_AUTH_RESOURCE_URL is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  : "${MCP_SETUP_MCP_AUTH_TLS_SECRET:?MCP_SETUP_MCP_AUTH_TLS_SECRET is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  : "${MCP_SETUP_MCP_AUTH_SIGNING_KEY_SECRET:?MCP_SETUP_MCP_AUTH_SIGNING_KEY_SECRET is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  : "${MCP_SETUP_MCP_AUTH_CONNECTORS_FILE:?MCP_SETUP_MCP_AUTH_CONNECTORS_FILE is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  : "${MCP_SETUP_MCP_AUTH_CONNECTOR:?MCP_SETUP_MCP_AUTH_CONNECTOR is required when MCP_SETUP_WITH_MCP_AUTH_SERVER=1}"
  SETUP_ARGS+=(
    --with-mcp-auth-server
    --mcp-auth-issuer-url "$MCP_SETUP_MCP_AUTH_ISSUER_URL"
    --mcp-auth-resource-url "$MCP_SETUP_MCP_AUTH_RESOURCE_URL"
    --mcp-auth-tls-secret "$MCP_SETUP_MCP_AUTH_TLS_SECRET"
    --mcp-auth-signing-key-secret "$MCP_SETUP_MCP_AUTH_SIGNING_KEY_SECRET"
    --mcp-auth-connectors-file "$MCP_SETUP_MCP_AUTH_CONNECTORS_FILE"
    --mcp-auth-connector "$MCP_SETUP_MCP_AUTH_CONNECTOR"
  )
  if [[ -n "${MCP_SETUP_MCP_AUTH_SERVER_IMAGE:-}" ]]; then
    SETUP_ARGS+=(--mcp-auth-server-image "$MCP_SETUP_MCP_AUTH_SERVER_IMAGE")
  fi
fi

echo "MCP platform domain: $MCP_PLATFORM_DOMAIN"
echo "kubeconfig: $MCP_SETUP_KUBECONFIG"
echo "registry host (derived): registry.${MCP_PLATFORM_DOMAIN}"
echo "running: ./bin/mcp-runtime ${SETUP_ARGS[*]}"

MCP_SETUP_WAIT_TIMEOUT="${MCP_SETUP_WAIT_TIMEOUT:-900}" \
  ./bin/mcp-runtime "${SETUP_ARGS[@]}"

if [[ "${MCP_RESTORE_TLS_AFTER_SETUP:-1}" == "1" ]] && mcpruntime_org_has_platform_backup; then
  echo "Restoring platform-runtime backup ..."
  MCP_DEPLOY_ENV="$MCP_DEPLOY_ENV_FILE" "$SCRIPT_DIR/restore.sh"
fi
