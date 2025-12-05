#!/usr/bin/env bash
set -euo pipefail

# End-to-end check on a fresh kind cluster: deploy platform, push example app, deploy MCPServer, verify response.
# For Kind, we load the operator image directly to avoid containerd registry trust issues.

# Ensure we're in project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"
echo "[info] Running from: ${PROJECT_ROOT}"

CLUSTER_NAME="mcp-e2e"
KIND_CONFIG="$(mktemp)"
ORIG_CONTEXT="$(kubectl config current-context 2>/dev/null || true)"

cleanup() {
  kubectl config use-context "${ORIG_CONTEXT}" >/dev/null 2>&1 || true
  kind delete cluster --name "${CLUSTER_NAME}" >/dev/null 2>&1 || true
  rm -f "${KIND_CONFIG}"
}
trap cleanup EXIT

cat > "${KIND_CONFIG}" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
    endpoint = ["http://registry.registry.svc.cluster.local:5000"]
EOF

echo "[kind] creating cluster ${CLUSTER_NAME} with registry mirror"
kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
kubectl config use-context "kind-${CLUSTER_NAME}"

echo "[build] rebuilding CLI"
go build -o bin/mcp-runtime ./cmd/mcp-runtime

# Build and load operator image DIRECTLY into kind (bypasses registry for operator)
echo "[operator] building operator image locally"
docker build -t mcp-runtime-operator:latest -f Dockerfile.operator .

# Tag with full name that Kubernetes will use
echo "[operator] tagging image with full registry path"
docker tag mcp-runtime-operator:latest docker.io/library/mcp-runtime-operator:latest

echo "[operator] loading operator image into kind (bypassing registry)"
kind load docker-image docker.io/library/mcp-runtime-operator:latest --name "${CLUSTER_NAME}"

# Deploy registry for app images
echo "[registry] deploying registry namespace and manifests"
kubectl apply -k config/registry/

echo "[registry] waiting for registry to be ready"
kubectl rollout status deploy/registry -n registry --timeout=180s

# Note: For Kind, we load images directly instead of using the registry
# because Kind's containerd doesn't easily trust HTTP registries.
# The registry is still deployed as part of setup verification.

# Verify image was loaded
echo "[debug] verifying operator image is available in kind"
docker exec "${CLUSTER_NAME}-control-plane" crictl images | grep -E "mcp-runtime-operator|REPOSITORY" || true

# Run setup with pre-loaded operator image (skip building/pushing operator)
# Note: kind stores images with docker.io/library/ prefix for local images
echo "[setup] running platform setup with pre-loaded operator image"
export OPERATOR_IMG="docker.io/library/mcp-runtime-operator:latest"
export SKIP_OPERATOR_BUILD="1"
./bin/mcp-runtime setup
SETUP_EXIT=$?

# Debug: Check pod status right after setup
echo "[debug] Operator pod status after setup:"
kubectl -n mcp-runtime get pods -o wide || true
kubectl -n mcp-runtime describe pod -l control-plane=controller-manager 2>/dev/null | grep -A 20 "Events:" || true

if [ $SETUP_EXIT -ne 0 ]; then
  echo "[debug] Setup failed (exit code: $SETUP_EXIT)"
  exit 1
fi

echo "[operator] waiting for operator deployment"
kubectl rollout status deploy/mcp-runtime-operator-controller-manager -n mcp-runtime --timeout=180s
ROLLOUT_EXIT=$?
if [ $ROLLOUT_EXIT -ne 0 ]; then
  echo "[debug] Operator rollout failed (exit code: $ROLLOUT_EXIT), checking pod status..."
  kubectl -n mcp-runtime get pods || true
  kubectl -n mcp-runtime describe pod -l control-plane=controller-manager | tail -30 || true
  exit 1
fi

echo "[image] building example app image"
docker build -t example-app:latest -f examples/example-app/Dockerfile examples/example-app

# Tag with full name that Kubernetes will use
docker tag example-app:latest docker.io/library/example-app:latest

echo "[image] loading example app into kind (bypassing registry for Kind)"
kind load docker-image docker.io/library/example-app:latest --name "${CLUSTER_NAME}"

echo "[cr] applying MCPServer for example app (using local image)"
# Note: kind stores local images with docker.io/library/ prefix
cat <<EOF | kubectl apply -f -
apiVersion: mcp.agent-hellboy.io/v1alpha1
kind: MCPServer
metadata:
  name: example-mcp-server
  namespace: mcp-servers
spec:
  image: docker.io/library/example-app
  imageTag: latest
  replicas: 1
  port: 8088
  servicePort: 80
  ingressHost: example.local
  ingressPath: /example-mcp-server/mcp
EOF

echo "[deploy] waiting for MCPServer deployment to be created and ready"
# Wait for deployment to be created (operator needs time for leader election)
for i in {1..30}; do
  if kubectl get deploy/example-mcp-server -n mcp-servers &>/dev/null; then
    echo "[deploy] deployment found, waiting for rollout..."
    break
  fi
  echo "[deploy] waiting for operator to create deployment... ($i/30)"
  sleep 2
done
kubectl rollout status deploy/example-mcp-server -n mcp-servers --timeout=180s

echo "[verify] curling service from inside cluster"
kubectl -n mcp-servers run curl --rm -i --image=curlimages/curl --restart=Never --command -- \
  sh -c "curl -s http://example-mcp-server.mcp-servers.svc.cluster.local/" | tee /tmp/mcp-e2e-curl.log

# Ensure Traefik is ready before port-forwarding to it
echo "[verify] waiting for Traefik ingress controller to be ready"
set +e
kubectl rollout status deploy/traefik -n traefik --timeout=180s
TRAEFIK_ROLLOUT_RC=$?
set -e
if [ ${TRAEFIK_ROLLOUT_RC} -ne 0 ]; then
  echo "[error] Traefik failed to roll out, collecting diagnostics..."
  kubectl -n traefik get pods -o wide || true
  kubectl -n traefik get rs -o wide || true
  kubectl -n traefik describe deploy/traefik || true
  kubectl -n traefik describe rs -l app=traefik || true
  kubectl -n traefik describe pod -l app=traefik || true
  kubectl -n traefik logs -l app=traefik --tail=200 || true
  kubectl -n traefik get events --sort-by=.metadata.creationTimestamp | tail -n 50 || true
  kubectl get nodes -o wide || true
  exit ${TRAEFIK_ROLLOUT_RC}
fi

echo "[verify] curling ingress via Traefik (port-forwarded)"
PF_LOG="$(mktemp)"
kubectl port-forward -n traefik svc/traefik 18080:8000 >"${PF_LOG}" 2>&1 &
PF_PID=$!
sleep 3
set +e
curl -i -H "Host: example.local" http://127.0.0.1:18080/example-mcp-server/mcp -o /tmp/mcp-e2e-ingress.html -w "HTTP:%{http_code}\n" -s | tee /tmp/mcp-e2e-ingress.log
ING_RC=${PIPESTATUS[0]}
set -e
kill "${PF_PID}" >/dev/null 2>&1 || true
wait "${PF_PID}" 2>/dev/null || true
if [ "${ING_RC}" -ne 0 ]; then
  echo "[error] ingress curl failed, port-forward log:"
  cat "${PF_LOG}" || true
  exit 1
fi

echo "[done] E2E completed successfully"
