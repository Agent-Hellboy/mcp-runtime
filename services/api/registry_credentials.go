package main

import (
	"net/http"

	"mcp-sentinel-api/registry"
	"mcp-sentinel-api/users"
)

func registryCredentialUsername(p principal) string {
	return users.RegistryCredentialUsername(p)
}

func (s *apiServer) handleRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	registry.HandleRegistryCredentials(w, r, registry.CredentialDependencies{
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
	registry.HandleRegistryCredentialItem(w, r, registry.CredentialDependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
