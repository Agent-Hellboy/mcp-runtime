# Publish an MCP Server

This guide covers the user-facing path for getting an MCP server into MCP Runtime:

1. write an `MCPServer` manifest or `.mcp` metadata
2. build the server image
3. push the image to the platform registry
4. deploy the server into the platform
5. verify that the server is reachable and governed

Use this guide after [Getting started](getting-started.md) once the platform stack is already installed.

## Choose an authoring format

You can describe a server in two ways:

- `MCPServer` manifest
  Best when you want direct control over the Kubernetes resource the operator will reconcile.
- `.mcp` metadata
  Best when you want a lighter authoring format and `server generate` / `server deploy` from `.mcp` metadata.

The platform outcome is the same in both cases: the operator reconciles a server deployment, service, route, and optional governed request path.

## Option A: write an `MCPServer` manifest

Start with a minimal manifest:

```yaml
apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: payments
  namespace: mcp-servers
spec:
  description: Payments MCP server for invoice lookup and refund workflows.
  image: registry.example.com/payments
  imageTag: v1.0.0
  port: 8088
  publicPathPrefix: payments
  gateway:
    enabled: true
```

### What each field does

- `metadata.name`
  The server name inside the platform. This is also the default public route prefix when you do not override it.
- `metadata.namespace`
  Usually `mcp-servers` for a single-team setup. In a multi-team deployment,
  use the team's namespace, for example `mcp-team-acme`; see
  [Multi-team isolation](multi-team.md).
- `spec.teamID`
  Stable platform team ID for the server owner. The platform API defaults this
  for team namespaces; hand-written YAML should set it explicitly.
- `spec.description`
  A short platform-facing summary shown in the server catalog.
- `spec.image`
  The image repository to run.
- `spec.imageTag`
  The image tag when the tag is not embedded directly in `spec.image`.
- `spec.port`
  The port your MCP process listens on inside the container.
- `spec.publicPathPrefix`
  The public route prefix. `payments` becomes `/payments/mcp`.
- `spec.gateway.enabled`
  Sends requests through `mcp-gateway` so policy, session checks, and request analytics run before tool calls.
- `spec.analytics`
  Analytics emission to the Sentinel stack is on by default whenever the
  operator has an ingest URL configured (via `MCP_SENTINEL_INGEST_URL` or
  `spec.analytics.ingestURL`). Set `spec.analytics.disabled: true` to opt
  this server out. Platform API deploys through `mcp-runtime server deploy`
  create a namespace-local ingest-key Secret and set
  `spec.analytics.apiKeySecretRef` automatically when gateway analytics are
  enabled.

`mcp-runtime server deploy` and the platform API (`POST /api/runtime/servers`)
publish governed servers by default: when the request does not provide
`spec.gateway`, the platform writes `spec.gateway.enabled: true`. Hand-written
YAML must set that field explicitly if you want request analytics and governed
tool calls.

### Common edits

- Add `spec.ingressHost` for host-based routing instead of path-based routing.
- Add `spec.servicePort` when you want a Service port other than `80`.
- Add `spec.envVars` or `spec.secretEnvVars` for runtime configuration.
- Add `spec.imagePullSecrets` if your registry requires explicit pull credentials.
- Add `spec.tools` with tool descriptions, trust levels, and side-effect classes so the platform catalog and policy engine mirror the tool summaries clients see from `tools/list`.
- Add `spec.auth`, `spec.policy`, `spec.session`, or `spec.rollout` when you want stricter governance or more delivery control.

Apply the manifest only from an admin/operator workstation. `server apply`
requires `--use-kube`, kubectl, and kubeconfig/RBAC access to the target
namespace. For normal platform workflows, use the platform-backed
`server deploy` flow in Option B instead.

```bash
./bin/mcp-runtime server apply --file payments.yaml --use-kube
./bin/mcp-runtime server status --use-kube   # pod detail; omit --use-kube for platform API summary
```

## Option B: initialize or write `.mcp` metadata

The metadata-driven server flow uses YAML files under `.mcp`. `server deploy`
publishes directly from that metadata, while `server generate` renders
`MCPServer` manifests when you need YAML for review or GitOps.

