// Package oauthresource contains dependency-light OAuth resource URL helpers
// shared by the API and runtime services.
package oauthresource

import (
	"net/url"
	"strings"
)

// ProtectedResourceMetadataURL returns the RFC 9728 metadata document URL for
// a resource URL by inserting the well-known path between its origin and path.
func ProtectedResourceMetadataURL(resource string) string {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	metadata := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: ProtectedResourceMetadataPath(parsed.Path)}
	return metadata.String()
}

// ProtectedResourceMetadataPath returns the metadata document path for a
// resource path, trimming a trailing slash the same way the URL form does so
// ingress routes and advertised URLs always agree.
func ProtectedResourceMetadataPath(resourcePath string) string {
	resourcePath = strings.TrimRight(strings.TrimSpace(resourcePath), "/")
	if resourcePath != "" && !strings.HasPrefix(resourcePath, "/") {
		resourcePath = "/" + resourcePath
	}
	return "/.well-known/oauth-protected-resource" + resourcePath
}
