package cli

import (
	"strings"
	"testing"
)

func TestRenderPlatformIngressManifestNoTLS(t *testing.T) {
	got := renderPlatformIngressManifest("platform.example.com", "")
	mustContain := []string{
		"name: " + platformIngressName,
		"namespace: " + defaultAnalyticsNamespace,
		"traefik.ingress.kubernetes.io/router.entrypoints: web",
		`- host: "platform.example.com"`,
		"name: mcp-sentinel-ui",
		"number: 8082",
		"name: grafana",
		"name: prometheus",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	if strings.Contains(got, "tls:") {
		t.Fatalf("did not expect a TLS block when issuer is empty:\n%s", got)
	}
	if strings.Contains(got, "cert-manager.io/cluster-issuer") {
		t.Fatalf("did not expect cert-manager annotation when issuer is empty:\n%s", got)
	}
}

func TestRenderPlatformIngressManifestWithTLS(t *testing.T) {
	got := renderPlatformIngressManifest("platform.mcpruntime.org", "letsencrypt-prod")
	mustContain := []string{
		"traefik.ingress.kubernetes.io/router.entrypoints: websecure",
		"cert-manager.io/cluster-issuer: letsencrypt-prod",
		"tls:",
		`- "platform.mcpruntime.org"`,
		"secretName: mcp-sentinel-platform-tls",
		`- host: "platform.mcpruntime.org"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
}

func TestACMETLSDNSNamesIncludesPlatformHost(t *testing.T) {
	prev := DefaultCLIConfig
	t.Cleanup(func() { DefaultCLIConfig = prev })
	DefaultCLIConfig = &CLIConfig{
		RegistryIngressHost: "registry.example.com",
		McpIngressHost:      "mcp.example.com",
		PlatformIngressHost: "platform.example.com",
	}
	names := acmeTLSDNSNames()
	want := map[string]bool{
		"registry.example.com": true,
		"mcp.example.com":      true,
		"platform.example.com": true,
	}
	if len(names) != len(want) {
		t.Fatalf("expected %d hostnames, got %d (%v)", len(want), len(names), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected hostname %q", n)
		}
	}
}
