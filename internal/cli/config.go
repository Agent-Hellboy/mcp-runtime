// Package cli provides CLI configuration and commands for the mcp-runtime.
package cli

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

	// Registry settings
	RegistryPort  int
	SkopeoImage   string
	OperatorImage string // Override for operator image

	// Server defaults
	DefaultServerPort int

	// External/Provisioned registry credentials
	ProvisionedRegistryURL      string
	ProvisionedRegistryUsername string
	ProvisionedRegistryPassword string
}

// Default values
const (
	defaultDeploymentTimeout = 5 * time.Minute
	defaultCertTimeout       = 60 * time.Second
	defaultRegistryPort      = 5000
	defaultSkopeoImage       = "quay.io/skopeo/stable:v1.14"
	defaultServerPort        = 8088
)

// DefaultCLIConfig is the global CLI configuration loaded at startup.
var DefaultCLIConfig = LoadCLIConfig()

// LoadCLIConfig loads CLI configuration from environment variables.
func LoadCLIConfig() *CLIConfig {
	return &CLIConfig{
		DeploymentTimeout:           parseDurationEnv("MCP_DEPLOYMENT_TIMEOUT", defaultDeploymentTimeout),
		CertTimeout:                 parseDurationEnv("MCP_CERT_TIMEOUT", defaultCertTimeout),
		RegistryPort:                parseIntEnv("MCP_REGISTRY_PORT", defaultRegistryPort),
		SkopeoImage:                 getEnvOrDefault("MCP_SKOPEO_IMAGE", defaultSkopeoImage),
		OperatorImage:               os.Getenv("MCP_OPERATOR_IMAGE"), // No default, empty means auto
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

// --- Convenience accessors using DefaultCLIConfig ---

// GetDeploymentTimeout returns the deployment wait timeout.
func GetDeploymentTimeout() time.Duration {
	return DefaultCLIConfig.DeploymentTimeout
}

// GetCertTimeout returns the certificate issuance timeout.
func GetCertTimeout() time.Duration {
	return DefaultCLIConfig.CertTimeout
}

// GetRegistryPort returns the registry port.
func GetRegistryPort() int {
	return DefaultCLIConfig.RegistryPort
}

// GetSkopeoImage returns the skopeo image for in-cluster operations.
func GetSkopeoImage() string {
	return DefaultCLIConfig.SkopeoImage
}

// GetOperatorImageOverride returns the operator image override, empty if not set.
func GetOperatorImageOverride() string {
	return DefaultCLIConfig.OperatorImage
}

// GetDefaultServerPort returns the default MCP server port.
func GetDefaultServerPort() int {
	return DefaultCLIConfig.DefaultServerPort
}
