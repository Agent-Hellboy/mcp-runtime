#!/usr/bin/env bash
set -euo pipefail

# Build a tooling image and run the e2e script inside it.
# Requires Docker on the host; the container uses the host docker daemon via /var/run/docker.sock.

IMAGE_NAME="${IMAGE_NAME:-mcp-runtime-e2e}"
DOCKER_CONFIG="${DOCKER_CONFIG:-/tmp/docker-config}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

mkdir -p "${DOCKER_CONFIG}"

echo "[build] building e2e tooling image ${IMAGE_NAME}"
DOCKER_CONFIG="${DOCKER_CONFIG}" docker build -f test/e2e/Dockerfile -t "${IMAGE_NAME}" .

echo "[run] executing e2e-kind.sh inside container"
DOCKER_FLAGS=(-i --rm --privileged)
if [ -t 0 ]; then
  DOCKER_FLAGS=(-it --rm --privileged)
fi
DOCKER_BASE_ARGS=(--network=host -v /var/run/docker.sock:/var/run/docker.sock)
MOUNT_ARGS=(-v "${ROOT}":/workspace -w /workspace)

if ! DOCKER_CONFIG="${DOCKER_CONFIG}" docker run "${DOCKER_FLAGS[@]}" "${DOCKER_BASE_ARGS[@]}" \
  "${MOUNT_ARGS[@]}" "${IMAGE_NAME}" -lc "test -f test/e2e-kind.sh"; then
  echo "[warn] bind mount unavailable, running from image contents"
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker run "${DOCKER_FLAGS[@]}" "${DOCKER_BASE_ARGS[@]}" \
    "${IMAGE_NAME}" \
    test/e2e-kind.sh
else
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker run "${DOCKER_FLAGS[@]}" "${DOCKER_BASE_ARGS[@]}" \
    "${MOUNT_ARGS[@]}" "${IMAGE_NAME}" \
    test/e2e-kind.sh
fi
