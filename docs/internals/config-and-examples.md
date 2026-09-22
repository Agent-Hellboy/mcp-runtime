# Config, Examples, and Supporting Assets

## config/
- `config/crd/bases/` contains generated CRDs for `MCPServer`, `MCPAccessGrant`, and `MCPAgentSession`. Regenerate them with `make -f Makefile.operator generate manifests` after API type changes.
- `config/default/` is the Kustomize entrypoint for operator deployment assets.
- `config/manager/` holds the controller-manager Deployment and PodDisruptionBudget manifests.
- `config/rbac/` contains service account, role, and binding definitions used by the operator.
- `config/ingress/` provides Traefik ingress controller manifests and overlays, including the HTTP dev overlay used by local and e2e setup.
- `config/registry/` contains the bundled Docker distribution registry manifests plus TLS/hostpath overlays.
- `config/cert-manager/` contains sample issuer/certificate resources for registry and ingress TLS.

## examples/
- `workspace-assistant-mcp/` implements the `workspace-assistant-mcp` sample used by smoke/e2e flows.
- `data-utility-mcp/` implements `data-utility-mcp`, and `text-analysis-mcp/` implements `text-analysis-mcp` for e2e validation.
- `mcpserver-path-based.yaml` is the maintained MCPServer manifest example for path-based ingress.

## Makefiles
- `Makefile` exposes high-level tasks (fmt, lint, test, build) for the CLI binary.
- `Makefile.runtime` bundles runtime-specific build/install tasks.
- `Makefile.operator` builds operator manifests, docker image, and runs controller-gen tools; used by setup/build scripts.

## Dockerfiles
- `Dockerfile.operator` builds the operator image with Go build steps and copies manifests.
- `examples/workspace-assistant-mcp/Dockerfile` builds the workspace assistant sample container.
- `test/e2e/Dockerfile` builds images for end-to-end tests.

## Scripts
- `hack/dev/dev-setup.sh` prepares the developer toolchain, not a platform install: it installs `controller-gen` and `kustomize`, regenerates CRD manifests and DeepCopy code, runs `go fmt`/`go vet`, and can install and start a minikube cluster (`install`, `generate`, `format`, `validate`, `minikube`, or `all`). Use `mcp-runtime bootstrap`/`setup` or `test/e2e/kind.sh` for a Kind cluster.
- `test/e2e/kind.sh` creates a kind cluster, builds/pushes test images through the registry flow, and runs e2e validation; `test/e2e/run-in-docker.sh` runs e2e flows inside Docker.

## Other assets
- `LICENSE` (MIT), `README.md` project overview, and `Dockerfile.operator`/`Makefile.operator` referenced above.
- `config/ingress/base/traefik.yaml` includes values for deploying Traefik via Kustomize.
- `config/ingress/overlays/http/service-ports.patch.yaml` etc. tweak ports and args for different ingress modes.
