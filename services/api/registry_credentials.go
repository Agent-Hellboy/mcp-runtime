package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *apiServer) handleRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok || p.userID() == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.platform.ListRegistryCredentials(r.Context(), p.userID())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list registry credentials"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"credentials": keys})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBodyDecodeError(w, err)
			return
		}
		key, cleartext, err := s.platform.CreateRegistryCredential(r.Context(), p.userID(), req.Name)
		if err != nil {
			s.platform.WriteAudit(r.Context(), auditEvent{
				UserID:   p.userID(),
				Action:   "registry_credential_create",
				Resource: strings.TrimSpace(req.Name),
				Status:   "error",
				Message:  err.Error(),
				ActorIP:  requestIP(r),
			})
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.platform.WriteAudit(r.Context(), auditEvent{
			UserID:   p.userID(),
			Action:   "registry_credential_create",
			Resource: key.ID,
			Status:   "success",
			ActorIP:  requestIP(r),
		})
		writeJSON(w, http.StatusCreated, map[string]any{"credential": key, "username": p.Namespace, "password": cleartext})
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *apiServer) handleRegistryCredentialItem(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok || p.userID() == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/user/registry-credentials/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential path"})
		return
	}
	key, err := s.platform.RevokeRegistryCredential(r.Context(), p.userID(), parts[0])
	if err != nil {
		s.platform.WriteAudit(r.Context(), auditEvent{
			UserID:   p.userID(),
			Action:   "registry_credential_revoke",
			Resource: parts[0],
			Status:   "error",
			Message:  err.Error(),
			ActorIP:  requestIP(r),
		})
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "credential not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke credential"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{
		UserID:   p.userID(),
		Action:   "registry_credential_revoke",
		Resource: key.ID,
		Status:   "success",
		ActorIP:  requestIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"credential": key})
}
