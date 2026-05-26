package main

import (
	"net/http"

	"mcp-sentinel-api/users"
)

func (s *apiServer) handleUserAPIKeys(w http.ResponseWriter, r *http.Request) {
	users.HandleUserAPIKeys(w, r, users.Dependencies{
		Platform:             s.platform,
		UserKeys:             s.userKeys,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}

func (s *apiServer) handleUserAPIKeyItem(w http.ResponseWriter, r *http.Request) {
	users.HandleUserAPIKeyItem(w, r, users.Dependencies{
		Platform:             s.platform,
		UserKeys:             s.userKeys,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
