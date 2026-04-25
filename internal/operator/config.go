package operator

import (
	"os"
	"strconv"
)

// OperatorConfig holds configuration for the operator loaded from environment variables.
type OperatorConfig struct {
	// DefaultIngressHost is the default host for ingress resources.
	DefaultIngressHost string

	// DefaultIngressClass is the ingress class to use.
	DefaultIngressClass string

	// ProvisionedRegistryURL is the URL of the provisioned registry.
	ProvisionedRegistryURL string

	// ProvisionedRegistryUsername is the username for the provisioned registry.
	ProvisionedRegistryUsername string

	// ProvisionedRegistryPassword is the password for the provisioned registry.
	ProvisionedRegistryPassword string

	// ProvisionedRegistrySecretName is the name of the secret for registry credentials.
	ProvisionedRegistrySecretName string

	// InternalRegistryEndpoint is the internal registry endpoint to use for image refs when not using a provisioned registry.
	InternalRegistryEndpoint string

	// RequeueDelaySeconds is the delay in seconds before requeueing when resources aren't ready.
	RequeueDelaySeconds int

	// GatewayProxyImage is the default image used for the optional MCP gateway sidecar.
	GatewayProxyImage string

	// AnalyticsIngestURL is the default analytics ingest endpoint for gateway sidecars.
	AnalyticsIngestURL string

	// ClusterName is the cluster label attached to emitted audit events.
	ClusterName string
}

// LoadOperatorConfig loads operator configuration from environment variables.
func LoadOperatorConfig() *OperatorConfig {
	cfg := &OperatorConfig{
		DefaultIngressHost:            getEnvCompat("MCP_DEFAULT_INGRESS_HOST", "DEFAULT_INGRESS_HOST"),
		DefaultIngressClass:           getEnvOrDefault("DEFAULT_INGRESS_CLASS", DefaultIngressClass),
		ProvisionedRegistryURL:        os.Getenv("PROVISIONED_REGISTRY_URL"),
		ProvisionedRegistryUsername:   os.Getenv("PROVISIONED_REGISTRY_USERNAME"),
		ProvisionedRegistryPassword:   os.Getenv("PROVISIONED_REGISTRY_PASSWORD"),
		ProvisionedRegistrySecretName: getEnvOrDefault("PROVISIONED_REGISTRY_SECRET_NAME", DefaultRegistrySecretName),
		InternalRegistryEndpoint:      getEnvOrDefault("MCP_REGISTRY_ENDPOINT", getEnvOrDefault("MCP_REGISTRY_HOST", "registry.local")),
		RequeueDelaySeconds:           getEnvIntOrDefault("REQUEUE_DELAY_SECONDS", RequeueDelayNotReady),
		GatewayProxyImage:             os.Getenv("MCP_GATEWAY_PROXY_IMAGE"),
		AnalyticsIngestURL:            getEnvCompat("MCP_SENTINEL_INGEST_URL", "MCP_ANALYTICS_INGEST_URL"),
		ClusterName:                   getEnvOrDefault("MCP_CLUSTER_NAME", "local"),
	}
	return cfg
}

// HasProvisionedRegistry returns true if a provisioned registry is configured.
func (c *OperatorConfig) HasProvisionedRegistry() bool {
	return c.ProvisionedRegistryURL != ""
}

// ToRegistryConfig converts the config to a RegistryConfig if provisioned registry is enabled.
func (c *OperatorConfig) ToRegistryConfig() *RegistryConfig {
	if !c.HasProvisionedRegistry() {
		return nil
	}
	return &RegistryConfig{
		URL:        c.ProvisionedRegistryURL,
		Username:   c.ProvisionedRegistryUsername,
		Password:   c.ProvisionedRegistryPassword,
		SecretName: c.ProvisionedRegistrySecretName,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvCompat(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// DefaultOperatorConfig is the default configuration loaded at startup.
var DefaultOperatorConfig = LoadOperatorConfig()
