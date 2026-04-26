package metadata

import (
	"testing"
)

func TestNormalizePlatformDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"MCPRUNTIME.com", "mcpruntime.com"},
		{"https://MCPRUNTIME.com/path", "mcpruntime.com"},
		{"http://mcpruntime.com:5000", "mcpruntime.com"},
		{"reg.mcpruntime.com:443", "reg.mcpruntime.com"},
	}
	for _, tc := range cases {
		if got := NormalizePlatformDomain(tc.in); got != tc.want {
			t.Errorf("NormalizePlatformDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveHostsWithPlatformDomain(t *testing.T) {
	for _, k := range []string{
		envMCPRegistryEndpoint, envMCPRegistryHost, envMCPRegistryIngressHost,
		envMCPPlatformDomain, envMCPMcpIngressHost, envMCPPlatformIngressHost,
	} {
		t.Setenv(k, "")
	}
	t.Setenv(envMCPPlatformDomain, "mcpruntime.com")

	if got := ResolveRegistryEndpoint(); got != "registry.mcpruntime.com" {
		t.Fatalf("ResolveRegistryEndpoint: got %q", got)
	}
	if got := ResolveRegistryHost(); got != "registry.mcpruntime.com" {
		t.Fatalf("ResolveRegistryHost: got %q", got)
	}
	if got := ResolveMcpIngressHost(); got != "mcp.mcpruntime.com" {
		t.Fatalf("ResolveMcpIngressHost: got %q", got)
	}
	if got := ResolvePlatformIngressHost(); got != "platform.mcpruntime.com" {
		t.Fatalf("ResolvePlatformIngressHost: got %q", got)
	}
}

func TestResolveHostsExplicitOverride(t *testing.T) {
	for k, v := range map[string]string{
		envMCPRegistryEndpoint:    "int.cluster:5000",
		envMCPRegistryHost:        "legacy",
		envMCPRegistryIngressHost: "reg.public.mcpruntime.com",
		envMCPPlatformDomain:      "should-not-matter",
		envMCPMcpIngressHost:      "mcp.custom.mcpruntime.com",
		envMCPPlatformIngressHost: "platform.custom.mcpruntime.com",
	} {
		t.Setenv(k, v)
	}
	if got := ResolveRegistryEndpoint(); got != "int.cluster:5000" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveRegistryHost(); got != "reg.public.mcpruntime.com" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveMcpIngressHost(); got != "mcp.custom.mcpruntime.com" {
		t.Fatalf("got %q", got)
	}
	if got := ResolvePlatformIngressHost(); got != "platform.custom.mcpruntime.com" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePlatformIngressHostUnset(t *testing.T) {
	for _, k := range []string{envMCPPlatformDomain, envMCPPlatformIngressHost} {
		t.Setenv(k, "")
	}
	if got := ResolvePlatformIngressHost(); got != "" {
		t.Fatalf("expected empty platform host when env is unset; got %q", got)
	}
}