Start with `server init` when you do not already have metadata:

```bash
./bin/mcp-runtime server init payments --scope org --tool list_invoices --tool refund_invoice
./bin/mcp-runtime server init payments \
  --scope org \
  --tool list_invoices \
  --tool-spec refund_invoice:high:destructive
```

This creates `.mcp/servers.yaml` with sane defaults. Re-run with `--force` to
replace the same server entry, or edit the generated file when tools need
different trust levels or side-effect values. `--tool` is shorthand for a
read-only, low-trust tool. Use `--tool-spec name:low|medium|high:read|write|destructive`
for per-tool metadata.

For grant manifests, `access grant init --tool` is shorthand for
`name:allow:<--trust>`. Use repeated `--tool-rule name:allow|deny:low|medium|high`
when a grant needs mixed per-tool decisions or trust levels. For explicit
session manifests, `access session init` supports `--trust`,
`--expires-in`, `--expires-at`, `--revoked`, and upstream-token secret flags.

```bash
mcp-runtime auth login --api-url https://platform.example.com

./bin/mcp-runtime access grant init payments-globex-cursor \
  --namespace mcp-team-acme \
  --server payments \
  --team-id <globex-team-id> \
  --agent-id cursor \
  --tool list_invoices \
  --tool-rule refund_invoice:allow:high \
  --side-effect read \
  --output grant.yaml

./bin/mcp-runtime access session init cursor-session \
  --namespace mcp-team-acme \
  --server payments \
  --human-id <user-id> \
  --agent-id cursor \
  --trust medium \
  --expires-in 1h \
  --output session.yaml

./bin/mcp-runtime access grant apply --file grant.yaml
./bin/mcp-runtime access session apply --file session.yaml
```

Adapter-driven agents usually skip manual session apply; use
`mcp-runtime adapter stdio --server payments --agent cursor --auto-refresh`
after the grant exists. See [Agent Adapters](agent-adapters.md).

Example metadata:

```yaml
version: v1
servers:
  - name: payments
    description: Payments MCP server for invoice lookup and refund workflows.
    scope: org
    image: registry.example.com/org/payments
    imageTag: v1.0.0
    route: /payments
    port: 8088
    replicas: 1
    auth:
      mode: header
    policy:
      mode: allow-list
      defaultDecision: deny
      enforceOn: call_tool
      policyVersion: v1
    session:
      required: true
    gateway:
      enabled: true
    tools:
      - name: list_invoices
        description: List invoices for a customer account.
        requiredTrust: low
        sideEffect: read
      - name: refund_invoice
        description: Issue a refund for an invoice.
        requiredTrust: high
        sideEffect: destructive
```

### Metadata fields

- `name`
  The server name.
- `description`
  A short platform-facing summary shown in the server catalog.
- `image`
  The image repository.
- `imageTag`
  The image tag.
- `scope`
  Optional publish destination: `tenant`, `org`, or `public`. `org` resolves to
  the org catalog namespace, and `public` resolves to the public catalog
  namespace. `tenant` selects one of the authenticated user's team namespaces
  when you publish through the platform API; when generating Kubernetes YAML
  directly, set `namespace` explicitly for team deployments.
- `route`
  The public path prefix that will become the server ingress path.
- `port`
  The container port.
- `replicas`
  The desired replica count.
- `namespace`
  The target namespace.
- `tools`
  Tool inventory for the platform catalog and policy authoring. Include each tool's description when the MCP server SDK exposes one through `tools/list`, and set `sideEffect` to `read`, `write`, or `destructive`. Tool side effects are required when a tool is listed.
- `auth`, `policy`, `session`, and `gateway`
  Governed request-path settings. `server init` writes header auth, allow-list/deny policy, required adapter-issued sessions, and `gateway.enabled: true` so public tool calls go through the adapter/session path by default.

### Metadata defaults

If fields are omitted, the loader applies defaults:

- image defaults toward the platform registry path
- `scope: org` / `scope: public` prefix default image repositories with
  `org/` or `public/` and default the generated namespace to
  `mcp-servers-org` or `mcp-servers-public`
