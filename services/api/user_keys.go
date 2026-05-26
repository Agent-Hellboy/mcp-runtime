package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type userAPIKeyStore interface {
	AuthenticateUserAPIKey(ctx context.Context, rawKey string) (principal, bool, error)
	ListUserAPIKeys(ctx context.Context, userID string) ([]userAPIKeySummary, error)
	CreateUserAPIKey(ctx context.Context, userID, name string) (userAPIKeySummary, string, error)
	RevokeUserAPIKey(ctx context.Context, userID, id string) (userAPIKeySummary, error)
}

func (s *apiServer) handleUserAPIKeys(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.userKeys == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user api key store not configured"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.userKeys.ListUserAPIKeys(r.Context(), p.UserID())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list user api keys"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBodyDecodeError(w, err)
			return
		}
		key, cleartext, err := s.userKeys.CreateUserAPIKey(r.Context(), p.UserID(), req.Name)
		if err != nil {
			if s.platform != nil {
				s.platform.WriteAudit(r.Context(), auditEvent{
					UserID:       p.UserID(),
					Action:       "api_key_create",
					Resource:     strings.TrimSpace(req.Name),
					Namespace:    p.Namespace,
					Status:       "error",
					Message:      err.Error(),
					ActorIP:      requestIP(r),
					Source:       auditSource(r, p),
					AuthIdentity: auditIdentityLabel(p),
				})
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if s.platform != nil {
			s.platform.WriteAudit(r.Context(), auditEvent{
				UserID:       p.UserID(),
				Action:       "api_key_create",
				Resource:     key.ID,
				Namespace:    p.Namespace,
				Status:       "success",
				ActorIP:      requestIP(r),
				Source:       auditSource(r, p),
				AuthIdentity: auditIdentityLabel(p),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"key":          key,
			"api_key":      cleartext,
			"one_time_key": cleartext,
		})
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *apiServer) handleUserAPIKeyItem(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.userKeys == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user api key store not configured"})
		return
	}
	keyID, allowed, valid := parseUserAPIKeyItemPath(r.Method, r.URL.Path)
	if !allowed {
		w.Header().Set("allow", "DELETE, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key path"})
		return
	}
	key, revokeErr := s.userKeys.RevokeUserAPIKey(r.Context(), p.UserID(), keyID)
	if revokeErr != nil {
		if s.platform != nil {
			s.platform.WriteAudit(r.Context(), auditEvent{
				UserID:       p.UserID(),
				Action:       "api_key_revoke",
				Resource:     keyID,
				Namespace:    p.Namespace,
				Status:       "error",
				Message:      revokeErr.Error(),
				ActorIP:      requestIP(r),
				Source:       auditSource(r, p),
				AuthIdentity: auditIdentityLabel(p),
			})
		}
		if apierrors.IsNotFound(revokeErr) || errors.Is(revokeErr, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke key"})
		return
	}
	if s.platform != nil {
		s.platform.WriteAudit(r.Context(), auditEvent{
			UserID:       p.UserID(),
			Action:       "api_key_revoke",
			Resource:     key.ID,
			Namespace:    p.Namespace,
			Status:       "success",
			ActorIP:      requestIP(r),
			Source:       auditSource(r, p),
			AuthIdentity: auditIdentityLabel(p),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func parseUserAPIKeyItemPath(method, path string) (keyID string, allowed bool, valid bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/user/api-keys/"), "/"), "/")
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
