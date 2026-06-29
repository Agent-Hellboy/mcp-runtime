// Package identity provides shared SPIFFE session identity parsing and formatting
// used by the gateway, platform API, and adapter clients.
package identity

import (
	"fmt"
	"net/url"
	"strings"
)

// SessionSPIFFEID returns the canonical session-bound SPIFFE URI:
// spiffe://<trustDomain>/ns/<namespace>/session/<session>.
func SessionSPIFFEID(trustDomain, namespace, session string) string {
	trustDomain = strings.TrimSpace(trustDomain)
	namespace = strings.TrimSpace(namespace)
	session = strings.TrimSpace(session)
	return fmt.Sprintf("spiffe://%s/ns/%s/session/%s", trustDomain, url.PathEscape(namespace), url.PathEscape(session))
}

// ParseSessionSPIFFE parses a session-bound SPIFFE ID of the form
// spiffe://<trustDomain>/ns/<namespace>/session/<sessionID>. It is deliberately
// strict and fails closed on any deviation (wrong scheme, trust domain, or path).
func ParseSessionSPIFFE(raw, trustDomain string) (namespace, sessionID string, ok bool) {
	uri, err := url.Parse(raw)
	if err != nil || uri == nil {
		return "", "", false
	}
	if !strings.EqualFold(uri.Scheme, "spiffe") || !strings.EqualFold(uri.Host, strings.TrimSpace(trustDomain)) {
		return "", "", false
	}
	// SPIFFE IDs must not carry query parameters or fragments; reject them to
	// avoid parameter-injection bypasses.
	if uri.RawQuery != "" || uri.Fragment != "" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(uri.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "ns" || parts[2] != "session" {
		return "", "", false
	}
	namespace, err = url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(namespace) == "" {
		return "", "", false
	}
	sessionID, err = url.PathUnescape(parts[3])
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return "", "", false
	}
	return namespace, sessionID, true
}
