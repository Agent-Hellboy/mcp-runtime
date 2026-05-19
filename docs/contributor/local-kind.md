# Local Kind and Test Mode

Use this flow when you need a full local platform: API, UI, operator, registry,
Traefik, Sentinel services, and real MCP ingress routes.

## Prerequisites

Install Docker, Kind, `kubectl`, Go, `curl`, `jq`, and Python 3. Then build the
CLI:

```bash
make deps
make build
```

## Create the Kind Cluster

The documented test-mode install emits pod images that use
`registry.registry.svc.cluster.local:5000/...`. The Kind node needs a matching
containerd mirror before setup runs.

```bash
cat > /tmp/mcp-runtime-kind.yaml <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.registry.svc.cluster.local:5000"]
      endpoint = ["http://127.0.0.1:32000"]
EOF

kind create cluster --name mcp-runtime --config /tmp/mcp-runtime-kind.yaml
kubectl config use-context kind-mcp-runtime
```

## Install MCP Runtime

Run preflight checks, then install with the HTTP ingress overlay:

```bash
./bin/mcp-runtime bootstrap

MCP_SETUP_WAIT_TIMEOUT=900 \
  ./bin/mcp-runtime setup --test-mode \
  --ingress-manifest config/ingress/overlays/http
```

Check the platform:

```bash
./bin/mcp-runtime status
./bin/mcp-runtime cluster status
./bin/mcp-runtime registry status
./bin/mcp-runtime sentinel status
./bin/mcp-runtime cluster doctor
```

Expose the dashboard and MCP routes:

```bash
kubectl port-forward -n traefik svc/traefik 18080:8000
```

Local URLs:

| Surface | URL |
|---|---|
| Platform UI | `http://localhost:18080/` |
| Platform API | `http://localhost:18080/api` |
| MCP route shape | `http://localhost:18080/<server-name>/mcp` |

Keep the Traefik port-forward running while using the browser or `curl`.

## Seeded Logins

`setup --test-mode` seeds local-only platform logins:

| Role | Email | Password |
|---|---|---|
| User | `test@mcpruntime.org` | `test@123` |
| Admin | `admin@mcpruntime.org` | `admin@123` |

These are controlled by `PLATFORM_DEV_*` keys in the `mcp-sentinel-secrets`
Secret. They are for local debugging only.

The shared contributor cluster used for tenant-isolation smoke testing also has
these local-only tenant accounts:

| Tenant | Email | Password |
|---|---|---|
| Tenant A | `tenant-a-20260510232145@mcpruntime.org` | `TenantA-20260510232145!` |
| Tenant B | `tenant-b-20260510232145@mcpruntime.org` | `TenantB-20260510232145!` |

Fresh local clusters only have the `test` and `admin` accounts unless you create
tenant teams and users yourself.

## Catalog Visibility Checks

Anonymous users must not see the MCP catalog:

```bash
curl -i http://localhost:18080/api/runtime/servers
```

Expected status: `401 Unauthorized`.

Check the default test user:

```bash
rm -f /tmp/mcp-test-user-cookie.txt
curl -sS -c /tmp/mcp-test-user-cookie.txt \
  -H 'content-type: application/json' \
  -d '{"email":"test@mcpruntime.org","password":"test@123"}' \
  http://localhost:18080/auth/login

curl -sS -b /tmp/mcp-test-user-cookie.txt \
  http://localhost:18080/api/runtime/servers |
  jq '{count: (.servers|length), names: [.servers[] | (.namespace + "/" + .name)]}'
```

In default tenant mode, signed-in users see MCPs from team namespaces they
belong to only. A setup installed with `--platform-mode org` instead shows the
shared org catalog from `mcp-servers-org`, and `--platform-mode public` shows
the public preview catalog from `mcp-servers-public`.

Check tenant isolation in the shared contributor cluster:

```bash
rm -f /tmp/mcp-tenant-a-cookie.txt
curl -sS -c /tmp/mcp-tenant-a-cookie.txt \
  -H 'content-type: application/json' \
  -d '{"email":"tenant-a-20260510232145@mcpruntime.org","password":"TenantA-20260510232145!"}' \
  http://localhost:18080/auth/login

curl -sS -b /tmp/mcp-tenant-a-cookie.txt \
  http://localhost:18080/api/runtime/servers |
  jq '{count: (.servers|length), names: [.servers[] | (.namespace + "/" + .name)]}'

curl -sS -o /tmp/mcp-tenant-a-cross.txt -w '%{http_code}\n' \
  -b /tmp/mcp-tenant-a-cookie.txt \
  'http://localhost:18080/api/runtime/servers?namespace=mcp-team-tenant-b'
```

Tenant A should see `mcp-team-tenant-a`, and the explicit Tenant B namespace
read should return `403`.

## Quick Cluster Inventory

```bash
kubectl get pods -n mcp-runtime -o wide
kubectl get pods -n mcp-sentinel -o wide
kubectl get mcpservers -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,TEAM:.spec.teamID,PATH:.spec.ingressPath,READY:.status.deploymentReady,GW:.status.gatewayReady'
kubectl get mcpaccessgrant,mcpagentsession -A -o wide
kubectl get ingress -A
```

If you remove a stale test server, remove its matching grants, sessions, and
single-purpose analytics Secret too:

```bash
kubectl delete mcpagentsession <session-name> -n <namespace> --ignore-not-found
kubectl delete mcpaccessgrant <grant-name> -n <namespace> --ignore-not-found
kubectl delete mcpserver <server-name> -n <namespace> --ignore-not-found
kubectl delete secret <server-name>-analytics -n <namespace> --ignore-not-found
```
