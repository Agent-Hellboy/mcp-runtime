package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"mcp-runtime/pkg/publishscope"
	"mcp-sentinel-api/registry"
)

const registryAuthChallenge = `Basic realm="mcp-runtime-registry"`

type registryCredentialAuthenticator interface {
	AuthenticateRegistryCredential(ctx context.Context, username, password string) (principal, bool, error)
}

func (s *apiServer) handleRegistryAuthz(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.authenticateRegistryRequest(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
		return
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", registryAuthChallenge)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if p.Role != roleAdmin {
		if !s.principalCanAccessRegistryPath(p, r) {
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
	if password != "" {
		clone := r.Clone(r.Context())
		clone.Header.Set("x-api-key", password)
		if p, ok, err := s.authenticateRequest(clone); err == nil && ok {
			_ = username
			return p, ok, nil
		}
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

func (s *apiServer) principalCanAccessRegistryPath(p principal, r *http.Request) bool {
	scope, ok := registry.RegistryPathScope(r)
	if !ok {
		return false
	}
	if scope == "" {
		return true
	}
	return s.principalCanAccessRegistryScope(p, scope)
}

func (s *apiServer) principalCanAccessRegistryScope(p principal, scope string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	cfg := s.registryAuthzSettings()
	if cfg.catalogScopeWritable(scope) {
		return true
	}
	if scope == sharedCatalogNamespace && !cfg.sharedCatalogWritableForUsers() {
		return false
	}
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Slug) == scope || strings.TrimSpace(team.Namespace) == scope {
			return true
		}
	}
	return false
}

type registryAuthzConfig struct {
	mode              string
	catalogNamespaces map[string]struct{}
}

func (s *apiServer) registryAuthzSettings() *registryAuthzConfig {
	s.registryAuthzOnce.Do(func() {
		if s.registryAuthz == nil {
			s.registryAuthz = newRegistryAuthzConfigFromEnv()
		}
	})
	return s.registryAuthz
}

func newRegistryAuthzConfigFromEnv() *registryAuthzConfig {
	mode := registryPlatformModeFromEnv()
	catalogNamespaces := make(map[string]struct{})
	for _, namespace := range registryModeCatalogNamespacesFromEnv(mode) {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			catalogNamespaces[namespace] = struct{}{}
		}
	}
	return &registryAuthzConfig{
		mode:              mode,
		catalogNamespaces: catalogNamespaces,
	}
}

func registryPlatformModeFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("PLATFORM_MODE"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_MODE"))
	}
	switch strings.ToLower(raw) {
	case string(publishscope.Org):
		return string(publishscope.Org)
	case string(publishscope.Public):
		return string(publishscope.Public)
	case "", "tenant":
		return "tenant"
	default:
		return "tenant"
	}
}

func registryModeCatalogNamespacesFromEnv(mode string) []string {
	switch mode {
	case "tenant":
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACES"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACES"))
	}
	if raw == "" && mode == string(publishscope.Public) {
		raw = strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACES"))
		if raw == "" {
			raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_PUBLIC_NAMESPACES"))
		}
	}
	namespaces := []string{registryDefaultModeCatalogNamespace(mode)}
	for _, namespace := range strings.Split(raw, ",") {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	return dedupeNonEmptyStrings(namespaces)
}

func registryDefaultModeCatalogNamespace(mode string) string {
	if mode == "tenant" {
		return ""
	}
	if override := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	if override := strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	switch mode {
	case string(publishscope.Org):
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_ORG_NAMESPACE")); namespace != "" {
			return namespace
		}
		return publishscope.DefaultOrgCatalogNamespace
	case string(publishscope.Public):
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACE")); namespace != "" {
			return namespace
		}
		return publishscope.DefaultPublicCatalogNamespace
	default:
		return ""
	}
}

func (cfg *registryAuthzConfig) catalogScopeWritable(scope string) bool {
	if cfg == nil {
		return false
	}
	scope = strings.TrimSpace(scope)
	switch cfg.mode {
	case string(publishscope.Public):
		if scope == publishscope.PublicRegistryAlias {
			return true
		}
	case string(publishscope.Org):
		if scope == publishscope.OrgRegistryAlias {
			return true
		}
	default:
		return false
	}
	_, ok := cfg.catalogNamespaces[scope]
	return ok
}

func (cfg *registryAuthzConfig) sharedCatalogWritableForUsers() bool {
	if cfg == nil {
		return false
	}
	switch cfg.mode {
	case string(publishscope.Public), string(publishscope.Org):
		return true
	default:
		return false
	}
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
