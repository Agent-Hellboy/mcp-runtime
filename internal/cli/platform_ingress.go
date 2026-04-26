package cli

import (
	"fmt"
	"strconv"
	"strings"
)

const platformIngressName = "mcp-sentinel-platform-ui"
const platformTLSSecretName = "mcp-sentinel-platform-tls"

// applyPlatformIngressIfConfigured applies a host-based ingress for the
// dashboard UI on platform.<MCP_PLATFORM_DOMAIN> when MCP_PLATFORM_INGRESS_HOST
// (or MCP_PLATFORM_DOMAIN) is set. When unset, the dev path-based gateway
// ingress in k8s/10-gateway.yaml continues to handle all dashboard traffic.
func applyPlatformIngressIfConfigured(kubectl KubectlRunner) error {
	host := strings.TrimSpace(GetPlatformIngressHost())
	if host == "" {
		return nil
	}
	manifest := renderPlatformIngressManifest(host, GetRegistryClusterIssuerName())
	Info(fmt.Sprintf("Applying platform UI ingress for %s", host))
	if err := applyManifestContent(kubectl, manifest); err != nil {
		return fmt.Errorf("apply platform UI ingress: %w", err)
	}
	return nil
}

// renderPlatformIngressManifest emits an Ingress that maps platform.<domain>
// to the dashboard UI, /api on the same UI service (which reverse-proxies to
// mcp-sentinel-api via API_UPSTREAM), and the in-cluster Grafana / Prometheus
// paths. When issuerName is set, a TLS section and cert-manager annotation are
// added so cert-manager's ingress-shim provisions a Certificate for
// platform.<domain> into the mcp-sentinel-platform-tls Secret in the same
// namespace as the Ingress.
func renderPlatformIngressManifest(host, issuerName string) string {
	host = strings.TrimSpace(host)
	issuerName = strings.TrimSpace(issuerName)

	var b strings.Builder
	b.WriteString("apiVersion: networking.k8s.io/v1\n")
	b.WriteString("kind: Ingress\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: ")
	b.WriteString(platformIngressName)
	b.WriteString("\n")
	b.WriteString("  namespace: ")
	b.WriteString(defaultAnalyticsNamespace)
	b.WriteString("\n")
	b.WriteString("  annotations:\n")
	if issuerName != "" {
		b.WriteString("    traefik.ingress.kubernetes.io/router.entrypoints: websecure\n")
		b.WriteString("    cert-manager.io/cluster-issuer: ")
		b.WriteString(issuerName)
		b.WriteString("\n")
	} else {
		b.WriteString("    traefik.ingress.kubernetes.io/router.entrypoints: web\n")
	}
	b.WriteString("spec:\n")
	b.WriteString("  ingressClassName: traefik\n")
	if issuerName != "" {
		b.WriteString("  tls:\n")
		b.WriteString("    - hosts:\n")
		b.WriteString("        - ")
		b.WriteString(strconv.Quote(host))
		b.WriteString("\n")
		b.WriteString("      secretName: ")
		b.WriteString(platformTLSSecretName)
		b.WriteString("\n")
	}
	b.WriteString("  rules:\n")
	b.WriteString("    - host: ")
	b.WriteString(strconv.Quote(host))
	b.WriteString("\n")
	b.WriteString("      http:\n")
	b.WriteString("        paths:\n")
	b.WriteString("          - path: /api\n")
	b.WriteString("            pathType: Prefix\n")
	b.WriteString("            backend:\n")
	b.WriteString("              service:\n")
	b.WriteString("                name: mcp-sentinel-ui\n")
	b.WriteString("                port:\n")
	b.WriteString("                  number: 8082\n")
	b.WriteString("          - path: /grafana\n")
	b.WriteString("            pathType: Prefix\n")
	b.WriteString("            backend:\n")
	b.WriteString("              service:\n")
	b.WriteString("                name: grafana\n")
	b.WriteString("                port:\n")
	b.WriteString("                  number: 3000\n")
	b.WriteString("          - path: /prometheus\n")
	b.WriteString("            pathType: Prefix\n")
	b.WriteString("            backend:\n")
	b.WriteString("              service:\n")
	b.WriteString("                name: prometheus\n")
	b.WriteString("                port:\n")
	b.WriteString("                  number: 9090\n")
	b.WriteString("          - path: /\n")
	b.WriteString("            pathType: Prefix\n")
	b.WriteString("            backend:\n")
	b.WriteString("              service:\n")
	b.WriteString("                name: mcp-sentinel-ui\n")
	b.WriteString("                port:\n")
	b.WriteString("                  number: 8082\n")
	return b.String()
}
