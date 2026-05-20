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
		"name: " + PlatformObservabilityIngressName,
		"namespace: " + testAnalyticsNS,
		"traefik.ingress.kubernetes.io/router.entrypoints: web",
		"traefik.ingress.kubernetes.io/router.middlewares: sentinel-admin-auth@file",
		`- host: "platform.example.com"`,
		"- path: /api\n",
		"- path: /grafana\n",
		"- path: /prometheus\n",
		"- path: /\n",
		"name: mcp-sentinel-ui",
		"name: grafana",
		"name: prometheus",
		"number: 8082",
		"number: 3000",
		"number: 9090",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	mustNotContain := []string{
		"name: " + PlatformHTTPRedirectIngressName,
	}
	for _, unwanted := range mustNotContain {
		if strings.Contains(got, unwanted) {
			t.Fatalf("manifest must not contain %q (redirect ingress only emitted with TLS):\n%s", unwanted, got)
		}
	}
	if strings.Contains(got, "tls:") {
		t.Fatalf("did not expect a TLS block when issuer is empty:\n%s", got)
	}
	if strings.Contains(got, "cert-manager.io/cluster-issuer") {
		t.Fatalf("did not expect cert-manager annotation when issuer is empty:\n%s", got)
	}
}

func TestRenderPlatformUIIngressApiBeforeRoot(t *testing.T) {
	got := RenderPlatformUIIngress("platform.example.com", "", testAnalyticsNS)
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

func TestRenderPlatformUIIngressWithTLS(t *testing.T) {
	got := RenderPlatformUIIngress("platform.mcpruntime.org", "letsencrypt-prod", testAnalyticsNS)
	mustContain := []string{
		"traefik.ingress.kubernetes.io/router.entrypoints: websecure",
		"cert-manager.io/cluster-issuer: letsencrypt-prod",
		"tls:",
		`- "platform.mcpruntime.org"`,
		"secretName: " + PlatformTLSSecretName,
		`- host: "platform.mcpruntime.org"`,
		"name: " + PlatformObservabilityIngressName,
		"name: " + PlatformHTTPRedirectIngressName,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in manifest:\n%s", want, got)
		}
	}
	if count := strings.Count(got, "cert-manager.io/cluster-issuer:"); count != 1 {
		t.Fatalf("expected exactly one cert-manager annotation, got %d:\n%s", count, got)
	}
	if strings.Contains(got, "\n    traefik.ingress.kubernetes.io/router.entrypoints: web\n  ingressClassName") {
		t.Fatalf("primary ingress should be on websecure when TLS issuer is set:\n%s", got)
	}
}

func TestRenderPlatformObservabilityIngressShape(t *testing.T) {
	got := RenderPlatformUIIngress("platform.example.com", "", testAnalyticsNS)
	idx := strings.Index(got, "name: "+PlatformObservabilityIngressName)
	if idx < 0 {
		t.Fatalf("expected platform observability ingress:\n%s", got)
	}
	tail := got[idx:]
	mustContain := []string{
		"namespace: " + testAnalyticsNS,
		"traefik.ingress.kubernetes.io/router.entrypoints: web",
		"traefik.ingress.kubernetes.io/router.middlewares: sentinel-admin-auth@file",
		`- host: "platform.example.com"`,
		"- path: /grafana\n",
		"name: grafana",
		"number: 3000",
		"- path: /prometheus\n",
		"name: prometheus",
		"number: 9090",
	}
	for _, want := range mustContain {
		if !strings.Contains(tail, want) {
			t.Fatalf("observability ingress missing %q:\n%s", want, tail)
		}
	}
	if strings.Contains(tail, "cert-manager.io/cluster-issuer") {
		t.Fatalf("observability ingress must not request a certificate:\n%s", tail)
	}
}

func TestRenderPlatformObservabilityIngressWithTLS(t *testing.T) {
	got := RenderPlatformUIIngress("platform.mcpruntime.org", "letsencrypt-prod", testAnalyticsNS)
	idx := strings.Index(got, "name: "+PlatformObservabilityIngressName)
	if idx < 0 {
		t.Fatalf("expected platform observability ingress:\n%s", got)
	}
	tail := got[idx:]
	if redirectIdx := strings.Index(tail, "name: "+PlatformHTTPRedirectIngressName); redirectIdx >= 0 {
		tail = tail[:redirectIdx]
	}
	mustContain := []string{
		"namespace: " + testAnalyticsNS,
		"traefik.ingress.kubernetes.io/router.entrypoints: websecure",
		"traefik.ingress.kubernetes.io/router.middlewares: sentinel-admin-auth@file",
		"tls:",
		`- "platform.mcpruntime.org"`,
		"secretName: " + PlatformTLSSecretName,
		`- host: "platform.mcpruntime.org"`,
		"- path: /grafana\n",
		"name: grafana",
		"- path: /prometheus\n",
		"name: prometheus",
	}
	for _, want := range mustContain {
		if !strings.Contains(tail, want) {
			t.Fatalf("TLS observability ingress missing %q:\n%s", want, tail)
		}
	}
	if strings.Contains(tail, "cert-manager.io/cluster-issuer") {
		t.Fatalf("observability ingress must not request a certificate:\n%s", tail)
	}
}

func TestRenderPlatformUIIngressHTTPRedirectShape(t *testing.T) {
	got := RenderPlatformUIIngress("platform.mcpruntime.org", "letsencrypt-prod", testAnalyticsNS)
	idx := strings.Index(got, "name: "+PlatformHTTPRedirectIngressName)
	if idx < 0 {
		t.Fatalf("expected HTTP redirect ingress when TLS configured:\n%s", got)
	}
	tail := got[idx:]
	mustContain := []string{
		"namespace: " + testAnalyticsNS,
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
	if strings.Contains(tail, "tls:") {
		t.Fatalf("HTTP redirect ingress must not have a tls block:\n%s", tail)
	}
	if strings.Contains(tail, "cert-manager.io/cluster-issuer") {
		t.Fatalf("HTTP redirect ingress must not request a certificate:\n%s", tail)
	}
}
