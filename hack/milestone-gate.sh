#!/usr/bin/env bash
# Full milestone gate: live Kind cluster checks + Trivy image scans for split API services.
# Usage: bash hack/milestone-gate.sh [validate-api-split-milestone.sh args]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SCOPE="${1:-all}"

echo "=== Step 1/2: Trivy sentinel API images ==="
bash hack/trivy-sentinel-images.sh platform-api analytics-api runtime-control

echo "=== Step 2/2: Live cluster validation ($SCOPE) ==="
bash hack/validate-api-split-milestone.sh "$SCOPE"

echo "=== MILESTONE GATE OK ==="
