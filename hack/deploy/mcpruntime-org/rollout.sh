#!/usr/bin/env bash
# Targeted rollout for mcpruntime.org k3s: build/push Sentinel API+UI and apply RBAC/config.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/env.sh
source "$SCRIPT_DIR/lib/env.sh"
# shellcheck source=lib/registry.sh
source "$SCRIPT_DIR/lib/registry.sh"

mcpruntime_org_load_env 0

ROOT="$(mcpruntime_org_repo_root)"
TAG="${MCP_ROLLOUT_TAG:-verify-$(date +%m%d%H%M)}"
PLATFORM="${MCP_IMAGE_PLATFORM:-linux/amd64}"
REGISTRY_INTERNAL="$(mcpruntime_org_resolve_registry_internal)"
REGISTRY_HOST="$(mcpruntime_org_registry_host)"
PF_PORT="${MCP_REGISTRY_PF_PORT:-15000}"

cleanup() {
  mcpruntime_org_registry_port_forward_stop "$MCP_REGISTRY_PF_PID"
}
trap cleanup EXIT

cd "$ROOT"
echo "kubeconfig: $KUBECONFIG"
echo "rollout tag: $TAG"
echo "registry internal: $REGISTRY_INTERNAL"
echo "registry host: $REGISTRY_HOST"

echo "building ./bin/mcp-runtime ..."
go build -o bin/mcp-runtime ./cmd/mcp-runtime

mcpruntime_org_kubectl apply -f k8s/08-api-rbac.yaml
mcpruntime_org_ensure_platform_pull_secret

mcpruntime_org_kubectl patch configmap mcp-sentinel-config -n mcp-sentinel --type merge -p "$(cat <<PATCH
{
  "data": {
    "PLATFORM_TEAM_TRAEFIK_WATCH": "${PLATFORM_TEAM_TRAEFIK_WATCH:-disabled}",
    "PLATFORM_TRAEFIK_NAMESPACE": "${PLATFORM_TRAEFIK_NAMESPACE:-kube-system}",
    "MCP_REGISTRY_ENDPOINT": "${REGISTRY_HOST}",
    "MCP_REGISTRY_INGRESS_HOST": "${REGISTRY_HOST}"
  }
}
PATCH
)"

echo "Building mcp-sentinel-api:${TAG} (${PLATFORM})..."
docker build --platform "$PLATFORM" -f services/api/Dockerfile -t "${REGISTRY_INTERNAL}/mcp-sentinel-api:${TAG}" .

echo "Building mcp-sentinel-ui:${TAG} (${PLATFORM})..."
docker build --platform "$PLATFORM" -f services/ui/Dockerfile -t "${REGISTRY_INTERNAL}/mcp-sentinel-ui:${TAG}" .

MCP_REGISTRY_PF_PID="$(mcpruntime_org_registry_ensure_port_forward "$PF_PORT" "$MCP_REGISTRY_PF_PID")"

if ! mcpruntime_org_registry_push_via_port_forward "$REGISTRY_INTERNAL" "$PF_PORT" "mcp-sentinel-api" "$TAG"; then
  echo "failed to push mcp-sentinel-api:${TAG}" >&2
  exit 1
fi
if ! mcpruntime_org_registry_push_via_port_forward "$REGISTRY_INTERNAL" "$PF_PORT" "mcp-sentinel-ui" "$TAG"; then
  echo "failed to push mcp-sentinel-ui:${TAG}" >&2
  exit 1
fi

mcpruntime_org_kubectl set image deployment/mcp-sentinel-api -n mcp-sentinel \
  "api=${REGISTRY_HOST}/mcp-sentinel-api:${TAG}"
mcpruntime_org_kubectl set image deployment/mcp-sentinel-ui -n mcp-sentinel \
  "ui=${REGISTRY_HOST}/mcp-sentinel-ui:${TAG}"

mcpruntime_org_kubectl rollout status deployment/mcp-sentinel-api -n mcp-sentinel --timeout=180s
mcpruntime_org_kubectl rollout status deployment/mcp-sentinel-ui -n mcp-sentinel --timeout=180s

echo "Patching team namespace NetworkPolicies for ingress controller (${PLATFORM_TRAEFIK_NAMESPACE:-kube-system})..."
TRAEFIK_NS="${PLATFORM_TRAEFIK_NAMESPACE:-kube-system}"
for ns in $(mcpruntime_org_kubectl get ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | grep '^mcp-team-' || true); do
  if ! mcpruntime_org_kubectl get networkpolicy platform-default-deny -n "$ns" >/dev/null 2>&1; then
    continue
  fi
  if mcpruntime_org_kubectl get networkpolicy platform-default-deny -n "$ns" -o json \
    | jq -e --arg traefik_ns "$TRAEFIK_NS" '
        (.spec.ingress // []) | any(.from[]?;
          .namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == $traefik_ns)
      ' >/dev/null; then
    continue
  fi
  mcpruntime_org_kubectl patch networkpolicy platform-default-deny -n "$ns" --type='json' -p="[
    {\"op\":\"add\",\"path\":\"/spec/ingress/-\",\"value\":{\"from\":[{\"namespaceSelector\":{\"matchLabels\":{\"kubernetes.io/metadata.name\":\"${TRAEFIK_NS}\"}}}]}}
  ]"
done

echo "Rollout complete: api/ui tag ${TAG}"
