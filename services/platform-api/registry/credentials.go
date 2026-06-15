package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mcp-platform-api/identity"
	"mcp-platform-api/internal/apiauth"
	"mcp-platform-api/internal/httperrors"
	"mcp-platform-api/internal/platformstore"
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
		httperrors.PlatformUnavailable(w)
		return
	}
	p, ok := deps.PrincipalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		httperrors.Unauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := deps.Platform.ListRegistryCredentials(r.Context(), p.UserID())
		if err != nil {
			httperrors.QueryFailed(w, "failed to list registry credentials")
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
			httperrors.BadRequest(w, err.Error())
			return
		}
		deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_create", Resource: key.ID, Status: "success", ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
		deps.WriteJSON(w, http.StatusCreated, map[string]any{"credential": key, "username": identity.RegistryCredentialUsername(p), "password": cleartext})
	default:
		httperrors.MethodNotAllowed(w, "GET, POST")
	}
}

func HandleRegistryCredentialItem(w http.ResponseWriter, r *http.Request, deps CredentialDependencies) {
	if deps.Platform == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	p, ok := deps.PrincipalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		httperrors.Unauthorized(w)
		return
	}
	credentialID, allowed, valid := parseRegistryCredentialItemPath(r.Method, r.URL.Path)
	if !allowed {
		httperrors.MethodNotAllowed(w, "DELETE, POST")
		return
	}
	if !valid {
		httperrors.BadRequest(w, "invalid credential path")
		return
	}
	key, err := deps.Platform.RevokeRegistryCredential(r.Context(), p.UserID(), credentialID)
	if err != nil {
		deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_revoke", Resource: credentialID, Status: "error", Message: err.Error(), ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
		if errors.Is(err, sql.ErrNoRows) {
			httperrors.NotFound(w, "credential not found")
			return
		}
		httperrors.QueryFailed(w, "failed to revoke credential")
		return
	}
	deps.Platform.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: p.UserID(), Action: "registry_credential_revoke", Resource: key.ID, Status: "success", ActorIP: deps.RequestIP(r), Source: deps.AuditSource(r, p), AuthIdentity: deps.AuditIdentityLabel(p)})
	deps.WriteJSON(w, http.StatusOK, map[string]any{"credential": key})
}

func parseRegistryCredentialItemPath(method, path string) (credentialID string, allowed bool, valid bool) {
	const prefix = "/api/v1/user/registry-credentials/"
	if strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	} else {
		path = strings.TrimPrefix(path, "/api/user/registry-credentials/")
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
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