- tag defaults to `latest`
- route is normalized with a leading `/`
- port defaults to `8088`
- replicas default to `1`
- namespace defaults to `mcp-servers`
- `server init` writes explicit governed defaults; hand-authored metadata may
  omit them only when you intentionally want the platform/operator defaults

For multi-team deployments, set `scope: tenant` and deploy through
`server deploy --scope tenant` with platform credentials. The platform API
resolves the target team namespace and defaults team ownership metadata. The
namespace is the write boundary for the `MCPServer`, grants, sessions, and
secrets. `server deploy` creates by default; if a server with the same name
already exists, pass `--update` to redeploy it intentionally.

Deploy from metadata:

```bash
./bin/mcp-runtime server deploy payments --scope org --metadata-dir .mcp

# Redeploy an existing server after changing metadata or image tag.
./bin/mcp-runtime server deploy payments --scope org --metadata-dir .mcp --update

# Optional: render YAML for review/GitOps.
./bin/mcp-runtime server generate --metadata-dir .mcp --output manifests/
```

## Build and push the server image

MCP Runtime supports two practical image flows. Keep these flows separate so tags stay consistent.

### Flow A — metadata-driven build with the CLI

```bash
./bin/mcp-runtime server build image payments --tag v1.0.0 --platform linux/amd64
```

`server build image` builds the image, resolves the target registry host, tags the local image with that resolved reference, and rewrites matching `.mcp` metadata (`image` and `imageTag`). The command defaults Docker builds to `linux/amd64`, matching common amd64 Kubernetes nodes; set `--platform` or `MCP_DOCKER_PLATFORM` when your target nodes use another architecture. Registry resolution prefers explicit registry env, then the cluster's `registry/registry` Ingress host, before falling back to the registry Service address. When metadata sets `scope: tenant`, the build command uses platform credentials to resolve the same team repository prefix that `registry push --scope tenant` uses, so log in first or set `MCP_PLATFORM_API_TOKEN` plus `MCP_PLATFORM_API_URL`.

After this command, push the exact image reference produced by the build output (or read it from the rewritten metadata):

```bash
mcp-runtime auth login --api-url https://platform.example.com
./bin/mcp-runtime registry push --scope org --image <exact-image-ref-from-build>
```

`registry push` requires platform credentials from `mcp-runtime auth login` or
`MCP_PLATFORM_API_TOKEN` plus `MCP_PLATFORM_API_URL`; unauthenticated pushes are
rejected before Docker or the in-cluster helper starts. `<exact-image-ref-from-build>`
may be a resolved public registry host such as `registry.example.com/org/payments:v1.0.0`,
or a registry Service address when no public registry Ingress is configured.
Use `--scope public` for public catalog images. Use `--scope tenant` for team
images; if the image name has no repository prefix, the CLI prefixes it with
the authenticated user's active team slug. Explicit repository prefixes for
tenant images must match one of the user's teams.

Then deploy from metadata:

```bash
./bin/mcp-runtime server deploy payments --scope org --metadata-dir .mcp
```

### Flow B — manual Docker build, push, and direct platform deploy

Use this when you manage image tags directly and want the platform API to write
the `MCPServer` for you:

```bash
docker build -t payments:v1.0.0 .
mcp-runtime auth login --api-url https://platform.example.com
./bin/mcp-runtime registry push --scope public --image payments:v1.0.0
./bin/mcp-runtime server deploy payments --scope public --image payments --tag v1.0.0
```

Short names like `payments:v1.0.0` are valid for `registry push` when that
exact local image tag exists. For `server deploy`, the platform API accepts
short names such as `payments` and resolves them to the configured registry and
scope prefix, for example `<registry>/public/payments` in public mode or
`<registry>/<team-slug>/payments` in tenant mode.
`server deploy --scope public` resolves the platform public catalog namespace;
`--scope org` resolves the org catalog namespace; `--scope tenant` uses the
authenticated user's team namespace unless `--team` or `--namespace` selects one
explicitly. `server deploy` uses the default public route `/<name>/mcp` and
passes that same value as `MCP_PATH` so the bundled Go, Python, and Rust
examples listen on the route the ingress exposes. The platform API and CLI
deploy flow also default `spec.gateway.enabled: true`, so published servers use
the governed gateway path unless you explicitly provide `spec.gateway`. When
you run `server deploy` from a directory with `.mcp/*.yaml`, the CLI copies the
matching server metadata into the request. If the metadata directory contains
exactly one server, it uses that server's inventory even when the deployed
runtime name is different; this keeps `spec.tools` side-effect metadata in sync
with governance policy.

