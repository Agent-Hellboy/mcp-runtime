package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"

	policypkg "mcp-runtime/pkg/policy"
	"mcp-runtime/pkg/serviceutil"
)

func (s *gatewayServer) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request, policy *policypkg.Document) bool {
	if !isOAuthProtectedMetadataPath(r.URL.Path) {
		return false
	}
	if !policypkg.PolicyUsesOAuth(policy) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":            "oauth_not_enabled",
				"message":          "This MCP server uses MCP Runtime header/session governance. Connect through the mcp-runtime adapter proxy or stdio adapter instead of OAuth discovery.",
				"adapter_required": true,
			})
		}
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}

	resourcePath := oauthResourcePath(r.URL.Path)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return true
	}
	payload := map[string]any{
		"resource":                 s.publicRequestURL(r, resourcePath),
		"authorization_servers":    []string{strings.TrimSpace(policy.Auth.IssuerURL)},
		"bearer_methods_supported": []string{"header"},
	}
	_ = json.NewEncoder(w).Encode(payload)
	return true
}

func (s *gatewayServer) authenticateOAuth(r *http.Request, policy *policypkg.Document) oauthAuthResult {
	headerIdentity := s.extractIdentity(r, policy)
	result := oauthAuthResult{
		Allowed:  true,
		Status:   http.StatusOK,
		Identity: identityContext{SessionID: headerIdentity.SessionID},
	}
	if !policypkg.PolicyUsesOAuth(policy) {
		result.Identity = headerIdentity
		return result
	}

	if policy.Auth == nil {
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_config_missing",
			Identity: result.Identity,
		}
	}

	issuerURL := strings.TrimSpace(policy.Auth.IssuerURL)
	if issuerURL == "" {
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_issuer_missing",
			Identity: result.Identity,
		}
	}

	tokenHeader := oauthTokenHeader(policy)
	token := extractToken(tokenHeader, r.Header.Get(tokenHeader))
	if token == "" {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "missing_bearer_token",
			Identity: result.Identity,
		}
	}

	provider, err := s.oauthProviderForIssuer(r.Context(), issuerURL)
	if err != nil {
		log.Printf("oauth provider lookup failed for %s: %v", issuerURL, err)
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_provider_unavailable",
			Identity: result.Identity,
		}
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}))
	parsed, err := parser.ParseWithClaims(token, claims, provider.jwks.Keyfunc)
	if err != nil || !parsed.Valid {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}
	if !claims.VerifyIssuer(issuerURL, true) {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}
	audience := strings.TrimSpace(policy.Auth.Audience)
	if audience == "" {
		var ok bool
		audience, ok = s.canonicalOAuthResource(r)
		if !ok {
			return oauthAuthResult{
				Status:   http.StatusServiceUnavailable,
				Reason:   "oauth_resource_unavailable",
				Identity: result.Identity,
			}
		}
	}
	if !serviceutil.AudienceMatches(claims["aud"], audience) {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}

	return oauthAuthResult{
		Allowed: true,
		Status:  http.StatusOK,
		Token:   token,
		Identity: identityContext{
			HumanID:   stringClaim(claims, "sub"),
			AgentID:   policypkg.FirstNonEmpty(stringClaim(claims, "azp"), stringClaim(claims, "client_id")),
			TeamID:    oauthTeamID(claims, policy),
			SessionID: policypkg.FirstNonEmpty(stringClaim(claims, "sid"), headerIdentity.SessionID),
		},
	}
}

func oauthTeamID(claims jwt.MapClaims, policy *policypkg.Document) string {
	if teamID := policypkg.FirstNonEmpty(stringClaim(claims, "team_id"), stringClaim(claims, "tenant_id"), stringClaim(claims, "tid")); teamID != "" {
		return teamID
	}
	teamIDs := stringClaims(claims, "team_ids")
	if policyTeamID := policypkg.PolicyServerTeamID(policy); policyTeamID != "" {
		for _, teamID := range teamIDs {
			if teamID == policyTeamID {
				return teamID
			}
		}
	}
	if len(teamIDs) == 1 {
		return teamIDs[0]
	}
	return ""
}

