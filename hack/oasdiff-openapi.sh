#!/usr/bin/env bash
# Compare committed OpenAPI specs against merge-base for breaking changes (M3.3).
# Skips when oasdiff is not installed. Usage: bash hack/oasdiff-openapi.sh [base-ref]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BASE_REF="${1:-}"
if [ -z "$BASE_REF" ]; then
  if git rev-parse --verify origin/main >/dev/null 2>&1; then
    BASE_REF="$(git merge-base HEAD origin/main)"
  else
    echo "SKIP oasdiff: no base ref and origin/main unavailable"
    exit 0
  fi
fi

if ! command -v oasdiff >/dev/null 2>&1; then
  echo "SKIP oasdiff: oasdiff not installed (brew install oasdiff or go install github.com/oasdiff/oasdiff/cmd/oasdiff@latest)"
  exit 0
fi

specs=(
  services/platform-api/openapi.yaml
  services/runtime-control/openapi.yaml
  services/analytics-api/openapi.yaml
)

failed=0
for spec in "${specs[@]}"; do
  if ! git cat-file -e "${BASE_REF}:${spec}" 2>/dev/null; then
    echo "SKIP $spec (no base version at ${BASE_REF})"
    continue
  fi
  base_file="$(mktemp)"
  trap 'rm -f "$base_file"' RETURN
  git show "${BASE_REF}:${spec}" >"$base_file"
  echo "=== oasdiff breaking ${spec} (base ${BASE_REF}) ==="
  if ! oasdiff breaking "$base_file" "$spec" -o text; then
    failed=1
  fi
  rm -f "$base_file"
done

if [ "$failed" -ne 0 ]; then
  echo "FAIL oasdiff detected breaking OpenAPI changes"
  exit 1
fi
echo "PASS oasdiff OpenAPI breaking-change check"
