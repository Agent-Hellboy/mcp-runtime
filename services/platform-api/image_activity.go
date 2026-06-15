package main

import (
	"net/http"

	"mcp-platform-api/identity"
)

func (s *apiServer) handleUserImagePublishActivity(w http.ResponseWriter, r *http.Request) {
	identity.HandleUserImagePublishActivity(w, r, identity.Dependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
