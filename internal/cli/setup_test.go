package cli

import (
	"os"
	"testing"
	"time"
)

func TestLoadCLIConfig(t *testing.T) {
	// Save original env vars
	origDeployTimeout := os.Getenv("MCP_DEPLOYMENT_TIMEOUT")
	origCertTimeout := os.Getenv("MCP_CERT_TIMEOUT")
	origRegistryPort := os.Getenv("MCP_REGISTRY_PORT")
	origSkopeoImage := os.Getenv("MCP_SKOPEO_IMAGE")
	origOperatorImage := os.Getenv("MCP_OPERATOR_IMAGE")
	origServerPort := os.Getenv("MCP_DEFAULT_SERVER_PORT")

	// Restore on cleanup
	defer func() {
		os.Setenv("MCP_DEPLOYMENT_TIMEOUT", origDeployTimeout)
		os.Setenv("MCP_CERT_TIMEOUT", origCertTimeout)
		os.Setenv("MCP_REGISTRY_PORT", origRegistryPort)
		os.Setenv("MCP_SKOPEO_IMAGE", origSkopeoImage)
		os.Setenv("MCP_OPERATOR_IMAGE", origOperatorImage)
		os.Setenv("MCP_DEFAULT_SERVER_PORT", origServerPort)
	}()

	t.Run("uses defaults when env vars not set", func(t *testing.T) {
		os.Unsetenv("MCP_DEPLOYMENT_TIMEOUT")
		os.Unsetenv("MCP_CERT_TIMEOUT")
		os.Unsetenv("MCP_REGISTRY_PORT")
		os.Unsetenv("MCP_SKOPEO_IMAGE")
		os.Unsetenv("MCP_OPERATOR_IMAGE")
		os.Unsetenv("MCP_DEFAULT_SERVER_PORT")

		cfg := LoadCLIConfig()

		assertCLIConfig(t, cfg, cliConfigExpectation{
			deploymentTimeout: defaultDeploymentTimeout,
			certTimeout:       defaultCertTimeout,
			registryPort:      defaultRegistryPort,
			skopeoImage:       defaultSkopeoImage,
			operatorImage:     "",
			defaultServerPort: defaultServerPort,
		})
	})

	t.Run("reads env vars when set", func(t *testing.T) {
		os.Setenv("MCP_DEPLOYMENT_TIMEOUT", "10m")
		os.Setenv("MCP_CERT_TIMEOUT", "2m")
		os.Setenv("MCP_REGISTRY_PORT", "5001")
		os.Setenv("MCP_SKOPEO_IMAGE", "custom/skopeo:v2")
		os.Setenv("MCP_OPERATOR_IMAGE", "custom/operator:v1")
		os.Setenv("MCP_DEFAULT_SERVER_PORT", "9000")

		cfg := LoadCLIConfig()

		assertCLIConfig(t, cfg, cliConfigExpectation{
			deploymentTimeout: 10 * time.Minute,
			certTimeout:       2 * time.Minute,
			registryPort:      5001,
			skopeoImage:       "custom/skopeo:v2",
			operatorImage:     "custom/operator:v1",
			defaultServerPort: 9000,
		})
	})

	t.Run("handles invalid values gracefully", func(t *testing.T) {
		os.Setenv("MCP_DEPLOYMENT_TIMEOUT", "invalid")
		os.Setenv("MCP_REGISTRY_PORT", "not-a-number")
		os.Setenv("MCP_DEFAULT_SERVER_PORT", "-1")

		cfg := LoadCLIConfig()

		assertCLIConfig(t, cfg, cliConfigExpectation{
			deploymentTimeout: defaultDeploymentTimeout,
			certTimeout:       defaultCertTimeout,
			registryPort:      defaultRegistryPort,
			skopeoImage:       defaultSkopeoImage,
			operatorImage:     "",
			defaultServerPort: defaultServerPort,
		})
	})
}

func TestProvisionedRegistryConfig(t *testing.T) {
	origURL := os.Getenv("PROVISIONED_REGISTRY_URL")
	origUser := os.Getenv("PROVISIONED_REGISTRY_USERNAME")
	origPass := os.Getenv("PROVISIONED_REGISTRY_PASSWORD")

	defer func() {
		os.Setenv("PROVISIONED_REGISTRY_URL", origURL)
		os.Setenv("PROVISIONED_REGISTRY_USERNAME", origUser)
		os.Setenv("PROVISIONED_REGISTRY_PASSWORD", origPass)
	}()

	os.Setenv("PROVISIONED_REGISTRY_URL", "registry.example.com")
	os.Setenv("PROVISIONED_REGISTRY_USERNAME", "user")
	os.Setenv("PROVISIONED_REGISTRY_PASSWORD", "pass")

	cfg := LoadCLIConfig()

	if cfg.ProvisionedRegistryURL != "registry.example.com" {
		t.Errorf("ProvisionedRegistryURL = %v, want %v", cfg.ProvisionedRegistryURL, "registry.example.com")
	}
	if cfg.ProvisionedRegistryUsername != "user" {
		t.Errorf("ProvisionedRegistryUsername = %v, want %v", cfg.ProvisionedRegistryUsername, "user")
	}
	if cfg.ProvisionedRegistryPassword != "pass" {
		t.Errorf("ProvisionedRegistryPassword = %v, want %v", cfg.ProvisionedRegistryPassword, "pass")
	}
}

type cliConfigExpectation struct {
	deploymentTimeout time.Duration
	certTimeout       time.Duration
	registryPort      int
	skopeoImage       string
	operatorImage     string
	defaultServerPort int
}

func assertCLIConfig(t *testing.T, cfg CLIConfig, want cliConfigExpectation) {
	t.Helper()
	if cfg.DeploymentTimeout != want.deploymentTimeout {
		t.Errorf("DeploymentTimeout = %v, want %v", cfg.DeploymentTimeout, want.deploymentTimeout)
	}
	if cfg.CertTimeout != want.certTimeout {
		t.Errorf("CertTimeout = %v, want %v", cfg.CertTimeout, want.certTimeout)
	}
	if cfg.RegistryPort != want.registryPort {
		t.Errorf("RegistryPort = %v, want %v", cfg.RegistryPort, want.registryPort)
	}
	if cfg.SkopeoImage != want.skopeoImage {
		t.Errorf("SkopeoImage = %v, want %v", cfg.SkopeoImage, want.skopeoImage)
	}
	if cfg.OperatorImage != want.operatorImage {
		t.Errorf("OperatorImage = %v, want %v", cfg.OperatorImage, want.operatorImage)
	}
	if cfg.DefaultServerPort != want.defaultServerPort {
		t.Errorf("DefaultServerPort = %v, want %v", cfg.DefaultServerPort, want.defaultServerPort)
	}
}