func stringClaims(claims jwt.MapClaims, name string) []string {
	value, ok := claims[name]
	if !ok {
		return nil
	}
	values := make([]string, 0)
	switch typed := value.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	}
	return values
}

func (s *gatewayServer) oauthProviderForIssuer(ctx context.Context, issuerURL string) (*oauthProvider, error) {
	issuerURL = strings.TrimSpace(issuerURL)
	if issuerURL == "" {
		return nil, errors.New("issuer URL is required")
	}

	s.oauthMu.Lock()
	provider, ok := s.oauthProviders[issuerURL]
	s.oauthMu.Unlock()
	if ok {
		return provider, nil
	}

	metadata, usingInternalIssuer, err := s.fetchAuthServerMetadataWithFallback(ctx, issuerURL)
	if err != nil {
		return nil, err
	}
	if usingInternalIssuer {
		metadata.JWKSURI, err = rewriteOAuthEndpoint(metadata.JWKSURI, metadata.Issuer, s.oauthInternalIssuerURL)
		if err != nil {
			return nil, err
		}
	}
	jwks, err := keyfunc.Get(metadata.JWKSURI, keyfunc.Options{RefreshInterval: 10 * time.Minute})
	if err != nil {
		return nil, err
	}

	provider = &oauthProvider{jwks: jwks}
	s.oauthMu.Lock()
	if existing, ok := s.oauthProviders[issuerURL]; ok {
		s.oauthMu.Unlock()
		return existing, nil
	}
	s.oauthProviders[issuerURL] = provider
	s.oauthMu.Unlock()
	return provider, nil
}

// fetchAuthServerMetadataWithFallback prefers the in-cluster first-party issuer
// for local reachability, but still supports MCP servers configured with an
// external or test authorization server. The metadata issuer is validated
// against the configured issuer on every attempt.
func (s *gatewayServer) fetchAuthServerMetadataWithFallback(ctx context.Context, issuerURL string) (*authServerMetadata, bool, error) {
	internalIssuerURL := strings.TrimSpace(s.oauthInternalIssuerURL)
	if internalIssuerURL == "" || internalIssuerURL == issuerURL {
		metadata, err := s.fetchAuthServerMetadataForIssuer(ctx, issuerURL, issuerURL)
		return metadata, false, err
	}

	if metadata, err := s.fetchAuthServerMetadataForIssuer(ctx, internalIssuerURL, issuerURL); err == nil {
		return metadata, true, nil
	}

	// The internal issuer is only a transport optimization. It must not prevent
	// external providers, including the E2E mock provider, from being used.
	metadata, err := s.fetchAuthServerMetadataForIssuer(ctx, issuerURL, issuerURL)
	return metadata, false, err
}

func (s *gatewayServer) fetchAuthServerMetadata(ctx context.Context, issuerURL string) (*authServerMetadata, error) {
	return s.fetchAuthServerMetadataForIssuer(ctx, issuerURL, issuerURL)
}

func (s *gatewayServer) fetchAuthServerMetadataForIssuer(ctx context.Context, lookupIssuerURL, expectedIssuerURL string) (*authServerMetadata, error) {
	var lastErr error
	for _, endpoint := range authServerMetadataCandidates(lookupIssuerURL) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s returned status %d", endpoint, resp.StatusCode)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		var metadata authServerMetadata
		if err := json.Unmarshal(body, &metadata); err != nil {
			lastErr = err
			continue
		}
		if strings.TrimSpace(metadata.Issuer) != expectedIssuerURL {
			lastErr = fmt.Errorf("%s issuer %q does not match configured issuer %q", endpoint, metadata.Issuer, expectedIssuerURL)
			continue
		}
		if strings.TrimSpace(metadata.JWKSURI) == "" {
			lastErr = fmt.Errorf("%s missing jwks_uri", endpoint)
			continue
		}
		return &metadata, nil
	}
	if lastErr == nil {
		lastErr = errors.New("authorization server metadata lookup failed")
	}
	return nil, lastErr
}

