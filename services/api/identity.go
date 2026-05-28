package main

import (
	"net/http"

	"mcp-sentinel-api/identity"
)

func (s *apiServer) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	identity.HandleAuthMe(w, r, identity.Dependencies{
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
	})
}

func (s *apiServer) handleSignup(w http.ResponseWriter, r *http.Request) {
	identity.HandleSignup(w, r, identity.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
	})
}

func (s *apiServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	identity.HandleUsers(w, r, identity.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
