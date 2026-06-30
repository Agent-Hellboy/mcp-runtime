package main

import (
	"net/http"

	"mcp-platform-api/identity"
)

func (pr platformRoutes) handleUserImagePublishActivity(w http.ResponseWriter, r *http.Request) {
	identity.HandleUserImagePublishActivity(w, r, identity.Dependencies{
		Platform:             pr.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
