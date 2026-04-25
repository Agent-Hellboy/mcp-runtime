# internal/cli package

This package implements the `mcp-runtime` CLI commands. Each subsection walks through the files in order of execution and major blocks of lines.

## output.go
- L1 declares package and imports; L9-L16 define ANSI color helpers used for readable console output.
- L18-L33: `printSection/printStep/printSuccess/printError/printWarn/printInfo` format labeled messages to stdout/stderr for consistent UX.

## bootstrap.go
- L1-L10: Package and imports for cobra/zap and stdlib helpers used by the preflight workflow.
- L16-L72: `NewBootstrapCmd` wires the `bootstrap` cobra command. Flags: `--apply` (default false; only automated for k3s) and `--provider` (default `auto`). The `RunE` body detects the provider, runs `runBootstrapPreflight`, and — only when `--apply` is set — branches per provider: k3s applies the bundled CoreDNS and local-path manifests; everything else (`rke2`, `kubeadm`, `generic`) prints a warning that apply is not yet automated.
- L74-L88: `detectProvider` shells out to `kubectl get nodes -o jsonpath=...kubeletVersion` and substring-matches `k3s`/`rke2`, returning `generic` otherwise.
- L90-L120: `runBootstrapPreflight` checks kubectl connectivity, a CoreDNS deployment in `kube-system`, a default StorageClass annotation, the `traefik` IngressClass, and the `metallb-system` namespace. Missing pieces produce warnings (not errors) so users can decide what to install.
- L122-L138: `checkDeploymentExists` and `checkHasDefaultStorageClass` helpers used by the preflight loop.
- L140-L181: `bootstrapApplyK3s` is the only automated apply path. It confirms the k3s server-node manifests exist on disk under `/var/lib/rancher/k3s/server/manifests`, applies them via `kubectl`, then waits for the CoreDNS and local-path-provisioner rollouts. Best-effort, it also prints node disk-pressure status.
- L183-L189: `kubectlOutput` thin wrapper that builds a kubectl command via the runner and returns its captured output.

## access.go
- L1-L15: Package, imports, and the resource constants `accessGrantResource` (`mcpaccessgrant`) and `accessSessionResource` (`mcpagentsession`) used throughout the file.
- L17-L28: `AccessManager` struct, `NewAccessManager` constructor, and `DefaultAccessManager` factory bound to the package-level `kubectlClient`.
- L30-L46: `NewAccessCmd` and `NewAccessCmdWithManager` build the `access` root command and attach the `grant` and `session` subtrees.
- L48-L78: `newAccessGrantCmd` and `newAccessSessionCmd` wire the `list / get / apply / delete / disable|enable` and `list / get / apply / delete / revoke|unrevoke` subcommands respectively, sharing the helper builders below.
- L80-L96: `newAccessListCmd` registers a `list` command with `--namespace` and `--all-namespaces` (defaulting to all-namespaces). Calls `ListAccessResources`.
- L98-L113: `newAccessGetCmd` registers `get [name]` (single-arg) with a `--namespace` flag defaulting to `NamespaceMCPServers`. Calls `GetAccessResource`.
- L115-L130: `newAccessApplyCmd` registers `apply --file <manifest>` (file is required). Calls `ApplyAccessResource`.
- L132-L147: `newAccessDeleteCmd` registers `delete [name]` with namespace flag. Calls `DeleteAccessResource`.
- L149-L164: `newAccessToggleCmd` is the shared builder for the four toggle commands (`disable/enable` on grants, `revoke/unrevoke` on sessions). It captures the boolean value to set and calls `ToggleAccessResource`.
- L166-L183: `ListAccessResources` runs `kubectl get <resource>` with namespace scoping and streams output through the kubectl client.
- L185-L201: `GetAccessResource` validates the name/namespace pair, then runs `kubectl get <resource> <name> -n <ns> -o yaml`.
- L203-L211: `ApplyAccessResource` delegates to `applyManifestFromFile` so the user-supplied YAML/JSON is applied via the kubectl client.
- L213-L229: `DeleteAccessResource` runs `kubectl delete <resource> <name> -n <ns>` after the same input validation.
- L231-L267: `ToggleAccessResource` builds a strategic merge patch — `spec.disabled=<value>` for grants, `spec.revoked=<value>` for sessions — marshals it to JSON, then runs `kubectl patch ... --type merge --patch <json>`. Unknown resource kinds error out.
- L269-L283: `validateAccessInput` enforces the shared name regex (`validServerName`) and uses `validateManifestValue` to scrub name/namespace before they reach kubectl.

## status.go
- L1-L17: command wiring for `mcp-runtime status` that prints operator/registry/server health.
- L19-L74: `checkPlatformStatus` shells out to `kubectl` to fetch operator pods, registry pods, CRD presence, and lists MCPServer resources. It streams outputs directly to stdout/stderr and returns errors when kubectl fails.

