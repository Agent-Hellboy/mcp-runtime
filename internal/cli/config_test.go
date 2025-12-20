package cli

import (
	"testing"
	"time"
)

func TestParseDurationEnv(t *testing.T) {
	t.Setenv("MCP_DEPLOYMENT_TIMEOUT", "2s")
	if got := parseDurationEnv("MCP_DEPLOYMENT_TIMEOUT", 5*time.Second); got != 2*time.Second {
		t.Fatalf("expected 2s, got %s", got)
	}

	t.Setenv("MCP_DEPLOYMENT_TIMEOUT", "bad")
	if got := parseDurationEnv("MCP_DEPLOYMENT_TIMEOUT", 5*time.Second); got != 5*time.Second {
		t.Fatalf("expected default on invalid duration, got %s", got)
	}
}

func TestParseIntEnv(t *testing.T) {
	t.Setenv("MCP_REGISTRY_PORT", "6000")
	if got := parseIntEnv("MCP_REGISTRY_PORT", 5000); got != 6000 {
		t.Fatalf("expected 6000, got %d", got)
	}

	t.Setenv("MCP_REGISTRY_PORT", "-1")
	if got := parseIntEnv("MCP_REGISTRY_PORT", 5000); got != 5000 {
		t.Fatalf("expected default on negative value, got %d", got)
	}

	t.Setenv("MCP_REGISTRY_PORT", "bad")
	if got := parseIntEnv("MCP_REGISTRY_PORT", 5000); got != 5000 {
		t.Fatalf("expected default on invalid int, got %d", got)
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	t.Setenv("MCP_SKOPEO_IMAGE", "example/image:tag")
	if got := getEnvOrDefault("MCP_SKOPEO_IMAGE", "default"); got != "example/image:tag" {
		t.Fatalf("expected env value, got %q", got)
	}

	t.Setenv("MCP_SKOPEO_IMAGE", "")
	if got := getEnvOrDefault("MCP_SKOPEO_IMAGE", "default"); got != "default" {
		t.Fatalf("expected default value, got %q", got)
	}
}

func TestLoadCLIConfigWithProvisionedRegistry(t *testing.T) {
	t.Setenv("MCP_DEPLOYMENT_TIMEOUT", "3s")
	t.Setenv("MCP_CERT_TIMEOUT", "30s")
	t.Setenv("MCP_REGISTRY_PORT", "6000")
	t.Setenv("MCP_SKOPEO_IMAGE", "example/skopeo:latest")
	t.Setenv("MCP_OPERATOR_IMAGE", "example/operator:latest")
	t.Setenv("MCP_DEFAULT_SERVER_PORT", "9000")
	t.Setenv("PROVISIONED_REGISTRY_URL", "registry.example.com")
	t.Setenv("PROVISIONED_REGISTRY_USERNAME", "user")
	t.Setenv("PROVISIONED_REGISTRY_PASSWORD", "pass")

	cfg := LoadCLIConfig()
	if cfg.DeploymentTimeout != 3*time.Second {
		t.Fatalf("expected deployment timeout 3s, got %s", cfg.DeploymentTimeout)
	}
	if cfg.CertTimeout != 30*time.Second {
		t.Fatalf("expected cert timeout 30s, got %s", cfg.CertTimeout)
	}
	if cfg.RegistryPort != 6000 {
		t.Fatalf("expected registry port 6000, got %d", cfg.RegistryPort)
	}
	if cfg.SkopeoImage != "example/skopeo:latest" {
		t.Fatalf("expected skopeo image override, got %q", cfg.SkopeoImage)
	}
	if cfg.OperatorImage != "example/operator:latest" {
		t.Fatalf("expected operator image override, got %q", cfg.OperatorImage)
	}
	if cfg.DefaultServerPort != 9000 {
		t.Fatalf("expected default server port 9000, got %d", cfg.DefaultServerPort)
	}
	if cfg.ProvisionedRegistryURL != "registry.example.com" {
		t.Fatalf("expected registry url, got %q", cfg.ProvisionedRegistryURL)
	}
	if cfg.ProvisionedRegistryUsername != "user" || cfg.ProvisionedRegistryPassword != "pass" {
		t.Fatalf("expected registry credentials, got %q/%q", cfg.ProvisionedRegistryUsername, cfg.ProvisionedRegistryPassword)
	}
}

func TestConfigAccessors(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() { DefaultCLIConfig = orig })

	DefaultCLIConfig = &CLIConfig{
		DeploymentTimeout: 10 * time.Second,
		CertTimeout:       15 * time.Second,
		RegistryPort:      7000,
		SkopeoImage:       "skopeo:test",
		OperatorImage:     "operator:test",
		DefaultServerPort: 7070,
	}

	if GetDeploymentTimeout() != 10*time.Second {
		t.Fatalf("GetDeploymentTimeout mismatch")
	}
	if GetCertTimeout() != 15*time.Second {
		t.Fatalf("GetCertTimeout mismatch")
	}
	if GetRegistryPort() != 7000 {
		t.Fatalf("GetRegistryPort mismatch")
	}
	if GetSkopeoImage() != "skopeo:test" {
		t.Fatalf("GetSkopeoImage mismatch")
	}
	if GetOperatorImageOverride() != "operator:test" {
		t.Fatalf("GetOperatorImageOverride mismatch")
	}
	if GetDefaultServerPort() != 7070 {
		t.Fatalf("GetDefaultServerPort mismatch")
	}
}
