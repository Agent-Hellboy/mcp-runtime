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
		"- path: /api\n",
		"- path: /\n",
		"name: mcp-sentinel-ui",
		"number: 8082",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	mustNotContain := []string{
		"- path: /grafana",
		"- path: /prometheus",
		"name: grafana",
		"name: prometheus",
		"name: " + platformHTTPRedirectIngressName,
	}
	for _, unwanted := range mustNotContain {
		if strings.Contains(got, unwanted) {
			t.Fatalf("manifest must not contain %q (Grafana/Prometheus must not be exposed publicly, redirect ingress only emitted with TLS):\n%s", unwanted, got)
		}
	}
	if strings.Contains(got, "tls:") {
		t.Fatalf("did not expect a TLS block when issuer is empty:\n%s", got)
	}
	if strings.Contains(got, "cert-manager.io/cluster-issuer") {
		t.Fatalf("did not expect cert-manager annotation when issuer is empty:\n%s", got)
	}
}

func TestRenderPlatformIngressManifestApiBeforeRoot(t *testing.T) {
	got := renderPlatformIngressManifest("platform.example.com", "")
	apiIdx := strings.Index(got, "- path: /api")
	rootIdx := strings.Index(got, "- path: /\n")
	if apiIdx < 0 || rootIdx < 0 {
		t.Fatalf("missing /api or / paths:\n%s", got)
	}
	// Traefik matches longer/more-specific prefixes before /, so /api must be
	// declared in the rule before the catch-all /.
	if apiIdx > rootIdx {
		t.Fatalf("/api must be listed before / catch-all:\n%s", got)
	}
}

func TestRenderPlatformIngressManifestWithTLS(t *testing.T) {
	got := renderPlatformIngressManifest("platform.mcpruntime.org", "letsencrypt-prod")
	mustContain := []string{
		"traefik.ingress.kubernetes.io/router.entrypoints: websecure",
		"cert-manager.io/cluster-issuer: letsencrypt-prod",
		"tls:",
		`- "platform.mcpruntime.org"`,
		"secretName: " + platformTLSSecretName,
		`- host: "platform.mcpruntime.org"`,
		"name: " + platformHTTPRedirectIngressName,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n    traefik.ingress.kubernetes.io/router.entrypoints: web\n  ingressClassName") {
		t.Fatalf("primary ingress should be on websecure when TLS issuer is set:\n%s", got)
	}
}

func TestRenderPlatformIngressManifestHTTPRedirectShape(t *testing.T) {
	got := renderPlatformIngressManifest("platform.mcpruntime.org", "letsencrypt-prod")
	idx := strings.Index(got, "name: "+platformHTTPRedirectIngressName)
	if idx < 0 {
		t.Fatalf("expected HTTP redirect ingress when TLS configured:\n%s", got)
	}
	tail := got[idx:]
	mustContain := []string{
		"traefik.ingress.kubernetes.io/router.entrypoints: web",
		`- host: "platform.mcpruntime.org"`,
		"- path: /\n",
		"name: mcp-sentinel-ui",
	}
	for _, want := range mustContain {
		if !strings.Contains(tail, want) {
			t.Fatalf("HTTP redirect ingress missing %q:\n%s", want, tail)
		}
	}
	// The HTTP redirect ingress must NOT request its own cert / TLS block.
	if strings.Contains(tail, "tls:") {
		t.Fatalf("HTTP redirect ingress must not have a tls block:\n%s", tail)
	}
	if strings.Contains(tail, "cert-manager.io/cluster-issuer") {
		t.Fatalf("HTTP redirect ingress must not request a certificate:\n%s", tail)
	}
}

// TestACMETLSDNSNamesExcludesPlatformHost asserts that the registry-cert SANs
// do NOT include the platform host. The platform Ingress in mcp-sentinel uses
// cert-manager's ingress-shim to mint its own cert; including the platform
// host in the registry-cert would cause a redundant ACME order on every
// renewal (and the secret in the registry namespace cannot be referenced from
// a different namespace by Kubernetes Ingress anyway).
func TestACMETLSDNSNamesExcludesPlatformHost(t *testing.T) {
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
	}
	if len(names) != len(want) {
		t.Fatalf("expected %d hostnames, got %d (%v)", len(want), len(names), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected hostname %q in registry SANs (platform host should be excluded)", n)
		}
	}
}