func rewriteOAuthEndpoint(endpoint, publicIssuer, internalIssuer string) (string, error) {
	publicBase := strings.TrimRight(strings.TrimSpace(publicIssuer), "/")
	internalBase := strings.TrimRight(strings.TrimSpace(internalIssuer), "/")
	if publicBase == "" || internalBase == "" || !strings.HasPrefix(endpoint, publicBase+"/") {
		return "", fmt.Errorf("OAuth endpoint %q is not under issuer %q", endpoint, publicIssuer)
	}
	return internalBase + strings.TrimPrefix(endpoint, publicBase), nil
}

func (s *gatewayServer) applyIdentityHeaders(r *http.Request, policy *policypkg.Document, identity identityContext) {
	humanHeader, agentHeader, teamHeader, sessionHeader := s.identityHeaderNames(policy)
	if humanHeader != "" {
		r.Header.Del(humanHeader)
		if identity.HumanID != "" {
			r.Header.Set(humanHeader, identity.HumanID)
		}
	}
	if agentHeader != "" {
		r.Header.Del(agentHeader)
		if identity.AgentID != "" {
			r.Header.Set(agentHeader, identity.AgentID)
		}
	}
	if teamHeader != "" {
		r.Header.Del(teamHeader)
		if identity.TeamID != "" {
			r.Header.Set(teamHeader, identity.TeamID)
		}
	}
	if sessionHeader != "" {
		r.Header.Del(sessionHeader)
		if identity.SessionID != "" {
			r.Header.Set(sessionHeader, identity.SessionID)
		}
	}
}

func (s *gatewayServer) applyUpstreamToken(r *http.Request, policy *policypkg.Document, token string) {
	if policypkg.PolicyUsesOAuth(policy) {
		// The token authenticated to this MCP resource is never an upstream API
		// credential. MCP's authorization spec explicitly forbids a resource
		// server from accepting or transiting unrelated bearer tokens.
		r.Header.Del(defaultTokenHeader)
		r.Header.Del(oauthTokenHeader(policy))
		if policy.Session != nil {
			r.Header.Del(strings.TrimSpace(policy.Session.UpstreamTokenHeader))
		}
		return
	}
	if policy == nil || policy.Session == nil {
		return
	}
	headerName := strings.TrimSpace(policy.Session.UpstreamTokenHeader)
	if headerName == "" {
		return
	}
	r.Header.Del(headerName)
	if token == "" {
		return
	}
	r.Header.Set(headerName, serviceutil.FormatTokenHeaderValue(headerName, token))
}

func (s *gatewayServer) identityHeaderNames(policy *policypkg.Document) (string, string, string, string) {
	humanHeader := s.defaultHumanHeader
	agentHeader := s.defaultAgentHeader
	teamHeader := s.defaultTeamHeader
	sessionHeader := s.defaultSessionHeader
	if policy != nil && policy.Auth != nil {
		if policy.Auth.HumanIDHeader != "" {
			humanHeader = policy.Auth.HumanIDHeader
		}
		if policy.Auth.AgentIDHeader != "" {
			agentHeader = policy.Auth.AgentIDHeader
		}
		if policy.Auth.TeamIDHeader != "" {
			teamHeader = policy.Auth.TeamIDHeader
		}
		if policy.Auth.SessionIDHeader != "" {
			sessionHeader = policy.Auth.SessionIDHeader
		}
	}
	return humanHeader, agentHeader, teamHeader, sessionHeader
}

func isOAuthProtectedMetadataPath(value string) bool {
	return value == oauthProtectedPrefix || strings.HasPrefix(value, oauthProtectedPrefix+"/")
}

func oauthResourcePath(value string) string {
	if !isOAuthProtectedMetadataPath(value) {
		return "/"
	}
	suffix := strings.TrimPrefix(value, oauthProtectedPrefix)
	if suffix == "" {
		return "/"
	}
	return normalizeURLPath(suffix)
}

func oauthMetadataPath(value string) string {
	value = normalizeURLPath(value)
	if value == "/" {
		return oauthProtectedPrefix
	}
	return oauthProtectedPrefix + value
}

func oauthTokenHeader(policy *policypkg.Document) string {
	if policy != nil && policy.Auth != nil && strings.TrimSpace(policy.Auth.TokenHeader) != "" {
		return strings.TrimSpace(policy.Auth.TokenHeader)
	}
	return defaultTokenHeader
}

