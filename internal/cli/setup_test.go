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

		if cfg.DeploymentTimeout != defaultDeploymentTimeout {
			t.Errorf("DeploymentTimeout = %v, want %v", cfg.DeploymentTimeout, defaultDeploymentTimeout)
		}
		if cfg.CertTimeout != defaultCertTimeout {
			t.Errorf("CertTimeout = %v, want %v", cfg.CertTimeout, defaultCertTimeout)
		}
		if cfg.RegistryPort != defaultRegistryPort {
			t.Errorf("RegistryPort = %v, want %v", cfg.RegistryPort, defaultRegistryPort)
		}
		if cfg.SkopeoImage != defaultSkopeoImage {
			t.Errorf("SkopeoImage = %v, want %v", cfg.SkopeoImage, defaultSkopeoImage)
		}
		if cfg.OperatorImage != "" {
			t.Errorf("OperatorImage = %v, want empty", cfg.OperatorImage)
		}
		if cfg.DefaultServerPort != defaultServerPort {
			t.Errorf("DefaultServerPort = %v, want %v", cfg.DefaultServerPort, defaultServerPort)
		}
	})

	t.Run("reads env vars when set", func(t *testing.T) {
		os.Setenv("MCP_DEPLOYMENT_TIMEOUT", "10m")
		os.Setenv("MCP_CERT_TIMEOUT", "2m")
		os.Setenv("MCP_REGISTRY_PORT", "5001")
		os.Setenv("MCP_SKOPEO_IMAGE", "custom/skopeo:v2")
		os.Setenv("MCP_OPERATOR_IMAGE", "custom/operator:v1")
		os.Setenv("MCP_DEFAULT_SERVER_PORT", "9000")

		cfg := LoadCLIConfig()

		if cfg.DeploymentTimeout != 10*time.Minute {
			t.Errorf("DeploymentTimeout = %v, want %v", cfg.DeploymentTimeout, 10*time.Minute)
		}
		if cfg.CertTimeout != 2*time.Minute {
			t.Errorf("CertTimeout = %v, want %v", cfg.CertTimeout, 2*time.Minute)
		}
		if cfg.RegistryPort != 5001 {
			t.Errorf("RegistryPort = %v, want %v", cfg.RegistryPort, 5001)
		}
		if cfg.SkopeoImage != "custom/skopeo:v2" {
			t.Errorf("SkopeoImage = %v, want %v", cfg.SkopeoImage, "custom/skopeo:v2")
		}
		if cfg.OperatorImage != "custom/operator:v1" {
			t.Errorf("OperatorImage = %v, want %v", cfg.OperatorImage, "custom/operator:v1")
		}
		if cfg.DefaultServerPort != 9000 {
			t.Errorf("DefaultServerPort = %v, want %v", cfg.DefaultServerPort, 9000)
		}
	})

	t.Run("handles invalid values gracefully", func(t *testing.T) {
		os.Setenv("MCP_DEPLOYMENT_TIMEOUT", "invalid")
		os.Setenv("MCP_REGISTRY_PORT", "not-a-number")
		os.Setenv("MCP_DEFAULT_SERVER_PORT", "-1")

		cfg := LoadCLIConfig()

		// Should fall back to defaults
		if cfg.DeploymentTimeout != defaultDeploymentTimeout {
			t.Errorf("DeploymentTimeout = %v, want default %v", cfg.DeploymentTimeout, defaultDeploymentTimeout)
		}
		if cfg.RegistryPort != defaultRegistryPort {
			t.Errorf("RegistryPort = %v, want default %v", cfg.RegistryPort, defaultRegistryPort)
		}
		if cfg.DefaultServerPort != defaultServerPort {
			t.Errorf("DefaultServerPort = %v, want default %v", cfg.DefaultServerPort, defaultServerPort)
		}
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