## cluster.go
- L1-L32: imports, default cluster name, and ingress option struct used by setup/config commands.
- L34-L48: `NewClusterCmd` defines the `cluster` root command and attaches subcommands.
- L50-L78: `newClusterInitCmd` wires the `init` subcommand; collects kubeconfig/context flags and calls `initCluster`.
- L80-L99: `newClusterStatusCmd` defines `cluster status` that delegates to `checkClusterStatus`.
- L101-L132: `newClusterConfigCmd` defines `cluster config` to install ingress controllers using `configureCluster` and ingress flags.
- L134-L167: `newClusterProvisionCmd` wires the `provision` subcommand for creating clusters via provider-specific helpers.
- L169-L216: `initCluster` resolves kubeconfig (defaulting to `~/.kube/config`), ensures it exists, optionally switches context, applies the MCP CRD manifest, creates namespaces `mcp-runtime` and `mcp-servers`, and logs progress.
- L218-L249: `checkClusterStatus` runs `kubectl` commands to show cluster info, nodes, CRD status, and operator pods, streaming output.
- L251-L305: `configureCluster` validates ingress mode, detects existing ingress classes unless forced, chooses manifest path (files or kustomize directories), applies via `kubectl`, and logs completion.
- L306-L362: `provisionCluster` dispatches to provider-specific provisioning helpers (Kind/GKE/EKS/AKS) and errors on unknown providers.
- L364-L394: `provisionKindCluster` builds a temporary kind cluster config based on node count, writes it to a temp file, runs `kind create cluster`, and cleans up the temp file.
- L396-L421: `provisionGKECluster`, `provisionEKSCluster`, and `provisionAKSCluster` currently return informative errors describing how to create clusters manually.
- L423-L431: `ensureNamespace` applies a simple Namespace manifest piped to `kubectl apply -f -`.

## build.go
- L1-L20: imports CLI/logging helpers, metadata package, and YAML marshalling.
- L22-L51: `newBuildImageCmd` defines `pipeline image` subcommand flags (dockerfile, metadata file/dir, registry URL, tag, context) and calls `buildImage` with optional server name argument.
- L53-L110: `buildImage` loads metadata from a file or directory, determines registry URL (defaults to platform registry), determines tag (git SHA or `latest`), filters servers by name if provided, and for each server builds a Docker image with `docker build -t <registry>/<name>:<tag>`; logs success and attempts to update metadata with the new image/tag.
- L112-L170: `updateMetadataImage` locates the metadata YAML containing the target server (searches directory if necessary), loads it, updates the server image/tag fields, marshals YAML, and writes back to disk.
- L172-L197: `getPlatformRegistryURL` tries to resolve the in-cluster registry service IP/port via `kubectl`; falls back to `registry.registry.svc.cluster.local:5000` if detection fails.
- L199-L208: `getGitTag` returns a short git SHA via `git rev-parse --short HEAD` or `latest` on failure.

## pipeline.go
- L1-L28: imports and `NewPipelineCmd` root wiring for pipeline-related commands.
- L30-L73: `newPipelineGenerateCmd` defines `pipeline generate` with flags for metadata dir/output dir; loads metadata via `pkg/metadata` and writes CRD YAML for each server using `GenerateCRDsFromRegistry`.
- L75-L130: `newPipelineDeployCmd` defines `pipeline deploy` to apply generated manifests from a directory via `kubectl apply -f`.
- L132-L195: `newPipelineRenderCmd` defines `pipeline render` to render CRDs to stdout instead of files, loading metadata and marshalling each CRD using the metadata generator.
- L197-L216: `writeCRDs` helper ensures the output dir exists and writes CRD YAML content for each server by calling `metadata.GenerateCRDsFromRegistry` (for file output) or direct marshal when rendering.

## registry.go
- L1-L22: imports and top-level registry command registration (`NewRegistryCmd`) attaching status/info/provision/push subcommands.
- L24-L60: `newRegistryStatusCmd` wires `registry status` which calls `checkRegistryStatus` with namespace flag.
- L62-L77: `newRegistryInfoCmd` prints registry connection info via `showRegistryInfo`.
- L79-L129: `newRegistryProvisionCmd` handles external registry configuration: merges flag/env/config, saves config YAML under `~/.mcp-runtime/registry.yaml`, optionally performs `docker login`, and optionally builds/pushes the operator image to that registry.
- L131-L196: `newRegistryPushCmd` retags and pushes an image to the platform/provisioned registry; resolves target registry precedence, parses source image into repo/tag, derives target name, and dispatches to direct or in-cluster push strategies (`pushDirect` vs `pushInCluster`).
- L198-L230: `ExternalRegistryConfig` struct plus helpers `registryConfigPath` and `saveExternalRegistryConfig` for persisting config to disk.
- L232-L264: `resolveExternalRegistryConfig` loads registry config from flags, env vars, or config file and returns nil when nothing is configured.
- L266-L292: `checkRegistryStatus` calls `kubectl get` to display pods and services in the registry namespace and prints ingress/ingressClass resources.
- L294-L339: `showRegistryInfo` prints platform registry service endpoints and tips for pushing/pulling; uses kubectl to grab service details.
- L341-L389: `deployRegistry` applies or updates a Docker registry deployment using kustomize or plain manifest path and patches the PVC size when provided.
- L391-L421: `waitForDeploymentAvailable` polls deployment readiness until timeout using `kubectl wait`.
- L423-L466: `pushDirect` performs `docker tag` and `docker push`, then reports success.
- L468-L547: `pushInCluster` spins up a temporary Kubernetes Job in the helper namespace running the `quay.io/skopeo/stable` image to copy from local Docker daemon to target registry using skopeo; it builds a YAML manifest with inline credentials when provided, applies it, tails logs, waits for completion, fetches job logs, and cleans up the job.
- L549-L584: `splitImage` and `dropRegistryPrefix` helpers parse image references.
- L586-L637: `loginRegistry` runs `docker login` non-interactively using provided credentials.
- L639-L665: `buildOperatorImage` builds the operator Dockerfile using `make -f Makefile.operator docker-build-operator IMG=<image>`.
- L667-L680: `pushOperatorImage` tags/pushes the operator image via Docker.
- L682-L733: `pushOperatorImageToInternalRegistry` uses an in-cluster helper job plus a temporary secret to push the operator image into the platform registry, cleaning up resources afterward.
- L735-L749: `printDeploymentDiagnostics` prints describe/get output for deployments/services when setup fails.