func shouldChallengeOAuth(policy *policypkg.Document, decision policypkg.Decision) bool {
	if !policypkg.PolicyUsesOAuth(policy) {
		return false
	}
	if decision.Status == http.StatusForbidden {
		return false
	}
	if decision.Status != http.StatusUnauthorized {
		return false
	}
	switch decision.Reason {
	case "missing_bearer_token", "invalid_token":
		return true
	default:
		return false
	}
}

func (s *gatewayServer) oauthAuthenticateHeader(r *http.Request, originalPath, reason, toolName string, decision policypkg.Decision) string {
	values := []string{
		`realm="mcp-runtime"`,
		fmt.Sprintf(`resource_metadata="%s"`, s.publicRequestURL(r, oauthMetadataPath(originalPath))),
	}
	if reason == "invalid_token" {
		values = append(values, `error="invalid_token"`)
	}
	if decision.Status == http.StatusForbidden {
		values = append(values, `error="insufficient_scope"`)
		if scopes := oauthRequiredScopes(decision, toolName); len(scopes) > 0 {
			values = append(values, fmt.Sprintf(`scope="%s"`, strings.Join(scopes, " ")))
		}
		values = append(values, fmt.Sprintf(`error_description="%s"`, oauthScopeDescription(decision.Reason)))
	}
	return "Bearer " + strings.Join(values, ", ")
}

func (s *gatewayServer) canonicalOAuthResource(r *http.Request) (string, bool) {
	if r == nil || r.URL == nil {
		return "", false
	}
	resource := s.publicRequestURL(r, normalizeURLPath(r.URL.Path))
	parsed, err := url.Parse(resource)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	parsed.RawPath = ""
	return parsed.String(), true
}

func oauthRequiredScopes(decision policypkg.Decision, toolName string) []string {
	switch decision.Reason {
	case "tool_denied", "tool_not_granted":
		if toolName != "" {
			return []string{"mcp:tool:" + oauthScopeToken(toolName)}
		}
	case "trust_too_low":
		if decision.RequiredTrust != "" {
			return []string{"mcp:trust:" + oauthScopeToken(decision.RequiredTrust)}
		}
	case "side_effect_not_allowed":
		if decision.RequiredSideEffect != "" {
			return []string{"mcp:side-effect:" + oauthScopeToken(decision.RequiredSideEffect)}
		}
	case "no_matching_grant", "grant_without_trust":
		return []string{"mcp:access"}
	}
	return nil
}

func oauthScopeDescription(reason string) string {
	switch reason {
	case "tool_denied", "tool_not_granted":
		return "Tool permission required for this operation"
	case "trust_too_low":
		return "Higher trust permission required for this operation"
	case "side_effect_not_allowed":
		return "Side-effect permission required for this operation"
	default:
		return "Permission required for this operation"
	}
}

func oauthScopeToken(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("!#$%&'()*+-.^_`|~:", r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func stringClaim(claims jwt.MapClaims, key string) string {
	if raw, ok := claims[key]; ok {
		if value, ok := raw.(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractToken(headerName, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(headerName), "authorization") {
		return serviceutil.ExtractBearer(value)
	}
	if token := serviceutil.ExtractBearer(value); token != "" {
		return token
	}
	return value
}

func authServerMetadataCandidates(issuerURL string) []string {
	issuerURL = strings.TrimSpace(issuerURL)
	if issuerURL == "" {
		return nil
	}

	var candidates []string
	addCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	parsed, err := url.Parse(issuerURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	issuerPath := strings.Trim(parsed.EscapedPath(), "/")
	base := url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	if issuerPath == "" {
		addCandidate(base.String() + "/.well-known/oauth-authorization-server")
		addCandidate(base.String() + "/.well-known/openid-configuration")
		return candidates
	}
	// RFC 8414 path insertion is required first for issuers with a path,
	// followed by OIDC path insertion, then OIDC path appending. The order is
	// observable with providers that expose more than one discovery document.
	addCandidate(base.String() + "/.well-known/oauth-authorization-server/" + issuerPath)
	addCandidate(base.String() + "/.well-known/openid-configuration/" + issuerPath)
	trimmed := strings.TrimRight(parsed.String(), "/")
	addCandidate(trimmed + "/.well-known/openid-configuration")
	return candidates
}
