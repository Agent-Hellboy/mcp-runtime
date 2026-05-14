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
		http.NotFound(w, r)
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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resource":                 s.publicRequestURL(r, resourcePath),
		"authorization_servers":    []string{strings.TrimSpace(policy.Auth.IssuerURL)},
		"bearer_methods_supported": []string{"header"},
	})
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
	if audience := strings.TrimSpace(policy.Auth.Audience); audience != "" && !serviceutil.AudienceMatches(claims["aud"], audience) {
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
			TeamID:    policypkg.FirstNonEmpty(stringClaim(claims, "team_id"), stringClaim(claims, "tenant_id"), stringClaim(claims, "tid")),
			SessionID: policypkg.FirstNonEmpty(stringClaim(claims, "sid"), headerIdentity.SessionID),
		},
	}
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

	metadata, err := s.fetchAuthServerMetadata(ctx, issuerURL)
	if err != nil {
		return nil, err
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

func (s *gatewayServer) fetchAuthServerMetadata(ctx context.Context, issuerURL string) (*authServerMetadata, error) {
	var lastErr error
	for _, endpoint := range authServerMetadataCandidates(issuerURL) {
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
	if !policypkg.PolicyUsesOAuth(policy) || decision.Status != http.StatusUnauthorized {
		return false
	}
	switch decision.Reason {
	case "missing_bearer_token", "invalid_token":
		return true
	default:
		return false
	}
}

func (s *gatewayServer) oauthAuthenticateHeader(r *http.Request, originalPath, reason string) string {
	values := []string{
		`realm="mcp-runtime"`,
		fmt.Sprintf(`resource_metadata="%s"`, s.publicRequestURL(r, oauthMetadataPath(originalPath))),
	}
	if reason == "invalid_token" {
		values = append(values, `error="invalid_token"`)
	}
	return "Bearer " + strings.Join(values, ", ")
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

	trimmed := strings.TrimRight(issuerURL, "/")
	addCandidate(trimmed + "/.well-known/oauth-authorization-server")
	addCandidate(trimmed + "/.well-known/openid-configuration")

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return candidates
	}
	issuerPath := strings.Trim(parsed.EscapedPath(), "/")
	if issuerPath == "" {
		return candidates
	}
	base := url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	addCandidate(base.String() + "/.well-known/oauth-authorization-server/" + issuerPath)
	addCandidate(base.String() + "/.well-known/openid-configuration/" + issuerPath)
	return candidates
}
