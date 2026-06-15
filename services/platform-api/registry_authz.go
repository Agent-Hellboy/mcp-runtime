package main

import (
	"context"
	"net/http"
	"strings"

	"mcp-platform-api/internal/apiauth"
	"mcp-platform-api/registry"
)

type registryCredentialAuthenticator interface {
	AuthenticateRegistryCredential(ctx context.Context, username, password string) (principal, bool, error)
}

func (s *apiServer) handleRegistryAuthz(w http.ResponseWriter, r *http.Request) {
	registry.HandleAuthz(w, r, s.registryAuthzDependencies())
}

func (s *apiServer) registryAuthzDependencies() registry.Dependencies {
	cfg := s.registryAuthzSettings()
	return registry.Dependencies{
		AuthenticateRequest:             s.authenticateRegistryRequest,
		RegistryCredentialAuthenticator: s.registryCredentialAuthenticator(),
		RegistryAuthzSettings:           func() *registry.AuthzConfig { return cfg },
		PrincipalCanAccessRegistryPath: func(p apiauth.Principal, r *http.Request) bool {
			return registry.PrincipalCanAccessRegistryPath(p, r, cfg)
		},
	}
}

func (s *apiServer) authenticateRegistryRequest(r *http.Request) (principal, bool, error) {
	if p, ok, err := s.authenticateRequest(r); err != nil || ok {
		if ok {
			p = s.enrichRegistryPrincipal(r.Context(), p)
		}
		return p, ok, err
	}

	username, password, ok := r.BasicAuth()
	if !ok {
		return principal{}, false, nil
	}
	// Registry-issued Docker credentials use the mcpr_ prefix. Route them through
	// AuthenticateRegistryCredential so username matching and team membership are
	// resolved from Postgres instead of the generic user API key shortcut below.
	if password != "" && !strings.HasPrefix(password, "mcpr_") {
		clone := r.Clone(r.Context())
		clone.Header.Set("x-api-key", password)
		if p, ok, err := s.authenticateRequest(clone); err == nil && ok {
			_ = username
			return s.enrichRegistryPrincipal(r.Context(), p), true, nil
		}
	}
	authn := s.registryCredentialAuthenticator()
	if authn == nil {
		return principal{}, false, nil
	}
	p, ok, err := authn.AuthenticateRegistryCredential(r.Context(), username, password)
	if !ok || err != nil {
		return principal(p), ok, err
	}
	return s.enrichRegistryPrincipal(r.Context(), principal(p)), true, nil
}

func (s *apiServer) enrichRegistryPrincipal(ctx context.Context, p principal) principal {
	if s.platform == nil || p.IsService || p.Role == roleAdmin {
		return p
	}
	userID := strings.TrimSpace(p.Subject)
	if userID == "" {
		return p
	}
	enriched, err := s.platform.PrincipalForUserID(ctx, userID)
	if err != nil {
		return p
	}
	enriched.AuthType = p.AuthType
	enriched.APIKeyID = p.APIKeyID
	enriched.IsService = p.IsService
	return principal(enriched)
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

func (s *apiServer) registryAuthzSettings() *registry.AuthzConfig {
	s.registryAuthzOnce.Do(func() {
		if s.registryAuthz == nil {
			s.registryAuthz = registry.NewAuthzConfigFromEnv()
		}
	})
	return s.registryAuthz
}
