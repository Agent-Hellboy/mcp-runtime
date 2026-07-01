# mTLS datapath checks for Kind E2E. Sourced from kind.sh when the mtls scenario
# is selected. Requires test-mode setup (managed mcp-runtime-ca issuer).

MTLS_SERVER_NAME="${MTLS_SERVER_NAME:-mtls-mcp-server}"
MTLS_TRUST_DOMAIN="${MTLS_TRUST_DOMAIN:-cluster.local}"
TRAEFIK_TLS_PORT="${TRAEFIK_TLS_PORT:-18443}"
MTLS_AGENT_ID="${MTLS_AGENT_ID:-e2e-mtls-agent}"

# mtls_dump_diagnostics captures the control-plane state needed to debug an mtls
# datapath failure into WORKDIR, which the EXIT trap archives to the CI artifact
# bundle. Without this, an mtls failure (e.g. adapter cert issuance) leaves no
# server-side evidence in the artifacts and can only be diagnosed by local repro.
mtls_dump_diagnostics() {
  local rc=$?
  local out="${WORKDIR}/mtls-diagnostics"
  mkdir -p "${out}"
  echo "[mtls] capturing failure diagnostics (exit=${rc}) to ${out}" >&2
  {
    kubectl get mcpserver "${MTLS_SERVER_NAME}" -n mcp-servers -o yaml
  } >"${out}/mcpserver.yaml" 2>&1 || true
  kubectl get certificaterequests -n mcp-servers -o yaml >"${out}/certificaterequests.yaml" 2>&1 || true
  kubectl get certificates,secrets -n mcp-servers >"${out}/certs-and-secrets.txt" 2>&1 || true
  kubectl get configmap mcp-sentinel-config -n mcp-sentinel -o yaml >"${out}/sentinel-config.yaml" 2>&1 || true
  kubectl logs -n mcp-sentinel deploy/mcp-runtime-api --tail=400 >"${out}/runtime-api.log" 2>&1 || true
  kubectl logs -n mcp-sentinel deploy/mcp-platform-api --tail=200 >"${out}/platform-api.log" 2>&1 || true
  kubectl logs -n mcp-servers -l "app=${MTLS_SERVER_NAME}" --all-containers=true --tail=200 >"${out}/mtls-server.log" 2>&1 || true
  kubectl get events -n mcp-servers --sort-by=.lastTimestamp >"${out}/mcp-servers-events.txt" 2>&1 || true
  return "${rc}"
}

