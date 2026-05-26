package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mcp-sentinel-api/auth"
)

type passwordUserCreateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

func (s *apiServer) handleSignup(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var req passwordUserCreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, auth.PlatformSignupRequestMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	role, err := normalizePasswordUserRole(req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}
	if role == roleAdmin {
		p, ok, err := s.authenticateRequest(r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
			return
		}
		if !ok || p.Role != roleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin signup requires an admin principal"})
			return
		}
	}
	u, err := s.platform.CreatePasswordUser(r.Context(), req.Email, req.Password, role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	token, err := s.platform.CreateAccessToken(u, platformAccessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "signup", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r), Source: requestSource(r), AuthIdentity: "password:" + u.Email})
	writeJSON(w, http.StatusCreated, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(platformAccessTokenTTL.Seconds()), "user": u})
}

func (s *apiServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var req passwordUserCreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, auth.PlatformSignupRequestMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	role, err := normalizePasswordUserRole(req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}
	p, ok, err := s.authenticateRequest(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
		return
	}
	if !ok || p.Role != roleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	u, err := s.platform.CreatePasswordUser(r.Context(), req.Email, req.Password, role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: p.UserID(), Action: "user_create", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r), Source: requestSource(r), AuthIdentity: auditIdentityLabel(p)})
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func normalizePasswordUserRole(raw string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(raw))
	if role == "" {
		role = roleUser
	}
	if role != roleUser && role != roleAdmin {
		return "", errors.New("invalid role")
	}
	return role, nil
}
