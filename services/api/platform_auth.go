package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const platformAccessTokenTTL = 15 * time.Minute

func platformDSNFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("POSTGRES_DSN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

func platformJWTSecretFromEnv() []byte {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_JWT_SECRET")); v != "" {
		return []byte(v)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("warning: failed to generate platform JWT secret: %v", err)
		return []byte("dev-only-mcp-runtime-platform-secret")
	}
	log.Printf("warning: PLATFORM_JWT_SECRET is not set; generated an ephemeral JWT secret")
	return []byte(base64.RawURLEncoding.EncodeToString(b))
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
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if req.Role == "" {
		req.Role = roleUser
	}
	if req.Role == roleAdmin {
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
	u, err := s.platform.CreatePasswordUser(r.Context(), req.Email, req.Password, req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.runtime != nil {
		if err := s.runtime.ensureUserNamespace(r.Context(), principal{Subject: u.ID, Role: u.Role, Email: u.Email, Namespace: u.Namespace}); err != nil {
			s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "namespace_create", Resource: u.Namespace, Namespace: u.Namespace, Status: "error", Message: err.Error(), ActorIP: requestIP(r)})
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to provision namespace"})
			return
		}
	}
	token, err := s.platform.CreateAccessToken(u, platformAccessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "signup", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r)})
	writeJSON(w, http.StatusCreated, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(platformAccessTokenTTL.Seconds()), "user": u})
}

func (s *apiServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	u, ok, err := s.platform.AuthenticatePassword(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login_failed"})
		return
	}
	if !ok {
		s.platform.WriteAudit(r.Context(), auditEvent{Action: "login", Resource: strings.ToLower(strings.TrimSpace(req.Email)), Status: "denied", Message: "invalid credentials", ActorIP: requestIP(r)})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	token, err := s.platform.CreateAccessToken(u, platformAccessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "login", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(platformAccessTokenTTL.Seconds()), "user": u})
}

func requestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("x-forwarded-for")); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return strings.TrimSpace(r.RemoteAddr)
}