run_e2e_mtls_scenario() {
  # Surface server-side diagnostics on any unexpected failure in this scenario.
  # errtrace (set -E) propagates the ERR trap into functions and command
  # substitutions (e.g. the `$(... adapter enroll ...)` capture) so those
  # failures are archived too. This scenario runs last, so scoping is moot.
  set -E
  trap 'mtls_dump_diagnostics' ERR

  log_line mtls "verifying test-mode workload PKI"
  if ! kubectl get clusterissuer mcp-runtime-ca >/dev/null 2>&1; then
    echo "expected ClusterIssuer mcp-runtime-ca from test-mode setup" >&2
    exit 1
  fi
  kubectl wait --for=condition=Ready clusterissuer/mcp-runtime-ca --timeout=120s >/dev/null

  operator_issuer="$(kubectl get deploy/mcp-runtime-operator-controller-manager -n mcp-runtime \
    -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="MCP_MTLS_CLUSTER_ISSUER")].value}')"
  if [[ "${operator_issuer}" != "mcp-runtime-ca" ]]; then
    echo "operator MCP_MTLS_CLUSTER_ISSUER = ${operator_issuer:-<empty>}, want mcp-runtime-ca" >&2
    exit 1
  fi

  # runtime-api issues adapter/session certificates and reads the issuer from the
  # mcp-sentinel-config ConfigMap (via envFrom), so assert it there rather than on
  # an inline env var. An empty value makes `adapter enroll` fail with a 503.
  runtime_issuer="$(kubectl get configmap mcp-sentinel-config -n mcp-sentinel \
    -o jsonpath='{.data.MCP_MTLS_CLUSTER_ISSUER}' 2>/dev/null || true)"
  if [[ "${runtime_issuer}" != "mcp-runtime-ca" ]]; then
    echo "mcp-sentinel-config MCP_MTLS_CLUSTER_ISSUER = ${runtime_issuer:-<empty>}, want mcp-runtime-ca" >&2
    exit 1
  fi

  log_line mtls "deploying auth.mode mtls MCPServer ${MTLS_SERVER_NAME}"
  MTLS_METADATA="${WORKDIR}/mtls-metadata.yaml"
  MTLS_MANIFEST="${WORKDIR}/mtls-manifest.yaml"
  MTLS_SECRET_NAME="${MTLS_SERVER_NAME}-analytics-creds"
  MTLS_IMAGE="registry.registry.svc.cluster.local:5000/${MTLS_SERVER_NAME}:${E2E_WORKLOAD_TAG}"
  kubectl create secret generic "${MTLS_SECRET_NAME}" \
    -n mcp-servers \
    --from-literal=api-key="${INGEST_API_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -
  cat > "${MTLS_METADATA}" <<EOF
version: v1
servers:
  - name: ${MTLS_SERVER_NAME}
    image: ${MTLS_IMAGE%:*}
    imageTag: ${MTLS_IMAGE##*:}
    route: /${MTLS_SERVER_NAME}/mcp
    publicPathPrefix: ${MTLS_SERVER_NAME}
    port: 8090
    namespace: mcp-servers
    resources:
      requests:
        cpu: 1m
        memory: 32Mi
    envVars:
      - name: PORT
        value: "8090"
      - name: MCP_PATH
        value: "/${MTLS_SERVER_NAME}/mcp"
    tools:
      - name: aaa-ping
        requiredTrust: low
        sideEffect: read
    auth:
      mode: mtls
      trustDomain: ${MTLS_TRUST_DOMAIN}
    policy:
      mode: allow-list
      defaultDecision: deny
      policyVersion: v1
    session:
      required: true
    gateway:
      enabled: true
      resources:
        requests:
          cpu: 1m
          memory: 32Mi
    analytics:
      ingestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
      apiKeySecretRef:
        name: ${MTLS_SECRET_NAME}
        key: api-key
EOF

  if ! docker image inspect "${MTLS_IMAGE}" >/dev/null 2>&1; then
    run_logged_stage "build mtls MCP server image" ./bin/mcp-runtime server build image "${MTLS_SERVER_NAME}" \
      --metadata-file "${MTLS_METADATA}" \
      --dockerfile "${GO_EXAMPLE_SOURCE_DIR}/Dockerfile" \
      --registry registry.registry.svc.cluster.local:5000 \
      --tag "${E2E_WORKLOAD_TAG}" \
      --context "${GO_EXAMPLE_SOURCE_DIR}"
  fi
  prune_kind_image "${MTLS_IMAGE}"
  load_image_into_kind "${MTLS_IMAGE}"

  run_logged_stage "deploy mtls MCP server manifests" bash -lc \
    "MCP_RUNTIME_CONFIG_DIR=\"${E2E_PIPELINE_CONFIG_DIR}\" ./bin/mcp-runtime server generate --metadata-file \"${MTLS_METADATA}\" --output \"${WORKDIR}/mtls-manifests\" && ./bin/mcp-runtime server --use-kube apply --file \"${WORKDIR}/mtls-manifests/${MTLS_SERVER_NAME}.yaml\""
  wait_for_named_server_ready "${MTLS_SERVER_NAME}" "mcp-servers" 240

  log_line mtls "waiting for gateway and trust-bundle certificates"
  deadline=$((SECONDS + 240))
  while (( SECONDS < deadline )); do
    if kubectl get secret "${MTLS_SERVER_NAME}-gateway-mtls" -n mcp-servers >/dev/null 2>&1 \
      && kubectl get secret "${MTLS_SERVER_NAME}-mtls-ca" -n mcp-servers >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done
  if ! kubectl get secret "${MTLS_SERVER_NAME}-gateway-mtls" -n mcp-servers >/dev/null 2>&1 \
    || ! kubectl get secret "${MTLS_SERVER_NAME}-mtls-ca" -n mcp-servers >/dev/null 2>&1; then
    echo "timed out waiting for gateway TLS secrets (${MTLS_SERVER_NAME}-gateway-mtls and/or ${MTLS_SERVER_NAME}-mtls-ca)" >&2
    exit 1
  fi

  # The mtls datapath only needs Traefik's websecure (TLS) entrypoint; it never
  # uses the plaintext web port-forward, so don't gate the scenario on it.
  if [[ -z "${TRAEFIK_TLS_PORT_FORWARD_PID:-}" ]]; then
    echo "[port-forward] exposing traefik websecure on localhost:${TRAEFIK_TLS_PORT}"
    port_forward_bg traefik traefik "${TRAEFIK_TLS_PORT}" 8443 "${WORKDIR}/traefik-tls-port-forward.log"
    TRAEFIK_TLS_PORT_FORWARD_PID="${LAST_MANAGED_PID}"
  fi
  wait_port "${TRAEFIK_TLS_PORT}"

  MTLS_INGRESS_PATH="/${MTLS_SERVER_NAME}/mcp"
  MTLS_URL="https://127.0.0.1:${TRAEFIK_TLS_PORT}${MTLS_INGRESS_PATH}"
  MTLS_CA_FILE="${WORKDIR}/mtls-ca.crt"
  kubectl get secret "${MTLS_SERVER_NAME}-mtls-ca" -n mcp-servers -o jsonpath='{.data.ca\.crt}' | decode_base64 >"${MTLS_CA_FILE}"

  log_line mtls "rejecting initialize without a client certificate"
  if curl -fsS --cacert "${MTLS_CA_FILE}" \
    -H "Host: ${SERVER_HOST}" \
    -H "content-type: application/json" \
    -H "accept: application/json, text/event-stream" \
    -H "Mcp-Protocol-Version: ${MCP_PROTOCOL_VERSION}" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"'"${MCP_PROTOCOL_VERSION}"'","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}' \
    "${MTLS_URL}" >/dev/null 2>&1; then
    echo "expected initialize without client certificate to fail" >&2
    exit 1
  fi

  ensure_gateway_port_forward
  ensure_api_port_forward
  if [[ -z "${ADAPTER_PLATFORM_TOKEN:-}" ]]; then
    ADAPTER_PLATFORM_TOKEN="$(PLATFORM_ADMIN_EMAIL="${PLATFORM_ADMIN_EMAIL}" PLATFORM_ADMIN_PASSWORD="${PLATFORM_ADMIN_PASSWORD}" python3 -c '
import json, os
print(json.dumps({"email": os.environ["PLATFORM_ADMIN_EMAIL"], "password": os.environ["PLATFORM_ADMIN_PASSWORD"]}))
' | curl -fsS -X POST \
      -H "content-type: application/json" \
      --data-binary @- \
      "http://127.0.0.1:${API_SERVICE_PORT}/api/v1/auth/login" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"
  fi

  log_line mtls "applying grant and adapter session for mtls datapath"
  cat >"${WORKDIR}/mtls-grant.yaml" <<EOF
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: ${MTLS_SERVER_NAME}-grant
  namespace: mcp-servers
spec:
  serverRef:
    name: ${MTLS_SERVER_NAME}
  subject:
    agentID: ${MTLS_AGENT_ID}
  maxTrust: low
  allowedSideEffects: [read]
  policyVersion: v1
  toolRules:
    - name: aaa-ping
      decision: allow
EOF
  (cd "${WORKDIR}" && "${PROJECT_ROOT}/bin/mcp-runtime" access --use-kube grant apply --file mtls-grant.yaml)
  wait_for_policy_text "\"name\": \"${MTLS_SERVER_NAME}-grant\"" "${MTLS_SERVER_NAME}"

  MTLS_CERT_DIR="${WORKDIR}/mtls-certs"
  mkdir -p "${MTLS_CERT_DIR}"
  MTLS_ENROLL_OUT="$(MCP_PLATFORM_API_URL="http://127.0.0.1:${SENTINEL_PORT}" \
  MCP_PLATFORM_API_TOKEN="${ADAPTER_PLATFORM_TOKEN}" \
    ./bin/mcp-runtime adapter enroll \
      --server "${MTLS_SERVER_NAME}" \
      --namespace mcp-servers \
      --agent "${MTLS_AGENT_ID}" \
      --trust-domain "${MTLS_TRUST_DOMAIN}" \
      --output-dir "${MTLS_CERT_DIR}")"
  MTLS_SESSION_NAME="$(printf '%s\n' "${MTLS_ENROLL_OUT}" | python3 -c 'import re,sys; m=re.search(r"/session/([A-Za-z0-9._-]+)", sys.stdin.read()); print(m.group(1) if m else ""); sys.exit(0 if m else 1)')"
  wait_for_policy_text "\"name\": \"${MTLS_SESSION_NAME}\"" "${MTLS_SERVER_NAME}"

  MTLS_CLIENT_CERT="${MTLS_CERT_DIR}/client.crt"
  MTLS_CLIENT_KEY="${MTLS_CERT_DIR}/client.key"
  MTLS_CLIENT_CA="${MTLS_CERT_DIR}/ca.crt"
  for f in "${MTLS_CLIENT_CERT}" "${MTLS_CLIENT_KEY}" "${MTLS_CLIENT_CA}"; do
    if [[ ! -s "${f}" ]]; then
      echo "adapter enroll did not write ${f}" >&2
      exit 1
    fi
  done

  log_line mtls "accepting initialize with session-bound client certificate"
  init_body="$(curl -fsS \
    --cert "${MTLS_CLIENT_CERT}" \
    --key "${MTLS_CLIENT_KEY}" \
    --cacert "${MTLS_CLIENT_CA}" \
    -H "Host: ${SERVER_HOST}" \
    -H "content-type: application/json" \
    -H "accept: application/json, text/event-stream" \
    -H "Mcp-Protocol-Version: ${MCP_PROTOCOL_VERSION}" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"'"${MCP_PROTOCOL_VERSION}"'","capabilities":{},"clientInfo":{"name":"e2e-mtls","version":"1"}}}' \
    "${MTLS_URL}")"
  echo "${init_body}" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
if doc.get("error"):
    raise SystemExit(f"initialize failed: {doc}")
if "result" not in doc:
    raise SystemExit(f"missing initialize result: {doc}")
print("mtls initialize ok")
'

  log_line mtls "ignoring spoofed governance headers on mtls path"
  # Intentionally omit curl -f: the gateway is expected to reject the spoofed
  # headers with a 4xx/5xx, and we still need to capture the response body so the
  # Python check below can validate the error instead of being skipped.
  spoof_body="$(curl -sS \
    --cert "${MTLS_CLIENT_CERT}" \
    --key "${MTLS_CLIENT_KEY}" \
    --cacert "${MTLS_CLIENT_CA}" \
    -H "Host: ${SERVER_HOST}" \
    -H "content-type: application/json" \
    -H "accept: application/json, text/event-stream" \
    -H "Mcp-Protocol-Version: ${MCP_PROTOCOL_VERSION}" \
    -H "X-MCP-Human-ID: spoofed-human" \
    -H "X-MCP-Agent-ID: spoofed-agent" \
    -H "X-MCP-Agent-Session: definitely-not-${MTLS_SESSION_NAME}" \
    --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aaa-ping","arguments":{}}}' \
    "${MTLS_URL}" 2>/dev/null || true)"
  if [[ -n "${spoof_body}" ]]; then
    echo "${spoof_body}" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
err = doc.get("error") or {}
msg = str(err.get("message", "")).lower()
if doc.get("result") and "pong" in json.dumps(doc.get("result")).lower():
    raise SystemExit("spoofed governance headers must not bypass mtls auth")
if err and any(token in msg for token in ("session", "denied", "unauthorized", "forbidden", "grant", "trust")):
    raise SystemExit(0)
if err:
    raise SystemExit(0)
raise SystemExit(f"unexpected tools/call response with spoofed headers: {doc}")
'
  fi
}
