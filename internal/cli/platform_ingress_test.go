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
		"- path: /grafana\n",
		"- path: /prometheus\n",
		"- path: /\n",
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

func TestRenderPlatformIngressManifestApiBeforeGrafana(t *testing.T) {
	got := renderPlatformIngressManifest("platform.example.com", "")
	apiIdx := strings.Index(got, "- path: /api")
	grafanaIdx := strings.Index(got, "- path: /grafana")
	rootIdx := strings.Index(got, "- path: /\n")
	if apiIdx < 0 || grafanaIdx < 0 || rootIdx < 0 {
		t.Fatalf("missing one of /api, /grafana, / paths:\n%s", got)
	}
	// Traefik matches longer/more-specific prefixes before /, so /api must
	// appear in the manifest and be a sibling of /grafana, /prometheus.
	if apiIdx > grafanaIdx {
		t.Fatalf("/api must be listed before /grafana in the rule for readability:\n%s", got)
	}
	if grafanaIdx > rootIdx {
		t.Fatalf("/grafana must be listed before / catch-all:\n%s", got)
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
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n    traefik.ingress.kubernetes.io/router.entrypoints: web\n") {
		t.Fatalf("did not expect plain web entrypoint when TLS issuer is set:\n%s", got)
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
