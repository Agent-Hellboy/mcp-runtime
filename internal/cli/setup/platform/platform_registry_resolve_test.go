package platform

import (
	"testing"

	"mcp-runtime/internal/cli/core"
)

func TestResolveInternalPlatformRegistryURLIgnoresPublicIngressHost(t *testing.T) {
	orig := core.DefaultCLIConfig
	t.Cleanup(func() { core.DefaultCLIConfig = orig })
	core.DefaultCLIConfig = &core.CLIConfig{
		RegistryEndpoint:    "registry.mcpruntime.org",
		RegistryIngressHost: "registry.mcpruntime.org",
	}

	// Ensure non-test-mode path so the function resolves via ClusterIP, not DNS shortcut.
	t.Setenv("MCP_RUNTIME_TEST_MODE", "")
	resetPlatformKubeconfig(t)
	swapKubernetesClientsForTest(t, platformTestClientsWithRegistryService(5000))
	initPlatformKubeconfig("")

	got := resolveInternalPlatformRegistryURLClientGo(nil)
	want := "10.96.201.51:5000"
	if got != want {
		t.Fatalf("internal registry URL = %q, want %q", got, want)
	}
}

func TestRegistryEndpointExplicitlyConfiguredForPlatform(t *testing.T) {
	t.Setenv("MCP_REGISTRY_ENDPOINT", "")
	t.Setenv("MCP_REGISTRY_HOST", "registry.mcpruntime.org")
	if registryEndpointExplicitlyConfiguredForPlatform() {
		t.Fatal("MCP_REGISTRY_HOST alone must not mark registry endpoint explicit for platform pulls")
	}

	t.Setenv("MCP_REGISTRY_ENDPOINT", "10.43.69.247:5000")
	if !registryEndpointExplicitlyConfiguredForPlatform() {
		t.Fatal("expected MCP_REGISTRY_ENDPOINT to mark registry endpoint explicit")
	}
}