## server.go
- L1-L22: imports and `NewServerCmd` which provides server CRUD commands and nests build helpers.
- L24-L46: `newServerBuildCmd` exposes image build via `newBuildImageCmd` (push is handled by `registry push`).
- L48-L76: `newServerListCmd` lists MCPServers in a namespace via `kubectl get`.
- L78-L103: `newServerGetCmd` fetches a single MCPServer as YAML.
- L105-L146: `newServerCreateCmd` creates a server either from inline flags (image/tag/namespace) or a provided manifest file; delegates to `createServer` or `createServerFromFile`.
- L148-L174: `newServerDeleteCmd` deletes a server resource by name/namespace using `kubectl delete mcpserver`.
- L176-L204: `newServerLogsCmd` streams pod logs for a server label selector, optionally following output.
- L206-L233: `newServerStatusCmd` calls `serverStatus` to show deployments, pods, images, and pull secrets for all MCPServers in a namespace.
- L235-L303: `createServer` validates inputs, constructs a minimal MCPServer manifest with defaults, writes it to `/tmp`, and applies it with `kubectl apply`; `createServerFromFile` just applies a provided YAML file.
- L305-L322: `deleteServer` wraps `kubectl delete mcpserver` with logging.
- L324-L345: `viewServerLogs` builds `kubectl logs` args with label selectors and optional `-f`.
- L347-L402: `serverStatus` prints MCPServer/Deployment/Pod listings, then iterates MCPServer specs to show image and registry flags plus pull secrets discovered from deployments.
- L404-L429: Manifest struct definitions (`mcpServerManifest`, `manifestMetadata`, `manifestSpec`) used by `createServer`; `validateManifestValue` trims/validates fields to avoid invalid YAML content.

## setup.go
- L1-L25: imports, constants, and `defaultRegistrySecretName` for provisioned registry credentials.
- L27-L63: `NewSetupCmd` wires the top-level `setup` command with flags for registry type/storage, ingress mode/manifest, TLS overlay, and test mode.
- L65-L152: `setupPlatform` executes the end-to-end setup: prints steps, loads external registry config, initializes cluster (`initCluster`), configures ingress (`configureCluster`), deploys registry (internal or external login), waits for readiness, shows registry info, builds/pushes operator image (or uses test-mode kind-loaded image), deploys operator manifests, configures provisioned registry env on operator, restarts deployment, verifies setup, and prints success.
- L154-L205: `getOperatorImage` picks a default operator image (local tag or provisioned registry override) depending on external registry/test mode flags.
- L207-L257: `deployOperatorManifests` uses kustomize to render operator manifests, substitutes the operator image, applies them via `kubectl`, and patches the deployment image when needed.
- L259-L293: `restartDeployment` triggers a rollout restart on the operator deployment to pick up env changes.
- L295-L333: `verifySetup` calls `checkClusterStatus` and `checkRegistryStatus`, ensuring registry ingress routes resolve; on external registry, it requires external config.
- L335-L373: `resolveExternalRegistryConfig` variant for setup reads config/env/flags to decide whether to use an external registry.
- L375-L436: `configureProvisionedRegistryEnv` patches operator deployment env vars with provisioned registry credentials using `kubectl set env` and creates the pull secret in the `mcp-runtime` namespace.
- L438-L455: `waitForPodsDeleted` polls until pods with a label selector disappear after restarts.
- L457-L516: `pushOperatorImageToInternalRegistry` helper reuses registry push logic to move the operator image inside the cluster, then logs the internal image reference.
- L518-L547: `getDeploymentImage` fetches a deployment's current image via `kubectl get -o jsonpath`.
- L549-L623: `deployRegistry` and `printDeploymentDiagnostics` are re-used; setup.go calls into registry helpers above when needed.

## pipeline/build/server/status tests
- `output_test.go`, `registry_test.go`, `server_test.go`, `controller_test.go`, and `pipeline`/`build`-related tests validate the CLI wiring and behaviors using mocks, golden output snapshots, or stub command executions.
