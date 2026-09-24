package v1alpha1

import (
	"net/url"
	"path"
	"strings"
)

const (
	traefikRouterTLSAnnotation = "traefik.ingress.kubernetes.io/router.tls"
	oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"
)

// PublicURLOptions carries the operator-wide ingress settings that decide the
// public URL of an MCPServer when the spec leaves them unset.
type PublicURLOptions struct {
	DefaultIngressHost string
	DefaultIngressTLS  bool
}

// EffectivePublicPath returns the path clients reach the MCP endpoint on:
// "/<publicPathPrefix>/mcp" for path-based routing, else spec.ingressPath.
func (r *MCPServer) EffectivePublicPath() string {
	prefix := strings.Trim(strings.TrimSpace(r.Spec.PublicPathPrefix), "/")
	if prefix == "" {
		return r.Spec.IngressPath
	}
	return "/" + prefix + "/mcp"
}

// PublicBaseURL returns scheme://host for the server's public ingress, or ""
// when no host is known. Path-based servers leave spec.ingressHost empty (the
// ingress matches any host), so the operator-wide default host is used; the
// scheme follows the operator TLS default or the per-server Traefik TLS
// annotation, because the ingress, not the pod, terminates TLS.
func (r *MCPServer) PublicBaseURL(options PublicURLOptions) string {
	host := strings.TrimSpace(r.Spec.IngressHost)
	if host == "" {
		host = strings.TrimSpace(options.DefaultIngressHost)
	}
	if host == "" {
		return ""
	}
	scheme := "http"
	if r.PublicIngressUsesTLS(options.DefaultIngressTLS) {
		scheme = "https"
	}
	return scheme + "://" + host
}

// PublicIngressUsesTLS reports whether the server's ingress terminates TLS,
// either from the operator-wide default or an explicit Traefik annotation.
func (r *MCPServer) PublicIngressUsesTLS(defaultTLS bool) bool {
	if defaultTLS {
		return true
	}
	for key, value := range r.Spec.IngressAnnotations {
		if strings.EqualFold(strings.TrimSpace(key), traefikRouterTLSAnnotation) &&
			strings.EqualFold(strings.TrimSpace(value), "true") {
			return true
		}
	}
	return false
}

// CanonicalResourceURL returns the MCP endpoint URL clients connect to, which
// is also the OAuth resource identifier (RFC 8707/9728) tokens must be issued
// for. It returns "" when the public host is unknown.
func (r *MCPServer) CanonicalResourceURL(options PublicURLOptions) string {
	base := r.PublicBaseURL(options)
	publicPath := strings.TrimSpace(r.EffectivePublicPath())
	if base == "" || publicPath == "" {
		return ""
	}
	return base + cleanURLPath(publicPath)
}

// ProtectedResourceMetadataURL returns the RFC 9728 metadata document URL for
// a resource URL: the well-known prefix inserted between origin and path, the
// location a conforming client derives from the resource it connected to.
func ProtectedResourceMetadataURL(resource string) string {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	resourcePath := strings.TrimRight(parsed.Path, "/")
	metadata := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: oauthProtectedResourcePath + resourcePath}
	return metadata.String()
}

func cleanURLPath(value string) string {
	cleaned := path.Clean("/" + strings.TrimLeft(value, "/"))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}
