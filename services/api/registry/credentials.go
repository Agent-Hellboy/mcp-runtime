package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mcp-sentinel-api/internal/apiauth"
	"mcp-sentinel-api/internal/platformstore"
	"mcp-sentinel-api/users"
)

type CredentialDependencies struct {
	Platform             *platformstore.Store
	PrincipalFromContext func(context.Context) (apiauth.Principal, bool)
	WriteJSON            func(http.ResponseWriter, int, any)
	WriteBodyDecodeError func(http.ResponseWriter, error)
	RequestIP            func(*http.Request) string
	AuditSource          func(*http.Request, apiauth.Principal) string
	AuditIdentityLabel   func(apiauth.Principal) string
}

func HandleRegistryCredentials(w http.ResponseWriter, r *http.Request, deps CredentialDependencies) {
	if deps.Platform == nil {
		deps.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := deps.PrincipalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		deps.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := deps.Platform.ListRegistryCredentials(r.Context(), p.UserID())
		if err != nil {
			deps.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list registry credentials"})
			return
		}
		deps.WriteJSON(w, http.StatusOK, map[string]any{"credentials": keys})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			deps.WriteBodyDecodeError(w, err)
			return
		}
		key, cleartext, err := deps.Platform.CreateRegistryCredential(r.Context(), p.UserID(), req.Name)
		if err != nil {
			deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_create", Resource: strings.TrimSpace(req.Name), Status: "error", Message: err.Error(), ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
			deps.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_create", Resource: key.ID, Status: "success", ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
		deps.WriteJSON(w, http.StatusCreated, map[string]any{"credential": key, "username": users.RegistryCredentialUsername(p), "password": cleartext})
	default:
		w.Header().Set("allow", "GET, POST")
		deps.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func HandleRegistryCredentialItem(w http.ResponseWriter, r *http.Request, deps CredentialDependencies) {
	if deps.Platform == nil {
		deps.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := deps.PrincipalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		deps.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	credentialID, allowed, valid := parseRegistryCredentialItemPath(r.Method, r.URL.Path)
	if !allowed {
		w.Header().Set("allow", "DELETE, POST")
		deps.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !valid {
		deps.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential path"})
		return
	}
	key, err := deps.Platform.RevokeRegistryCredential(r.Context(), p.UserID(), credentialID)
	if err != nil {
		deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_revoke", Resource: credentialID, Status: "error", Message: err.Error(), ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
		if errors.Is(err, sql.ErrNoRows) {
			deps.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "credential not found"})
			return
		}
		deps.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke credential"})
		return
	}
	deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_revoke", Resource: key.ID, Status: "success", ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
	deps.WriteJSON(w, http.StatusOK, map[string]any{"credential": key})
}

func parseRegistryCredentialItemPath(method, path string) (credentialID string, allowed bool, valid bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/user/registry-credentials/"), "/"), "/")
	switch method {
	case http.MethodDelete:
		if len(parts) != 1 || parts[0] == "" {
			return "", true, false
		}
		return parts[0], true, true
	case http.MethodPost:
		if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" {
			return "", true, false
		}
		return parts[0], true, true
	default:
		return "", false, false
	}
}
