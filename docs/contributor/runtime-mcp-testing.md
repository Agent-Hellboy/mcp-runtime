# Runtime MCP Testing

Use this page to deploy MCP servers, verify catalog visibility, inspect
gateway policy, and clean up stale runtime objects in a contributor cluster.

## Catalog Model

`mcp-servers` is the legacy single-team/example namespace used by the local
testing manifests below. In default `tenant` platform mode, signed-in users see
only team namespaces they belong to. `--platform-mode org` uses
`mcp-servers-org` as the shared authenticated catalog, and
`--platform-mode public` uses `mcp-servers-public` as the anonymous preview
catalog. Team-specific MCPs belong in namespaces such as `mcp-team-tenant-a`.

Expected UI/API behavior:

| Principal | Expected catalog |
|---|---|
| Anonymous | `401` for `/api/runtime/servers`, except public mode can read `mcp-servers-public` |
| Normal user in tenant mode | MCPs from team namespaces they belong to |
| Tenant user in tenant mode | MCPs from their own team namespace |
| User in org mode | MCPs from `mcp-servers-org` |
| User in public mode | MCPs from `mcp-servers-public` |
| Admin | Cluster-wide management visibility, with namespace/team checks on writes |

## Deploy the Bundled Example

The bundled Go example is useful for disposable local testing. Keep it separate
from long-lived shared-cluster MCPs.

Create metadata:

```bash
cat > /tmp/go-example-mcp.yaml <<'EOF'
version: v1
servers:
  - name: go-example-mcp
    description: Go MCP example server with smoke and text transformation tools.
    route: /go-example-mcp/mcp
    publicPathPrefix: go-example-mcp
    port: 8088
    namespace: mcp-servers
    envVars:
      - name: MCP_PATH
        value: /go-example-mcp/mcp
    tools:
      - name: add
        description: Add two numbers.
        requiredTrust: low
        sideEffect: read
      - name: upper
        description: Uppercase the provided message.
        requiredTrust: medium
        sideEffect: read
    auth:
      mode: header
      humanIDHeader: X-MCP-Human-ID
      agentIDHeader: X-MCP-Agent-ID
      sessionIDHeader: X-MCP-Agent-Session
    policy:
      mode: allow-list
      defaultDecision: deny
      policyVersion: v1
    session:
      required: true
    gateway:
      enabled: true
    analytics:
      ingestURL: http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events
      apiKeySecretRef:
        name: go-example-mcp-analytics
        key: api-key
EOF
```

Create the analytics Secret:

```bash
API_KEY="$(
  kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
    -o jsonpath='{.data.INGEST_API_KEYS}' | base64 -d | cut -d, -f1
)"

kubectl create secret generic go-example-mcp-analytics \
  -n mcp-servers \
  --from-literal=api-key="$API_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Build, push, generate, and deploy:

```bash
./bin/mcp-runtime server build image go-example-mcp \
  --metadata-file /tmp/go-example-mcp.yaml \
  --dockerfile examples/go-mcp-server/Dockerfile \
  --context examples/go-mcp-server \
  --registry registry.registry.svc.cluster.local:5000 \
  --tag dev

./bin/mcp-runtime auth login --api-url http://localhost:18080

./bin/mcp-runtime registry push \
  --image registry.registry.svc.cluster.local:5000/go-example-mcp:dev

rm -rf /tmp/go-example-mcp-manifests
./bin/mcp-runtime pipeline generate \
  --file /tmp/go-example-mcp.yaml \
  --output /tmp/go-example-mcp-manifests

./bin/mcp-runtime pipeline deploy --dir /tmp/go-example-mcp-manifests
kubectl rollout status deploy/go-example-mcp -n mcp-servers --timeout=180s
```

## Inspect Runtime Outputs

```bash
SERVER=go-example-mcp
NAMESPACE=mcp-servers

kubectl get mcpserver "$SERVER" -n "$NAMESPACE" -o yaml
kubectl get deploy/"$SERVER" svc/"$SERVER" ingress/"$SERVER" -n "$NAMESPACE" -o wide
kubectl get cm -n "$NAMESPACE" "${SERVER}-gateway-policy" -o yaml
./bin/mcp-runtime server policy inspect "$SERVER" --namespace "$NAMESPACE"
```

The MCP app container is usually distroless. Use logs and `kubectl describe`
before trying to exec a shell:

```bash
POD="$(kubectl get pods -n "$NAMESPACE" -l app="$SERVER" -o jsonpath='{.items[0].metadata.name}')"
kubectl describe pod -n "$NAMESPACE" "$POD"
kubectl logs -n "$NAMESPACE" "$POD" -c "$SERVER"
kubectl logs -n "$NAMESPACE" "$POD" -c mcp-gateway
```

## Grants and Sessions

Gateway policy requires both an access grant and an agent session when the
server has `spec.session.required=true`.

Use the CLI apply commands:

```bash
./bin/mcp-runtime access grant apply --file /tmp/grant.yaml --use-kube
./bin/mcp-runtime access session apply --file /tmp/session.yaml --use-kube
```

Then verify materialization:

```bash
kubectl get mcpaccessgrant,mcpagentsession -n "$NAMESPACE" -o wide
./bin/mcp-runtime server policy inspect "$SERVER" --namespace "$NAMESPACE"
```

If the CRDs exist but the policy file does not include them, check:

```bash
kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --since=10m
kubectl get cm -n "$NAMESPACE" "${SERVER}-gateway-policy" \
  -o 'go-template={{index .data "policy.json"}}'
```

## Tenant MCPs

For team-specific servers, use one namespace per tenant or team and set
`spec.teamID` to the immutable platform team ID. The platform API defaults and
validates this for team namespace writes; direct `kubectl apply` depends on the
manifest and Kubernetes RBAC.

Cross-team delegation is modeled as an access resource in the server owner's
namespace with an explicit foreign `subject.teamID`. For example, Tenant A can
create a grant and session in `mcp-team-tenant-a` that point at
`tenant-a-mcp`, while setting `subject.teamID` to Tenant B's team ID. The
request must then carry Tenant B's `X-MCP-Team-ID` plus the matching human,
agent, and session headers. Reusing the same session with Tenant A's team ID
should fail with `session_not_found` or `no_matching_grant`.

Inventory command:

```bash
kubectl get mcpservers -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,TEAM:.spec.teamID,PATH:.spec.ingressPath,READY:.status.deploymentReady,GW:.status.gatewayReady'
```

Tenant visibility should be checked through the UI/API, not only `kubectl`,
because the UI/API path enforces principal namespaces.

## Remove a Stale Test MCP

Remove access resources first, then the server and single-purpose Secret:

```bash
kubectl delete mcpagentsession <session-name> -n <namespace> --ignore-not-found
kubectl delete mcpaccessgrant <grant-name> -n <namespace> --ignore-not-found
kubectl delete mcpserver <server-name> -n <namespace> --ignore-not-found
kubectl delete secret <server-name>-analytics -n <namespace> --ignore-not-found
```

Confirm the catalog through the UI/API after cleanup:

```bash
curl -sS -b /tmp/mcp-test-user-cookie.txt \
  http://localhost:18080/api/runtime/servers |
  jq '{count: (.servers|length), names: [.servers[] | (.namespace + "/" + .name)]}'
```
