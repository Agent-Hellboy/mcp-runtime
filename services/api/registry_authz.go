package main

import (
	"context"
	"log"
	"net/http"
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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
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
