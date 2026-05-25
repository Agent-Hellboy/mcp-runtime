#!/usr/bin/env bash
# Run MCP Runtime setup for the mcpruntime.org k3s public deployment.
# Sources config/deployments/mcpruntime-org.env when present (copy from .example).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ENV_FILE="${MCP_DEPLOY_ENV:-config/deployments/mcpruntime-org.env}"
ENV_EXAMPLE="config/deployments/mcpruntime-org.env.example"

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a && source "$ENV_FILE" && set +a
elif [[ -f "$ENV_EXAMPLE" ]]; then
  echo "warning: $ENV_FILE not found; using $ENV_EXAMPLE"
  echo "         copy $ENV_EXAMPLE to $ENV_FILE to persist local overrides"
  # shellcheck disable=SC1090
  set -a && source "$ENV_EXAMPLE" && set +a
else
  echo "error: missing $ENV_FILE and $ENV_EXAMPLE" >&2
  exit 1
fi

: "${MCP_PLATFORM_DOMAIN:?MCP_PLATFORM_DOMAIN is required}"
: "${MCP_SETUP_KUBECONFIG:=${KUBECONFIG:-}}"
: "${MCP_SETUP_KUBECONFIG:?set KUBECONFIG or MCP_SETUP_KUBECONFIG in the env file}"

if [[ ! -x ./bin/mcp-runtime ]]; then
  echo "building ./bin/mcp-runtime ..."
  go build -o bin/mcp-runtime ./cmd/mcp-runtime
fi

if ! kubectl --kubeconfig "$MCP_SETUP_KUBECONFIG" get nodes >/dev/null 2>&1; then
  echo "error: cannot reach cluster with kubeconfig $MCP_SETUP_KUBECONFIG" >&2
  exit 1
fi

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

echo "MCP platform domain: $MCP_PLATFORM_DOMAIN"
echo "kubeconfig: $MCP_SETUP_KUBECONFIG"
echo "registry host (derived): registry.${MCP_PLATFORM_DOMAIN}"
echo "running: ./bin/mcp-runtime ${SETUP_ARGS[*]}"

MCP_SETUP_WAIT_TIMEOUT="${MCP_SETUP_WAIT_TIMEOUT:-900}" \
  ./bin/mcp-runtime "${SETUP_ARGS[@]}"
