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

	// RequeueDelaySeconds is the delay in seconds before requeueing when resources aren't ready.
	RequeueDelaySeconds int
}

// LoadOperatorConfig loads operator configuration from environment variables.
func LoadOperatorConfig() *OperatorConfig {
	cfg := &OperatorConfig{
		DefaultIngressHost:            getEnvOrDefault("DEFAULT_INGRESS_HOST", ""),
		DefaultIngressClass:           getEnvOrDefault("DEFAULT_INGRESS_CLASS", DefaultIngressClass),
		ProvisionedRegistryURL:        os.Getenv("PROVISIONED_REGISTRY_URL"),
		ProvisionedRegistryUsername:   os.Getenv("PROVISIONED_REGISTRY_USERNAME"),
		ProvisionedRegistryPassword:   os.Getenv("PROVISIONED_REGISTRY_PASSWORD"),
		ProvisionedRegistrySecretName: getEnvOrDefault("PROVISIONED_REGISTRY_SECRET_NAME", DefaultRegistrySecretName),
		RequeueDelaySeconds:           getEnvIntOrDefault("REQUEUE_DELAY_SECONDS", RequeueDelayNotReady),
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

// DefaultOperatorConfig is the default configuration loaded at startup.
var DefaultOperatorConfig = LoadOperatorConfig()
