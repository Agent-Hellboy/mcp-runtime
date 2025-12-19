// Package operator provides the Kubernetes operator for MCPServer resources.
package operator

// Resource defaults for MCPServer deployments.
const (
	// DefaultRequestCPU is the default CPU request for containers.
	DefaultRequestCPU = "50m"
	// DefaultRequestMemory is the default memory request for containers.
	DefaultRequestMemory = "64Mi"
	// DefaultLimitCPU is the default CPU limit for containers.
	DefaultLimitCPU = "500m"
	// DefaultLimitMemory is the default memory limit for containers.
	DefaultLimitMemory = "256Mi"
)

// MCPServer defaults.
const (
	// DefaultReplicas is the default number of replicas.
	DefaultReplicas = 1
	// DefaultPort is the default container port.
	DefaultPort = 8080
	// DefaultServicePort is the default service port.
	DefaultServicePort = 80
)

// Labels used by the operator.
const (
	// LabelApp is the standard app label key.
	LabelApp = "app"
	// LabelManagedBy is the label indicating the managing controller.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// LabelManagedByValue is the value for the managed-by label.
	LabelManagedByValue = "mcp-runtime"
)

// Secret names.
const (
	// DefaultRegistrySecretName is the default name for registry pull secrets.
	// #nosec G101 -- This is a secret name, not a credential.
	DefaultRegistrySecretName = "mcp-runtime-registry-creds"
)

// Ingress configuration.
const (
	// DefaultIngressClass is the default ingress class.
	DefaultIngressClass = "traefik"
	// DefaultIngressPathType is the default path type for ingress rules.
	DefaultIngressPathType = "Prefix"
)

// Requeue delays for reconciliation.
const (
	// RequeueDelayNotReady is the delay before requeueing when resources are not ready.
	RequeueDelayNotReady = 10 // seconds
)
