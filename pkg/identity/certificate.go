package identity

import (
	"crypto/x509"
	"strings"
)

// CertificateHasURI reports whether cert carries want as a URI SAN.
func CertificateHasURI(cert *x509.Certificate, want string) bool {
	if cert == nil {
		return false
	}
	for _, uri := range cert.URIs {
		if uri != nil && uri.String() == want {
			return true
		}
	}
	return false
}

// FirstClientSPIFFEURI returns the first spiffe:// URI SAN on cert whose trust
// domain matches trustDomain when trustDomain is non-empty. Returns "" when no
// matching URI is present.
func FirstClientSPIFFEURI(cert *x509.Certificate, trustDomain string) string {
	if cert == nil {
		return ""
	}
	trustDomain = strings.TrimSpace(trustDomain)
	for _, uri := range cert.URIs {
		if uri == nil || !strings.EqualFold(uri.Scheme, "spiffe") {
			continue
		}
		if trustDomain != "" && !strings.EqualFold(uri.Host, trustDomain) {
			continue
		}
		return uri.String()
	}
	return ""
}