### Flow C — manual Docker build, push, and manifest apply

Use this when you need full control of `MCPServer` fields and you have
admin/operator Kubernetes access:

```bash
docker build -t payments:v1.0.0 .
mcp-runtime auth login --api-url https://platform.example.com
./bin/mcp-runtime registry push --scope tenant --image payments:v1.0.0
./bin/mcp-runtime server apply --file payments.yaml --use-kube
```

## What happens after deploy

After the server description reaches the platform, the operator does the following:

1. stores the `MCPServer` resource in Kubernetes
2. resolves the final image reference
3. creates or updates a `Deployment`
4. creates or updates a `Service`
5. creates or updates an `Ingress`
6. renders gateway policy when governed access is enabled
7. updates `MCPServer.status` with readiness and progress

With the default path-based shape, the server becomes available at:

```text
/{publicPathPrefix}/mcp
```

For the example above, that is:

```text
/payments/mcp
```

## Verify from the CLI

Check server state:

```bash
mcp-runtime auth login --api-url https://platform.example.com
./bin/mcp-runtime server status
./bin/mcp-runtime server get payments
./bin/mcp-runtime status
```

If the server uses governed access:

```bash
./bin/mcp-runtime server policy inspect payments
./bin/mcp-runtime sentinel status
```

If traffic is failing:

```bash
./bin/mcp-runtime sentinel logs gateway --follow

# Admin/operator only — MCP server pod logs require --use-kube
./bin/mcp-runtime server logs payments --follow --use-kube
```

## Common failure points

### Image built, but deploy still points at the wrong image

Check:

- the `spec.image` and `spec.imageTag` in your manifest
- the metadata entry updated by `server build image`
- whether you pushed the exact same image reference (registry/repo/tag) that your metadata or manifest points to

### Image pushed, but server never becomes ready

Check:

- `./bin/mcp-runtime server get <name>`
- `./bin/mcp-runtime server status`
- `./bin/mcp-runtime status`

Most often this is an image reference, image-pull, or routing mismatch.

### Route exists, but governed calls fail

Check:

- `./bin/mcp-runtime server policy inspect <name>`
- your grant and session objects
- `./bin/mcp-runtime sentinel logs gateway --follow`

### Event count is 0

Check:

- `kubectl get mcpserver <name> -n <namespace> -o yaml`
- `GatewayReady=True` and `PolicyReady=True` on the MCPServer status
- the server pod is `2/2` and includes the `mcp-gateway` sidecar
- `kubectl logs -n <namespace> <pod> -c mcp-gateway`
- `./bin/mcp-runtime sentinel status`
- `./bin/mcp-runtime sentinel logs ingest --follow`
- `./bin/mcp-runtime sentinel logs processor --follow`

Request analytics only exist for traffic that flows through `mcp-gateway`.
The adapter is not required for analytics; it only helps clients that cannot
attach identity or session headers directly, or that want platform-issued
sessions. Hand-written YAML must include `spec.gateway.enabled: true` for
request analytics. If you apply raw YAML with `kubectl apply` or
`server apply --use-kube` instead of `server deploy`, also create a
namespace-local ingest-key Secret and set `spec.analytics.apiKeySecretRef`;
otherwise the gateway can reach ingest but events will be rejected with 401.
Analytics is on by default for gateway traffic when the operator has
`MCP_SENTINEL_INGEST_URL`; opt out with:

```yaml
analytics:
  disabled: true
```

## Related docs

- [Getting Started](getting-started.md)
- [CLI](cli.md)
- [Runtime](runtime.md)
- [API](api.md)
- [Sentinel](sentinel.md)
