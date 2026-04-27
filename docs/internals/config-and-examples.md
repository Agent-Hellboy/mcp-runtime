# Config, Examples, and Supporting Assets

## config/
- `config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml` defines the MCPServer CRD with schema, printer columns, and validation; used by kubectl apply during setup.
- `config/default/kustomization.yaml` kustomize entry assembling manager, RBAC, CRD, and webhook assets for operator deployment.
- `config/manager` holds the controller-manager Deployment and PodDisruptionBudget manifests; `manager.yaml` sets image placeholder, args, probes, and RBAC subjects.
- `config/rbac` contains service account, role, and role binding definitions used by the operator.
- `config/ingress` provides base Traefik ingress controller manifests and overlays (`http`, `prod`, `dev`) to install ingress with different args/ports.
- `config/registry` contains manifests to deploy the internal Docker registry (namespace, deployment, service, pvc, ingress) plus a TLS overlay with ingress/secret adjustments.
- `config/cert-manager` offers sample cert-manager issuer/certificate for securing registry ingress.

## examples/
- `go-mcp-server/` is the primary sample server used by smoke/e2e flows.
- `python-mcp-server/` and `rust-mcp-server/` are language-specific sample servers used in e2e validation.
- `mcpserver-path-based.yaml` is the maintained MCPServer manifest example for path-based ingress.

## Makefiles
- `Makefile` exposes high-level tasks (fmt, lint, test, build) for the CLI binary.
- `Makefile.runtime` bundles runtime-specific build/install tasks.
- `Makefile.operator` builds operator manifests, docker image, and runs controller-gen tools; used by setup/build scripts.

## Dockerfiles
- `Dockerfile.operator` builds the operator image with Go build steps and copies manifests.
- `examples/go-mcp-server/Dockerfile` builds the primary sample server container.
- `test/e2e/Dockerfile` builds images for end-to-end tests.

## Scripts
- `hack/dev-setup.sh` automates local dev environment prep (kind cluster, registry, ingress installation, CRD apply) with informative logging.
- `test/e2e-kind.sh` creates a kind cluster with preloaded images for testing; `test/e2e/run-in-docker.sh` runs e2e flows inside Docker.

## Other assets
- `LICENSE` (MIT), `README.md` project overview, and `Dockerfile.operator`/`Makefile.operator` referenced above.
- `config/ingress/base/traefik.yaml` includes values for deploying Traefik via Kustomize.
- `config/ingress/overlays/http/service-ports.patch.yaml` etc. tweak ports and args for different ingress modes.
