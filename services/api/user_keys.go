package main

import (
	"net/http"

	"mcp-sentinel-api/identity"
)

func (s *apiServer) handleUserAPIKeys(w http.ResponseWriter, r *http.Request) {
	identity.HandleUserAPIKeys(w, r, identity.Dependencies{
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
	identity.HandleUserAPIKeyItem(w, r, identity.Dependencies{
		Platform:             s.platform,
		UserKeys:             s.userKeys,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
