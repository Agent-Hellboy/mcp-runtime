# mcp-auth TypeScript SDK resource server

This deployment wraps the [`mcp-auth` TypeScript MCP example](https://github.com/Agent-Hellboy/mcp-auth/tree/main/examples/typescript-mcp)
with a production image and an MCP Runtime `MCPServer` resource. The existing
cluster also has Go and Python SDK examples; this TypeScript endpoint is added
only when no TypeScript SDK resource is already deployed.

The sample uses `@mcp-auth/client` to verify the issuer, audience, signature,
expiry, and `tools:read` scope. Its image build needs the selected mcp-auth
checkout because the package is currently linked from the local repository.

## Build and deploy to the public k3s example

Select a clean mcp-auth ref first. Stage only the SDK and example into a small
build context, then build from your workstation with the production node
architecture:

```bash
MCP_AUTH_SOURCE=/path/to/mcp-auth
MCP_AUTH_REF=issue-13-cimd # choose the ref being tested
MCP_AUTH_COMMIT="$(git -C "$MCP_AUTH_SOURCE" rev-parse "$MCP_AUTH_REF^{commit}")"
MCP_AUTH_TAG="verify-ts-$(date -u +%Y%m%dT%H%M%S)-${MCP_AUTH_COMMIT:0:8}"
BUILD_CONTEXT="$(mktemp -d)"

git -C "$MCP_AUTH_SOURCE" archive "$MCP_AUTH_REF" \
  auth-client/typescript examples/typescript-mcp \
  | tar -x -C "$BUILD_CONTEXT"

source config/deployments/mcpruntime-org.env
docker build --platform linux/amd64 \
  --label "org.opencontainers.image.revision=$MCP_AUTH_COMMIT" \
  -f examples/mcp-auth-sdk-typescript/Dockerfile \
  -t "registry.mcpruntime.org/mcp-auth-sdk-typescript:${MCP_AUTH_TAG}" \
  "$BUILD_CONTEXT"
```

Authenticate to `registry.mcpruntime.org` with the deployment's existing
registry credential and add the resource URL to `MCP_AUTH_RESOURCES` on
`mcp-auth-server`:

```text
https://mcp.mcpruntime.org/mcp-auth-sdk-typescript/mcp
```

Publish and deploy through the Runtime CLI so the production QA covers the
same path users run. The API provisions the namespace-local registry pull
Secret and attaches it to `mcp-workload`:

```bash
IMAGE_TAG="$MCP_AUTH_TAG"
IMAGE_REF="registry.mcpruntime.org/mcp-auth-sdk-typescript:${IMAGE_TAG}"
DEPLOY_METADATA="$(mktemp)"
sed "s/__IMAGE_TAG__/${IMAGE_TAG}/g" \
  examples/mcp-auth-sdk-typescript/servers.yaml.tmpl > "$DEPLOY_METADATA"

./bin/mcp-runtime server push --scope public --image "$IMAGE_REF"
./bin/mcp-runtime server deploy mcp-auth-sdk-typescript \
  --scope public --metadata-file "$DEPLOY_METADATA" --update
kubectl get mcpserver mcp-auth-sdk-typescript -n mcp-servers
kubectl get secret mcp-runtime-registry-pull -n mcp-servers
kubectl get serviceaccount mcp-workload -n mcp-servers \
  -o jsonpath='{.imagePullSecrets[*].name}'
rm -f "$DEPLOY_METADATA"
```

The CLI flow uses platform credentials from `mcp-runtime auth login`. Keep the
issuer and resource/audience URLs identical to the metadata template. The
deployment reuses the existing `mcp-auth-server` issuer and TLS certificate;
it does not run setup or request certificates.

For a direct-manifest test, render `mcpserver.yaml.tmpl` by replacing
`__IMAGE_TAG__` with `$MCP_AUTH_TAG`, then apply it with kubectl. Raw manifest
apply bypasses the platform's pull-secret provisioning, so create the secret
in `mcp-servers` first:

```bash
umask 077
PULL_SECRET_DIR="$(mktemp -d)"
trap 'rm -rf "$PULL_SECRET_DIR"; unset REGISTRY_API_KEY' EXIT
REGISTRY_API_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.UI_API_KEY}' | base64 -d)"
REGISTRY_AUTH="$(printf 'platform-service:%s' "$REGISTRY_API_KEY" | base64 | tr -d '\n')"
jq -n --arg password "$REGISTRY_API_KEY" --arg auth "$REGISTRY_AUTH" \
  '{auths:{"registry.mcpruntime.org":{username:"platform-service",password:$password,auth:$auth}}}' \
  > "$PULL_SECRET_DIR/config.json"
kubectl create secret generic mcp-runtime-registry-pull -n mcp-servers \
  --type=kubernetes.io/dockerconfigjson \
  --from-file=.dockerconfigjson="$PULL_SECRET_DIR/config.json" \
  --dry-run=client -o yaml | kubectl apply -f -
```

## Configure a Claude Code project

From the project directory, add the remote HTTP server:

```bash
claude mcp add --scope project --transport http \
  mcp-auth-sdk-typescript \
  https://mcp.mcpruntime.org/mcp-auth-sdk-typescript/mcp
```

Claude Code will use the protected-resource metadata to start OAuth. Sign in
with the configured identity provider, then call `whoami`. The server returns
the verified subject and scopes. Remove the project MCP entry with
`claude mcp remove mcp-auth-sdk-typescript` when the test is over.
