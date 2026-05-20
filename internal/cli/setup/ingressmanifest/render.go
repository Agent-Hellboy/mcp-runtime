// Package ingressmanifest builds YAML for the host-based Sentinel platform UI Ingress.
package ingressmanifest

import (
	"strconv"
	"strings"
)

const (
	// PlatformIngressName is the Kubernetes Ingress resource name for the dashboard.
	PlatformIngressName = "mcp-sentinel-platform-ui"
	// PlatformObservabilityIngressName is the admin-gated platform Ingress for observability tools.
	PlatformObservabilityIngressName = "mcp-sentinel-platform-observability"
	// PlatformHTTPRedirectIngressName is the HTTP-only redirect Ingress resource name.
	PlatformHTTPRedirectIngressName = "mcp-sentinel-platform-ui-http"
	// PlatformTLSSecretName is the TLS secret name used when TLS is enabled.
	PlatformTLSSecretName = "mcp-sentinel-platform-tls"
)

// RenderPlatformUIIngress emits an Ingress that maps platform.<domain> to the
// dashboard UI and /api on the same UI service (which reverse-proxies to
// mcp-sentinel-api via API_UPSTREAM). It also emits a separate admin-gated
// Ingress on the same host for /grafana and /prometheus. The observability
// Ingress uses the repo-managed sentinel-admin-auth@file Traefik middleware so
// those tools are reachable from admin UI links without exposing them raw on
// the public platform host.
//
// When issuerName is set, a TLS section and cert-manager annotation are added
// so cert-manager's ingress-shim provisions a Certificate for platform.<domain>
// into the mcp-sentinel-platform-tls Secret in the same namespace as the UI
// Ingress. The observability Ingress references the same TLS Secret without a
// cert-manager annotation to avoid a second Certificate owner. A third Ingress
// on the `web` entrypoint is also emitted so HTTP requests to the same host hit
// the UI service, which redirects to HTTPS.
// (We can't rely on Traefik's entrypoint-level redirect because the prod
// overlay disables it to keep HTTP-01 ACME challenges working on first issue.)
func RenderPlatformUIIngress(host, issuerName, analyticsNamespace string) string {
	host = strings.TrimSpace(host)
	issuerName = strings.TrimSpace(issuerName)
	analyticsNamespace = strings.TrimSpace(analyticsNamespace)

	var b strings.Builder
	b.WriteString("apiVersion: networking.k8s.io/v1\n")
	b.WriteString("kind: Ingress\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: ")
	b.WriteString(PlatformIngressName)
	b.WriteString("\n")
	b.WriteString("  namespace: ")
	b.WriteString(analyticsNamespace)
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
		b.WriteString(PlatformTLSSecretName)
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
	b.WriteString("          - path: /\n")
	b.WriteString("            pathType: Prefix\n")
	b.WriteString("            backend:\n")
	b.WriteString("              service:\n")
	b.WriteString("                name: mcp-sentinel-ui\n")
	b.WriteString("                port:\n")
	b.WriteString("                  number: 8082\n")

	b.WriteString("---\n")
	b.WriteString("apiVersion: networking.k8s.io/v1\n")
	b.WriteString("kind: Ingress\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: ")
	b.WriteString(PlatformObservabilityIngressName)
	b.WriteString("\n")
	b.WriteString("  namespace: ")
	b.WriteString(analyticsNamespace)
	b.WriteString("\n")
	b.WriteString("  annotations:\n")
	if issuerName != "" {
		b.WriteString("    traefik.ingress.kubernetes.io/router.entrypoints: websecure\n")
	} else {
		b.WriteString("    traefik.ingress.kubernetes.io/router.entrypoints: web\n")
	}
	b.WriteString("    traefik.ingress.kubernetes.io/router.middlewares: sentinel-admin-auth@file\n")
	b.WriteString("spec:\n")
	b.WriteString("  ingressClassName: traefik\n")
	if issuerName != "" {
		b.WriteString("  tls:\n")
		b.WriteString("    - hosts:\n")
		b.WriteString("        - ")
		b.WriteString(strconv.Quote(host))
		b.WriteString("\n")
		b.WriteString("      secretName: ")
		b.WriteString(PlatformTLSSecretName)
		b.WriteString("\n")
	}
	b.WriteString("  rules:\n")
	b.WriteString("    - host: ")
	b.WriteString(strconv.Quote(host))
	b.WriteString("\n")
	b.WriteString("      http:\n")
	b.WriteString("        paths:\n")
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

	if issuerName != "" {
		// HTTP-only ingress on the same host so plain `http://platform.<domain>/`
		// hits the UI service (which 308s to HTTPS) instead of falling through to
		// the host-less dev gateway ingress in k8s/10-gateway.yaml.
		b.WriteString("---\n")
		b.WriteString("apiVersion: networking.k8s.io/v1\n")
		b.WriteString("kind: Ingress\n")
		b.WriteString("metadata:\n")
		b.WriteString("  name: ")
		b.WriteString(PlatformHTTPRedirectIngressName)
		b.WriteString("\n")
		b.WriteString("  namespace: ")
		b.WriteString(analyticsNamespace)
		b.WriteString("\n")
		b.WriteString("  annotations:\n")
		b.WriteString("    traefik.ingress.kubernetes.io/router.entrypoints: web\n")
		b.WriteString("spec:\n")
		b.WriteString("  ingressClassName: traefik\n")
		b.WriteString("  rules:\n")
		b.WriteString("    - host: ")
		b.WriteString(strconv.Quote(host))
		b.WriteString("\n")
		b.WriteString("      http:\n")
		b.WriteString("        paths:\n")
		b.WriteString("          - path: /\n")
		b.WriteString("            pathType: Prefix\n")
		b.WriteString("            backend:\n")
		b.WriteString("              service:\n")
		b.WriteString("                name: mcp-sentinel-ui\n")
		b.WriteString("                port:\n")
		b.WriteString("                  number: 8082\n")
	}

	return b.String()
}
