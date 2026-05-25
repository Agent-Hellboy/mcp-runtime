#!/usr/bin/env bash
# Targeted rollout for mcpruntime.org k3s: build/push Sentinel API+UI and apply RBAC/config.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ENV_FILE="${MCP_DEPLOY_ENV:-config/deployments/mcpruntime-org.env}"
ENV_EXAMPLE="config/deployments/mcpruntime-org.env.example"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a && source "$ENV_FILE" && set +a
elif [[ -f "$ENV_EXAMPLE" ]]; then
  set -a && source "$ENV_EXAMPLE" && set +a
fi

: "${KUBECONFIG:?set KUBECONFIG or source mcpruntime-org.env}"
TAG="${MCP_ROLLOUT_TAG:-verify-$(date +%m%d%H%M)}"
PLATFORM="${MCP_IMAGE_PLATFORM:-linux/amd64}"

resolve_registry_internal() {
  if [[ -n "${MCP_REGISTRY_INTERNAL:-}" ]]; then
    printf '%s' "$MCP_REGISTRY_INTERNAL"
    return
  fi
  local cluster_ip
  cluster_ip="$(kubectl --kubeconfig "$KUBECONFIG" get svc -n registry registry -o jsonpath='{.spec.clusterIP}')"
  local port
  port="$(kubectl --kubeconfig "$KUBECONFIG" get svc -n registry registry -o jsonpath='{.spec.ports[0].port}')"
  if [[ -z "$cluster_ip" || -z "$port" ]]; then
    echo "failed to resolve registry ClusterIP from registry/registry service" >&2
    exit 1
  fi
  printf '%s:%s' "$cluster_ip" "$port"
}

REGISTRY_INTERNAL="$(resolve_registry_internal)"

echo "kubeconfig: $KUBECONFIG"
echo "rollout tag: $TAG"
echo "registry: $REGISTRY_INTERNAL"

if [[ ! -x ./bin/mcp-runtime ]]; then
  go build -o bin/mcp-runtime ./cmd/mcp-runtime
fi

kubectl --kubeconfig "$KUBECONFIG" apply -f k8s/08-api-rbac.yaml

REGISTRY_HOST="${MCP_REGISTRY_INGRESS_HOST:-}"
if [[ -z "$REGISTRY_HOST" && -n "${MCP_PLATFORM_DOMAIN:-}" ]]; then
  REGISTRY_HOST="registry.${MCP_PLATFORM_DOMAIN}"
fi
REGISTRY_HOST="${REGISTRY_HOST:-registry.mcpruntime.org}"

kubectl --kubeconfig "$KUBECONFIG" patch configmap mcp-sentinel-config -n mcp-sentinel --type merge -p "$(cat <<PATCH
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

PF_PORT="${MCP_REGISTRY_PF_PORT:-15000}"
PF_PID=""
cleanup() {
  if [[ -n "$PF_PID" ]]; then
    kill "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

registry_reachable() {
  curl -k -sf "https://127.0.0.1:${PF_PORT}/v2/" >/dev/null 2>&1
}

if ! registry_reachable; then
  echo "Starting registry port-forward on 127.0.0.1:${PF_PORT}..."
  kubectl --kubeconfig "$KUBECONFIG" port-forward -n registry svc/registry "${PF_PORT}:5000" &
  PF_PID=$!
  for _ in {1..30}; do
    if registry_reachable; then
      break
    fi
    sleep 1
  done
fi

if ! registry_reachable; then
  echo "registry port-forward on 127.0.0.1:${PF_PORT} is not reachable" >&2
  echo "free the port or set MCP_REGISTRY_PF_PORT to an unused value" >&2
  exit 1
fi

push_image() {
  local name="$1"
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock --add-host=host.docker.internal:host-gateway \
    quay.io/skopeo/stable:v1.14 copy \
    "docker-daemon:${REGISTRY_INTERNAL}/${name}:${TAG}" \
    "docker://host.docker.internal:${PF_PORT}/${name}:${TAG}" \
    --dest-tls-verify=false
}

push_image mcp-sentinel-api
push_image mcp-sentinel-ui

kubectl --kubeconfig "$KUBECONFIG" set image deployment/mcp-sentinel-api -n mcp-sentinel \
  "api=${REGISTRY_INTERNAL}/mcp-sentinel-api:${TAG}"
kubectl --kubeconfig "$KUBECONFIG" set image deployment/mcp-sentinel-ui -n mcp-sentinel \
  "ui=${REGISTRY_INTERNAL}/mcp-sentinel-ui:${TAG}"

kubectl --kubeconfig "$KUBECONFIG" rollout status deployment/mcp-sentinel-api -n mcp-sentinel --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" rollout status deployment/mcp-sentinel-ui -n mcp-sentinel --timeout=180s

echo "Patching team namespace NetworkPolicies for ingress controller (${PLATFORM_TRAEFIK_NAMESPACE:-kube-system})..."
TRAEFIK_NS="${PLATFORM_TRAEFIK_NAMESPACE:-kube-system}"
for ns in $(kubectl --kubeconfig "$KUBECONFIG" get ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | grep '^mcp-team-'); do
  if ! kubectl --kubeconfig "$KUBECONFIG" get networkpolicy platform-default-deny -n "$ns" >/dev/null 2>&1; then
    continue
  fi
  if kubectl --kubeconfig "$KUBECONFIG" get networkpolicy platform-default-deny -n "$ns" -o json \
    | jq -e --arg traefik_ns "$TRAEFIK_NS" '
        (.spec.ingress // []) | any(.from[]?;
          .namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == $traefik_ns)
      ' >/dev/null; then
    continue
  fi
  kubectl --kubeconfig "$KUBECONFIG" patch networkpolicy platform-default-deny -n "$ns" --type='json' -p="[
    {\"op\":\"add\",\"path\":\"/spec/ingress/-\",\"value\":{\"from\":[{\"namespaceSelector\":{\"matchLabels\":{\"kubernetes.io/metadata.name\":\"${TRAEFIK_NS}\"}}}]}}
  ]"
done

echo "Rollout complete: api/ui tag ${TAG}"
