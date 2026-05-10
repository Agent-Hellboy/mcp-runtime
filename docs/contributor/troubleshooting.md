# Contributor Troubleshooting

Start from the symptom and inspect the narrowest surface first.

## Anonymous Users Can See MCPs

Expected behavior:

```bash
curl -i http://localhost:18080/api/runtime/servers
```

The response should be `401 Unauthorized`.

If it returns servers:

1. Confirm the UI and API Deployments are on the current images.

   ```bash
   kubectl get deploy mcp-sentinel-api mcp-sentinel-ui -n mcp-sentinel \
     -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.spec.template.spec.containers[0].image}{"\n"}{end}'
   ```

2. Roll both Deployments after patching auth or catalog code.

   ```bash
   kubectl rollout status deployment/mcp-sentinel-api -n mcp-sentinel --timeout=90s
   kubectl rollout status deployment/mcp-sentinel-ui -n mcp-sentinel --timeout=90s
   ```

3. Sign out in the browser or use a fresh private window. A valid UI session
   cookie makes the same URL authenticated.

## Tenant User Sees the Wrong MCPs

Check the API path first:

```bash
curl -sS -b /tmp/mcp-tenant-a-cookie.txt \
  http://localhost:18080/api/runtime/servers |
  jq '[.servers[] | {namespace,name,team_id}]'
```

Then compare Kubernetes state:

```bash
kubectl get mcpservers -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,TEAM:.spec.teamID'
```

Rules to verify:

- Org-wide MCPs live in `mcp-servers` with empty `spec.teamID`.
- Tenant MCPs live in `mcp-team-<slug>` with non-empty `spec.teamID`.
- Non-admin users should receive `403` when explicitly requesting another
  tenant namespace.
- Direct `kubectl get mcpservers -A` is not an authz check; it shows what your
  kubeconfig can read.

## `tools[].sideEffect` Is Required

Every listed tool must declare a side effect:

```yaml
tools:
  - name: add
    requiredTrust: low
    sideEffect: read
```

If the operator logs validation errors such as
`spec.tools[0].sideEffect: Required value`, check for CRD/operator skew and old
test objects:

```bash
kubectl apply -f config/crd/bases/mcpruntime.org_mcpservers.yaml
kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --since=10m
kubectl get mcpservers -A -o yaml | rg -n "name:|sideEffect"
```

Patch old local test objects or redeploy them from current metadata.

## Image Pulls Fail

For Kind test mode, pod images should use:

```text
registry.registry.svc.cluster.local:5000/<image>:<tag>
```

The Kind cluster must have a containerd mirror for that exact host:

```toml
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
  endpoint = ["http://127.0.0.1:32000"]
```

Useful checks:

```bash
kubectl describe pod -n <namespace> <pod>
kubectl get events -n <namespace> --sort-by=.lastTimestamp
./bin/mcp-runtime cluster doctor
```

If events say `http: server gave HTTP response to HTTPS client`, the node tried
HTTPS against the plain HTTP dev registry. Recreate the Kind cluster with the
mirror or configure the node runtime for the exact image host.

## Host Cannot Push to Cluster DNS Registry

This is expected:

```bash
docker push registry.registry.svc.cluster.local:5000/example:dev
```

The host usually cannot resolve Kubernetes service DNS. Use the CLI helper:

```bash
./bin/mcp-runtime registry push \
  --image example:dev \
  --name example \
  --registry registry.registry.svc.cluster.local:5000 \
  --namespace registry
```

For one-node Kind debug loops, `kind load docker-image` is also valid when the
Deployment image name matches the loaded tag.

## Dashboard or API Returns 404

Check the Traefik port-forward and ingress resources:

```bash
lsof -nP -iTCP:18080 -sTCP:LISTEN
kubectl get ingress -A
kubectl logs -n traefik deploy/traefik --tail=120
```

The local dashboard should be reachable at:

```text
http://localhost:18080/
```

## Browser Login Fails but Direct API Works

The HTTP ingress overlay can include the `pii-redactor@file` Traefik middleware.
That middleware is useful for request-path testing, but it can redact email-like
values in some local API calls. For normal UI login, use the seeded accounts.
For identity-store debugging, port-forward the API directly:

```bash
kubectl port-forward -n mcp-sentinel svc/mcp-sentinel-api 18081:8080
```

Then use `http://localhost:18081` for direct API debugging and stop the
port-forward afterward.

## Operator Is Reconciling but Routes Do Not Work

Check each layer in order:

```bash
kubectl get mcpserver <server-name> -n <namespace> -o yaml
kubectl get deploy,svc,ingress -n <namespace> | rg '<server-name>|NAME'
kubectl describe pod -n <namespace> -l app=<server-name>
kubectl logs -n <namespace> deploy/<server-name> -c mcp-gateway --tail=120
kubectl logs -n traefik deploy/traefik --tail=120
```

In local Kind, `MCPServer.status.phase` can stay `PartiallyReady` even when the
Deployment is ready and traffic works, because strict ingress readiness waits
for load balancer status. Use the Deployment, Service, Ingress, and actual
traffic checks to decide whether local routing works.

