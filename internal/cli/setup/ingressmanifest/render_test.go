package ingressmanifest

import (
	"strings"
	"testing"
)

const testAnalyticsNS = "mcp-sentinel"

func TestRenderPlatformUIIngressNoTLS(t *testing.T) {
	got := RenderPlatformUIIngress("platform.example.com", "", testAnalyticsNS)
	mustContain := []string{
		"name: " + PlatformIngressName,
		"namespace: " + testAnalyticsNS,
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

func TestRenderPlatformUIIngressApiBeforeGrafana(t *testing.T) {
	got := RenderPlatformUIIngress("platform.example.com", "", testAnalyticsNS)
	apiIdx := strings.Index(got, "- path: /api")
	grafanaIdx := strings.Index(got, "- path: /grafana")
	rootIdx := strings.Index(got, "- path: /\n")
	if apiIdx < 0 || grafanaIdx < 0 || rootIdx < 0 {
		t.Fatalf("missing one of /api, /grafana, / paths:\n%s", got)
	}
	if apiIdx > grafanaIdx {
		t.Fatalf("/api must be listed before /grafana in the rule for readability:\n%s", got)
	}
	if grafanaIdx > rootIdx {
		t.Fatalf("/grafana must be listed before / catch-all:\n%s", got)
	}
}

func TestRenderPlatformUIIngressWithTLS(t *testing.T) {
	got := RenderPlatformUIIngress("platform.mcpruntime.org", "letsencrypt-prod", testAnalyticsNS)
	mustContain := []string{
		"traefik.ingress.kubernetes.io/router.entrypoints: websecure",
		"cert-manager.io/cluster-issuer: letsencrypt-prod",
		"tls:",
		`- "platform.mcpruntime.org"`,
		"secretName: " + PlatformTLSSecretName,
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
