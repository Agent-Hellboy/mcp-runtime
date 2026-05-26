package main

import (
	"net/http"

	"mcp-sentinel-api/users"
)

func (s *apiServer) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	users.HandleAuthMe(w, r, users.Dependencies{
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
	})
}

func (s *apiServer) handleSignup(w http.ResponseWriter, r *http.Request) {
	users.HandleSignup(w, r, users.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
	})
}

func (s *apiServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	users.HandleUsers(w, r, users.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
