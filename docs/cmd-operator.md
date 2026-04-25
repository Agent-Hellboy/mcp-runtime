# cmd/operator/main.go

- L1: `package main` declares the binary entrypoint for the Kubernetes operator manager.
- L3-L15: imports CLI flag parsing, OS exit helpers, Kubernetes scheme/runtime packages, controller-runtime logger/health/metrics helpers, and the local CRD/operator packages.
- L17-L21: package-level scheme/log variables (`scheme` initialized empty; `setupLog` scoped logger).
- L23-L26: `init` registers Kubernetes built-in types and MCP CRD types into the shared scheme; fatal on error via `Must`.
- L28-L54: `main` defines flag-backed configuration for metrics/health ports and leader election, binds zap logging flags, parses args, and builds a controller manager with the configured scheme and endpoints; exits on manager creation failure.
- L56-L66: registers `MCPServerReconciler` with the manager so reconcile loops run; errors terminate the process.
- L68-L74: adds health and readiness probes using ping responders, failing fast on setup errors.
- L76-L83: logs startup and blocks on `mgr.Start`, exiting on fatal errors; signal handler allows graceful shutdown.

# internal/operator/controller.go

- L1: package `operator` implements the reconciler logic for the MCPServer CRD.
- L3-L22: imports context, encoding, Kubernetes API types (Deployments/Service/Ingress), controller-runtime helpers, and the CRD types.
- L24-L31: `MCPServerReconciler` struct embeds a controller-runtime client and scheme; default CPU/memory request/limit constants follow.
- L33-L42: kubebuilder RBAC markers declare permissions for MCPServer resources and owned resources (Deployments, Services, Ingresses, Leases, Secrets, Events).
- L44-L98: `Reconcile` fetches the MCPServer instance, ignoring NotFound, logs context, applies defaults via `setDefaults`, persists spec changes if defaults were added, requires `spec.ingressHost` (or env default) before continuing, then sequentially reconciles Deployment, Service, and Ingress. After resource reconciliation it checks readiness for each resource, computes phase (`Ready`, `PartiallyReady`, or `Pending`), updates status accordingly, logs success, and returns.
- L100-L135: `setDefaults` fills missing fields: default image tag when not provided, default replicas=1, container port=8088, service port=80, ingress path `/{name}/mcp`, ingress host from env `MCP_DEFAULT_INGRESS_HOST` when unset, and default ingress class `traefik`.
- L137-L208: `reconcileDeployment` builds/updates a Deployment for the server. It resolves the image (respecting tags and registry overrides), sets selector/labels, pod template with pull secrets, container definition (port, env vars, probes), optional resource limits/requests parsed from strings, applies default resource thresholds via `applyContainerResourceDefaults`, attaches owner reference, and logs when the Deployment was created/updated.
- L210-L232: `applyContainerResourceDefaults` ensures CPU/memory requests and limits exist, populating defaults when not provided in the container spec.
- L234-L271: `resolveImage` composes the full image string: appends tag if missing, rewrites the registry when `RegistryOverride` or provisioned-registry flags are set (preferring env `PROVISIONED_REGISTRY_URL`), and logs fallback behavior.
- L273-L289: `rewriteRegistry` swaps or prepends the registry host segment while preserving image path components, dropping any existing registry prefix heuristically.
- L291-L331: `buildImagePullSecrets` prefers explicit `spec.imagePullSecrets`; otherwise, if provisioned registry env vars are set, it ensures/creates a docker config secret in the namespace (via `ensureRegistryPullSecret`) and returns a reference to be used by the Deployment.
- L333-L372: `ensureRegistryPullSecret` marshals docker auth JSON, fetches or creates the secret, and updates it when credentials change.
- L374-L419: `reconcileService` ensures a ClusterIP Service exposing the target port -> container port, sets selector labels, attaches owner reference, and logs operation results.
- L421-L473: `reconcileIngress` ensures an Ingress routing `spec.ingressHost` + `spec.ingressPath` to the Service with an optional ingress class, applies annotations built by `buildIngressAnnotations`, attaches owner reference, and logs reconciliation results.
- L475-L528: readiness helpers (`checkDeploymentReady`, `checkServiceReady`, `checkIngressReady`) fetch resources and compute readiness flags (replicas ready, ClusterIP allocated, load balancer address present).
- L530-L539: `updateStatus` writes back MCPServer status fields (phase, message, readiness flags) and logs errors on failure.
- L541-L548: `buildEnvVars` converts CRD env var structs into corev1 env vars for the pod template.
- L550-L596: `buildIngressAnnotations` merges user annotations and adds sensible defaults per ingress class (Traefik, NGINX, Istio, or generic rewrite annotations).
- L598-L604: `SetupWithManager` wires the reconciler with controller-runtime, declaring ownership of Deployment, Service, and Ingress resources tied to MCPServer instances.
