package main

import (
	"net/http"

	"mcp-sentinel-api/users"
)

func registryCredentialUsername(p principal) string {
	return users.RegistryCredentialUsername(p)
}

func (s *apiServer) handleRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	users.HandleRegistryCredentials(w, r, users.Dependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}

func (s *apiServer) handleRegistryCredentialItem(w http.ResponseWriter, r *http.Request) {
	users.HandleRegistryCredentialItem(w, r, users.Dependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
