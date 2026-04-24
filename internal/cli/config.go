package cli

// This file defines CLI configuration loading from environment variables.
// CLIConfig holds all CLI settings including timeouts, registry settings, and server defaults.

import (
	"os"
	"strconv"
	"time"
)

// CLIConfig holds all CLI configuration loaded from environment variables.
// Use LoadCLIConfig() to create an instance with values from the environment.
type CLIConfig struct {
	// Timeouts
	DeploymentTimeout time.Duration
	CertTimeout       time.Duration
	HelperPodTimeout  time.Duration

	// Registry settings
	RegistryPort        int
	RegistryEndpoint    string
	RegistryIngressHost string
	SkopeoImage         string
	OperatorImage       string // Override for operator image
	GatewayProxyImage   string // Optional default image for the MCP gateway sidecar
	AnalyticsIngestURL  string // Optional analytics ingest URL override for the MCP gateway sidecar
	ClusterName         string // Optional cluster label attached to analytics/audit events

	// Server defaults
	DefaultServerPort int

	// External/Provisioned registry credentials
	ProvisionedRegistryURL      string
	ProvisionedRegistryUsername string
	ProvisionedRegistryPassword string
}

// Default values
const (
	defaultDeploymentTimeout   = 5 * time.Minute
	defaultCertTimeout         = 60 * time.Second
	defaultHelperPodTimeout    = 3 * time.Minute
	defaultRegistryPort        = 5000
	defaultRegistryEndpoint    = "registry.local"
	defaultRegistryIngressHost = "registry.local"
	defaultSkopeoImage         = "quay.io/skopeo/stable:v1.14"
	defaultServerPort          = 8088
)

// DefaultCLIConfig is the global CLI configuration loaded at startup.
var DefaultCLIConfig = LoadCLIConfig()

// LoadCLIConfig loads CLI configuration from environment variables.
func LoadCLIConfig() *CLIConfig {
	registryEndpoint := os.Getenv("MCP_REGISTRY_ENDPOINT")
	if registryEndpoint == "" {
		registryEndpoint = getEnvOrDefault("MCP_REGISTRY_HOST", defaultRegistryEndpoint)
	}
	registryIngressHost := os.Getenv("MCP_REGISTRY_INGRESS_HOST")
	if registryIngressHost == "" {
		registryIngressHost = getEnvOrDefault("MCP_REGISTRY_HOST", defaultRegistryIngressHost)
	}
	return &CLIConfig{
		DeploymentTimeout:           parseDurationEnv("MCP_DEPLOYMENT_TIMEOUT", defaultDeploymentTimeout),
		CertTimeout:                 parseDurationEnv("MCP_CERT_TIMEOUT", defaultCertTimeout),
		HelperPodTimeout:            parseDurationEnv("MCP_HELPER_POD_TIMEOUT", defaultHelperPodTimeout),
		RegistryPort:                parseIntEnv("MCP_REGISTRY_PORT", defaultRegistryPort),
		RegistryEndpoint:            registryEndpoint,
		RegistryIngressHost:         registryIngressHost,
		SkopeoImage:                 getEnvOrDefault("MCP_SKOPEO_IMAGE", defaultSkopeoImage),
		OperatorImage:               os.Getenv("MCP_OPERATOR_IMAGE"), // No default, empty means auto
		GatewayProxyImage:           os.Getenv("MCP_GATEWAY_PROXY_IMAGE"),
		AnalyticsIngestURL:          getEnvCompat("MCP_SENTINEL_INGEST_URL", "MCP_ANALYTICS_INGEST_URL"),
		ClusterName:                 getEnvOrDefault("MCP_CLUSTER_NAME", "local"),
		DefaultServerPort:           parseIntEnv("MCP_DEFAULT_SERVER_PORT", defaultServerPort),
		ProvisionedRegistryURL:      os.Getenv("PROVISIONED_REGISTRY_URL"),
		ProvisionedRegistryUsername: os.Getenv("PROVISIONED_REGISTRY_USERNAME"),
		ProvisionedRegistryPassword: os.Getenv("PROVISIONED_REGISTRY_PASSWORD"),
	}
}

// parseDurationEnv parses a duration from an environment variable, returning the default if not set or invalid.
func parseDurationEnv(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

// parseIntEnv parses an integer from an environment variable, returning the default if not set or invalid.
func parseIntEnv(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil && i > 0 {
			return i
		}
	}
	return defaultVal
}

// getEnvOrDefault returns the environment variable value or the default if not set.
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvCompat(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// --- Convenience accessors using DefaultCLIConfig ---

// GetDeploymentTimeout returns the deployment wait timeout.
func GetDeploymentTimeout() time.Duration {
	return DefaultCLIConfig.DeploymentTimeout
}

// GetCertTimeout returns the certificate issuance timeout.
func GetCertTimeout() time.Duration {
	return DefaultCLIConfig.CertTimeout
}

// GetHelperPodTimeout returns the helper pod ready timeout (e.g. registry pusher pod).
func GetHelperPodTimeout() time.Duration {
	return DefaultCLIConfig.HelperPodTimeout
}

// GetRegistryPort returns the registry port.
func GetRegistryPort() int {
	return DefaultCLIConfig.RegistryPort
}

// GetRegistryEndpoint returns the configured registry endpoint for image refs and pushes.
func GetRegistryEndpoint() string {
	return DefaultCLIConfig.RegistryEndpoint
}

// GetRegistryIngressHost returns the configured registry ingress host.
func GetRegistryIngressHost() string {
	return DefaultCLIConfig.RegistryIngressHost
}

// GetSkopeoImage returns the skopeo image for in-cluster operations.
func GetSkopeoImage() string {
	return DefaultCLIConfig.SkopeoImage
}

// GetOperatorImageOverride returns the operator image override, empty if not set.
func GetOperatorImageOverride() string {
	return DefaultCLIConfig.OperatorImage
}

// GetGatewayProxyImageOverride returns the gateway proxy image override, empty if not set.
func GetGatewayProxyImageOverride() string {
	return DefaultCLIConfig.GatewayProxyImage
}

// GetAnalyticsIngestURLOverride returns the analytics ingest URL override, empty if not set.
func GetAnalyticsIngestURLOverride() string {
	return DefaultCLIConfig.AnalyticsIngestURL
}

// GetClusterName returns the cluster label attached to analytics/audit events.
func GetClusterName() string {
	return DefaultCLIConfig.ClusterName
}

// GetDefaultServerPort returns the default MCP server port.
func GetDefaultServerPort() int {
	return DefaultCLIConfig.DefaultServerPort
}
