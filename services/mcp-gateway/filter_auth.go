package main

import policypkg "mcp-runtime/pkg/policy"

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
