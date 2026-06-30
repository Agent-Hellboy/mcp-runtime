#!/usr/bin/env bash
# Live-cluster authz matrix probe via Traefik gateway (split API services).
# Usage: bash hack/validate-authz-matrix.sh [base-url]
set -euo pipefail

BASE="${1:-http://127.0.0.1:18083}"
MATRIX="${MATRIX:-docs/security/authz-matrix.json}"

if [ ! -f "$MATRIX" ]; then
  echo "FAIL missing $MATRIX"
  exit 1
fi

NS="${NAMESPACE:-mcp-sentinel}"
if ! curl -fsS -o /dev/null "${BASE}/" 2>/dev/null; then
  echo "Port-forward Traefik gateway: kubectl -n $NS port-forward svc/mcp-sentinel-gateway 18083:8083"
  kubectl -n "$NS" port-forward svc/mcp-sentinel-gateway 18083:8083 >/tmp/pf-gateway-authz.log 2>&1 &
  sleep 2
fi

ADMIN_KEY=$(kubectl -n "$NS" get secret mcp-sentinel-secrets -o jsonpath='{.data.ADMIN_API_KEYS}' | base64 -d | cut -d, -f1)
UI_KEY=$(kubectl -n "$NS" get secret mcp-sentinel-secrets -o jsonpath='{.data.UI_API_KEY}' | base64 -d)
INGEST_KEY=$(kubectl -n "$NS" get secret mcp-sentinel-secrets -o jsonpath='{.data.INGEST_API_KEYS}' | base64 -d | cut -d, -f1)

pass=0
fail=0

while IFS= read -r row; do
  path=$(echo "$row" | jq -r .path)
  method=$(echo "$row" | jq -r .method)
  role=$(echo "$row" | jq -r .role)
  want=$(echo "$row" | jq -r .expect)
  headers=()
  case "$role" in
    anon) ;;
    user-key) headers=(-H "x-api-key: $UI_KEY") ;;
    admin-key) headers=(-H "x-api-key: $ADMIN_KEY") ;;
    ingest-key) headers=(-H "x-api-key: $INGEST_KEY") ;;
    *) echo "SKIP unknown role $role for $method $path"; continue ;;
  esac
  got=$(curl -sS -o /dev/null -w '%{http_code}' -X "$method" "${headers[@]}" "${BASE}${path}" || echo "000")
  if [ "$got" = "$want" ]; then
    echo "PASS $method $path role=$role"
    pass=$((pass + 1))
  else
    echo "FAIL $method $path role=$role expected=$want got=$got"
    fail=$((fail + 1))
  fi
done < <(jq -c '.[]' "$MATRIX")

echo "=== authz-matrix SUMMARY pass=$pass fail=$fail ==="
[ "$fail" -eq 0 ]
