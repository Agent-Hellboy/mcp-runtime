# Publish an MCP Server

This guide covers the user-facing path for getting an MCP server into MCP Runtime:

1. write an `MCPServer` manifest or `.mcp` metadata
2. build the server image
3. push the image to the platform registry
4. deploy the server into the platform
5. verify that the server is reachable and governed

Use this guide after [Getting started](getting-started.md) once the platform stack is already installed.

## Choose a description format

You can describe a server in two ways:

- `MCPServer` manifest
  Best when you want direct control over the Kubernetes resource the operator will reconcile.
- `.mcp` metadata
  Best when you want a lighter authoring format and a CLI pipeline that generates `MCPServer` manifests for you.

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
  image: registry.example.com/payments
  imageTag: v1.0.0
  port: 8088
  publicPathPrefix: payments
  gateway:
    enabled: true
  analytics:
    enabled: true
```

### What each field does

- `metadata.name`
  The server name inside the platform. This is also the default public route prefix when you do not override it.
- `metadata.namespace`
  Usually `mcp-servers`.
- `spec.image`
  The image repository to run.
- `spec.imageTag`
  The image tag when the tag is not embedded directly in `spec.image`.
- `spec.port`
  The port your MCP process listens on inside the container.
- `spec.publicPathPrefix`
  The public route prefix. `payments` becomes `/payments/mcp`.
- `spec.gateway.enabled`
  Sends requests through the broker path so policy and session checks run before tool calls.
- `spec.analytics.enabled`
  Emits governed request data into the Sentinel stack.

### Common edits

- Add `spec.ingressHost` for host-based routing instead of path-based routing.
- Add `spec.servicePort` when you want a Service port other than `80`.
- Add `spec.envVars` or `spec.secretEnvVars` for runtime configuration.
- Add `spec.imagePullSecrets` if your registry requires explicit pull credentials.
- Add `spec.tools`, `spec.auth`, `spec.policy`, `spec.session`, or `spec.rollout` when you want stricter governance or more delivery control.

Apply the manifest:

```bash
./bin/mcp-runtime server apply --file payments.yaml
./bin/mcp-runtime server status
```

## Option B: write `.mcp` metadata

The metadata-driven pipeline uses YAML files under `.mcp/` and generates `MCPServer` manifests for you.

Example:

```yaml
version: v1
servers:
  - name: payments
    image: registry.example.com/payments
    imageTag: v1.0.0
    route: /payments
    port: 8088
    replicas: 1
    namespace: mcp-servers
```

### Metadata fields

- `name`
  The server name.
- `image`
  The image repository.
- `imageTag`
  The image tag.
- `route`
  The public path prefix that will become the server ingress path.
- `port`
  The container port.
- `replicas`
  The desired replica count.
- `namespace`
  The target namespace.

### Metadata defaults

If fields are omitted, the loader applies defaults:

- image defaults toward the platform registry path
- tag defaults to `latest`
- route is normalized with a leading `/`
- port defaults to `8088`
- replicas default to `1`
- namespace defaults to `mcp-servers`

Generate and deploy manifests:

```bash
./bin/mcp-runtime pipeline generate --dir .mcp --output manifests/
./bin/mcp-runtime pipeline deploy --dir manifests/
```

## Build and push the server image

For the direct manifest path, build the image with your normal container workflow:

```bash
docker build -t payments:v1.0.0 .
```

For the metadata-driven path, use the CLI helper:

```bash
./bin/mcp-runtime server build image payments --tag v1.0.0
```

This command builds a Docker image and updates the matching metadata entry. It is meant for the `.mcp` pipeline flow.

Then push the image:

```bash
./bin/mcp-runtime registry push --image payments:v1.0.0
```

Typical user flow:

```bash
docker build -t payments:v1.0.0 .
./bin/mcp-runtime registry push --image payments:v1.0.0
./bin/mcp-runtime server apply --file payments.yaml
```

Or, with metadata:

```bash
./bin/mcp-runtime server build image payments --tag v1.0.0
./bin/mcp-runtime registry push --image payments:v1.0.0
./bin/mcp-runtime pipeline generate --dir .mcp --output manifests/
./bin/mcp-runtime pipeline deploy --dir manifests/
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
./bin/mcp-runtime server logs payments --follow
./bin/mcp-runtime sentinel logs gateway --follow
```

## Common failure points

### Image built, but deploy still points at the wrong image

Check:

- the `spec.image` and `spec.imageTag` in your manifest
- the metadata entry updated by `server build image`
- whether you pushed the same tag you applied

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

### Analytics enabled, but no request history appears

Check:

- `./bin/mcp-runtime sentinel status`
- `./bin/mcp-runtime sentinel logs ingest --follow`
- `./bin/mcp-runtime sentinel logs processor --follow`

## Related docs

- [Getting Started](getting-started.md)
- [CLI](cli.md)
- [Runtime](runtime.md)
- [API](api.md)
- [Sentinel](sentinel.md)
