package main

import (
	"context"
	"net/http"

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

func (s *apiServer) registryAuthzSettings() *registry.AuthzConfig {
	s.registryAuthzOnce.Do(func() {
		if s.registryAuthz == nil {
			s.registryAuthz = registry.NewAuthzConfigFromEnv()
		}
	})
	return s.registryAuthz
}
