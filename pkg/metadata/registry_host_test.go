package metadata

import "testing"

func clearRegistryEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envMCPRegistryEndpoint, envMCPRegistryHost, envMCPRegistryIngressHost, envMCPRegistryPullHost, envMCPPlatformDomain} {
		t.Setenv(key, "")
	}
}

// The internal endpoint must not fall back to the public ingress host.
func TestResolveRegistryEndpointSkipsIngressHost(t *testing.T) {
	clearRegistryEnv(t)
	t.Setenv(envMCPRegistryIngressHost, "registry.public.example")
	if got := ResolveRegistryEndpoint(); got != DefaultRegistryHost {
		t.Fatalf("ResolveRegistryEndpoint = %q, want the default, not the ingress host", got)
	}
	t.Setenv(envMCPRegistryHost, "registry.internal.example")
	if got := ResolveRegistryEndpoint(); got != "registry.internal.example" {
		t.Fatalf("ResolveRegistryEndpoint = %q, want MCP_REGISTRY_HOST", got)
	}
}

func TestRegistryHostForDomainDoesNotDoublePrefix(t *testing.T) {
	clearRegistryEnv(t)
	t.Setenv(envMCPPlatformDomain, "registry.example.com")
	if got := ResolveRegistryHost(); got != "registry.example.com" {
		t.Fatalf("ResolveRegistryHost = %q", got)
	}
	if got := ResolveRegistryEndpoint(); got != "registry.example.com" {
		t.Fatalf("ResolveRegistryEndpoint = %q", got)
	}
}

func TestImageRefForClusterPull(t *testing.T) {
	clearRegistryEnv(t)
	t.Setenv(envMCPPlatformDomain, "example.com")
	pull := defaultRegistryPullHost
	for image, want := range map[string]string{
		// Unqualified and platform images move to the in-cluster pull host.
		"demo:1":                      pull + "/demo:1",
		"registry.example.com/demo:1": pull + "/demo:1",
		"REGISTRY.Example.com/demo:1": pull + "/demo:1",
		"registry.local/demo:1":       pull + "/demo:1",
		"localhost:5000/demo:1":       pull + "/demo:1",
		// External registries are preserved.
		"ghcr.io/org/demo:1":            "ghcr.io/org/demo:1",
		"docker.io/library/nginx:1":     "docker.io/library/nginx:1",
		"registry.attacker.example/x:1": "registry.attacker.example/x:1",
	} {
		if got := imageRefForClusterPull(image); got != want {
			t.Errorf("imageRefForClusterPull(%q) = %q, want %q", image, got, want)
		}
	}
}
