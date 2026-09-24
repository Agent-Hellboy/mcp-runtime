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
	resourcePath := strings.TrimRight(parsed.Path, "/")
	metadata := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/.well-known/oauth-protected-resource" + resourcePath}
	return metadata.String()
}
