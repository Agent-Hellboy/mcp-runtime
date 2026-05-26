package main

import (
	"net/http"

	"mcp-sentinel-api/users"
)

func (s *apiServer) handleUserImagePublishActivity(w http.ResponseWriter, r *http.Request) {
	users.HandleUserImagePublishActivity(w, r, users.Dependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
