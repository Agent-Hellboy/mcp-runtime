package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const registryAuthChallenge = `Basic realm="mcp-runtime-registry"`

type registryCredentialAuthenticator interface {
	AuthenticateRegistryCredential(ctx context.Context, username, password string) (principal, bool, error)
}

func (s *apiServer) handleRegistryAuthz(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.authenticateRegistryRequest(r)
	if err != nil {
		log.Printf("registry auth error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
		return
	}
	if !ok {
		writeRegistryAuthChallenge(w)
		return
	}
	if p.Role != roleAdmin {
		if !principalCanAccessRegistryPath(p, r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *apiServer) authenticateRegistryRequest(r *http.Request) (principal, bool, error) {
	if p, ok, err := s.authenticateRequest(r); err != nil || ok {
		return p, ok, err
	}

	username, password, ok := r.BasicAuth()
	if !ok {
		return principal{}, false, nil
	}
	authn := s.registryCredentialAuthenticator()
	if authn == nil {
		return principal{}, false, nil
	}
	return authn.AuthenticateRegistryCredential(r.Context(), username, password)
}

func (s *apiServer) registryCredentialAuthenticator() registryCredentialAuthenticator {
	if s.registryAuth != nil {
		return s.registryAuth
	}
	if s.platform != nil {
		return s.platform
	}
	return nil
}

func writeRegistryAuthChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", registryAuthChallenge)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func principalCanAccessRegistryPath(p principal, r *http.Request) bool {
	scope, ok := registryPathScope(r)
	if !ok {
		return false
	}
	if scope == "" {
		return true
	}
	return principalCanAccessRegistryScope(p, scope)
}

func registryPathScope(r *http.Request) (string, bool) {
	path := registryForwardedPath(r)
	if path == "" {
		return "", false
	}
	if !strings.HasPrefix(path, "/v2/") {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(path, "/v2/"), "/")
	if rest == "" {
		return "", true
	}
	if strings.HasPrefix(rest, "_catalog") {
		return "", false
	}
	repo := registryRepoFromPath(rest)
	if repo == "" {
		return "", false
	}
	scope, _, _ := strings.Cut(repo, "/")
	scope = strings.TrimSpace(scope)
	return scope, scope != ""
}

func registryForwardedPath(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, key := range []string{"X-Forwarded-Uri", "X-Forwarded-URL"} {
		raw := strings.TrimSpace(r.Header.Get(key))
		if raw == "" {
			continue
		}
		if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
			return parsed.Path
		}
		return raw
	}
	if r.URL != nil {
		return r.URL.Path
	}
	return ""
}

func registryRepoFromPath(rest string) string {
	end := len(rest)
	for _, marker := range []string{"/blobs/", "/manifests/", "/tags/"} {
		if idx := strings.Index(rest, marker); idx >= 0 && idx < end {
			end = idx
		}
	}
	if end == len(rest) {
		return ""
	}
	return strings.Trim(rest[:end], "/")
}

func principalCanAccessRegistryScope(p principal, scope string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	if scope == sharedCatalogNamespace && !registrySharedCatalogWritableForUsers() {
		return false
	}
	if p.HasNamespace(scope) || strings.TrimSpace(p.Subject) == scope {
		return true
	}
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Slug) == scope {
			return true
		}
	}
	return false
}

func registrySharedCatalogWritableForUsers() bool {
	mode := strings.TrimSpace(os.Getenv("PLATFORM_MODE"))
	if mode == "" {
		mode = strings.TrimSpace(os.Getenv("MCP_PLATFORM_MODE"))
	}
	switch strings.ToLower(mode) {
	case "org", "public":
		return true
	default:
		return false
	}
}
