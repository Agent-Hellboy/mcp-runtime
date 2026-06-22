package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	policypkg "mcp-runtime/pkg/policy"
)

// authFilter is stage 3 of the gateway pipeline. It extracts the caller
// identity and, when the policy uses OAuth, validates the bearer JWT.
//
// For header-mode policies, identity is read directly from the governance
// headers; no further validation is performed here — authzFilter (stage 4)
// enforces grant and session policy.
//
// For OAuth policies, the JWT is verified against the issuer's JWKS. On any
// authentication failure, authFilter writes a denial response and returns
// Reject; it never falls through to stage 4 with an unauthenticated identity.
//
// authFilter always runs after policyFilter (stage 2) has set Exchange.Policy
// and always completes before authzFilter (stage 4) reads Exchange.Identity.
func (s *gatewayServer) authFilter(ex *Exchange) Result {
	if ex.Policy != nil && ex.Policy.Auth != nil && strings.EqualFold(ex.Policy.Auth.Mode, "mtls") {
		identity, reason := authenticateMTLS(ex.R, ex.Policy)
		ex.Identity = identity
		if reason == "" {
			return Continue
		}
		ex.Decision = policypkg.Deny(
			http.StatusUnauthorized,
			reason,
			policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(ex.Policy), s.defaultPolicyVersion),
		)
		s.writeDeniedResponse(ex)
		return Reject
	}

	// Extract identity from governance headers; for OAuth this populates at
	// least the session header before JWT validation overwrites the rest.
	ex.Identity = s.extractIdentity(ex.R, ex.Policy)

	if !policypkg.PolicyUsesOAuth(ex.Policy) {
		return Continue
	}

	oauthResult := s.authenticateOAuth(ex.R, ex.Policy)
	// OAuth result replaces the header-extracted identity; the session header
	// value from header extraction is merged inside authenticateOAuth.
	ex.Identity = oauthResult.Identity
	ex.OAuthToken = oauthResult.Token

	if !oauthResult.Allowed {
		ex.Decision = policypkg.Deny(
			oauthResult.Status,
			oauthResult.Reason,
			policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(ex.Policy), s.defaultPolicyVersion),
		)
		s.writeDeniedResponse(ex)
		return Reject
	}
	return Continue
}

// authenticateMTLS maps a verified SPIFFE URI SAN to the rendered session
// binding. Header identity is deliberately ignored in this mode.
func authenticateMTLS(r *http.Request, policy *policypkg.Document) (identityContext, string) {
	if r == nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return identityContext{}, "missing_client_certificate"
	}

	trustDomain := strings.TrimSpace(policy.Auth.TrustDomain)
	var matched *url.URL
	for _, uri := range r.TLS.PeerCertificates[0].URIs {
		if uri == nil || !strings.EqualFold(uri.Scheme, "spiffe") || !strings.EqualFold(uri.Host, trustDomain) {
			continue
		}
		if matched != nil {
			return identityContext{}, "ambiguous_spiffe_identity"
		}
		matched = uri
	}
	if matched == nil {
		return identityContext{}, "invalid_spiffe_identity"
	}

	parts := strings.Split(strings.Trim(matched.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "ns" || parts[2] != "session" {
		return identityContext{}, "invalid_spiffe_identity"
	}
	namespace, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(namespace) == "" {
		return identityContext{}, "invalid_spiffe_identity"
	}
	sessionID, err := url.PathUnescape(parts[3])
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return identityContext{}, "invalid_spiffe_identity"
	}

	for _, binding := range policy.Sessions {
		if string(binding.Namespace) != namespace || string(binding.Name) != sessionID {
			continue
		}
		if binding.Revoked {
			return identityContext{}, "session_revoked"
		}
		if mtlsBindingExpired(binding.ExpiresAt) {
			return identityContext{}, "session_expired"
		}
		return identityContext{
			HumanID:   string(binding.HumanID),
			AgentID:   string(binding.AgentID),
			TeamID:    string(binding.TeamID),
			SessionID: string(binding.Name),
		}, ""
	}
	return identityContext{}, "session_not_found"
}

func mtlsBindingExpired(expiresAt string) bool {
	trimmed := strings.TrimSpace(expiresAt)
	if trimmed == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	return err == nil && !parsed.After(time.Now())
}
