package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const platformAccessTokenTTL = 15 * time.Minute
const (
	apiLoginLockoutBase = 15 * time.Second
	apiLoginLockoutMax  = 5 * time.Minute
)

var platformLoginAttempts = newAPILoginAttemptTracker(time.Now)
var oidcLoginHook func(context.Context, *apiServer, string) (platformUser, error)
var errOIDCUnauthorized = errors.New("oidc unauthorized")

type apiLoginAttempt struct {
	failures    int
	lockedUntil time.Time
}

type apiLoginAttemptTracker struct {
	mu      sync.Mutex
	nowFunc func() time.Time
	entries map[string]apiLoginAttempt
}

func newAPILoginAttemptTracker(nowFn func() time.Time) *apiLoginAttemptTracker {
	return &apiLoginAttemptTracker{
		nowFunc: nowFn,
		entries: map[string]apiLoginAttempt{},
	}
}

func (t *apiLoginAttemptTracker) allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.entries[key]
	now := t.nowFunc()
	if state.lockedUntil.IsZero() || !state.lockedUntil.After(now) {
		return true
	}
	return false
}

func (t *apiLoginAttemptTracker) recordFailure(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	state := t.entries[key]
	state.failures++
	state.lockedUntil = now.Add(lockoutDurationForFailures(state.failures))
	t.entries[key] = state
	return state.failures
}

func (t *apiLoginAttemptTracker) recordSuccess(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.entries[key]
	failures := state.failures
	delete(t.entries, key)
	return failures
}

func lockoutDurationForFailures(failures int) time.Duration {
	if failures <= 2 {
		return 0
	}
	steps := failures - 2
	lockout := apiLoginLockoutBase
	for i := 1; i < steps; i++ {
		lockout *= 2
		if lockout >= apiLoginLockoutMax {
			return apiLoginLockoutMax
		}
	}
	if lockout > apiLoginLockoutMax {
		return apiLoginLockoutMax
	}
	return lockout
}

func platformDSNFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("POSTGRES_DSN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

func platformJWTSecretFromEnv() ([]byte, error) {
	secret := strings.TrimSpace(os.Getenv("PLATFORM_JWT_SECRET"))
	if secret == "" {
		return nil, errors.New("PLATFORM_JWT_SECRET is required when platform identity is enabled")
	}
	return []byte(secret), nil
}

func runPlatformAdminBootstrap(ctx context.Context) error {
	dsn := platformDSNFromEnv()
	if dsn == "" {
		return errors.New("POSTGRES_DSN (or DATABASE_URL) is required for PLATFORM_ADMIN_BOOTSTRAP_ONLY")
	}
	jwtSecret, err := platformJWTSecretFromEnv()
	if err != nil {
		return err
	}
	store, err := newPlatformStore(ctx, dsn, jwtSecret)
	if err != nil {
		return err
	}
	defer store.close()
	return seedPlatformAdminFromEnv(ctx, store)
}

func seedPlatformAdminFromEnv(ctx context.Context, store *platformStore) error {
	email := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_EMAIL"))
	password := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_PASSWORD"))
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return errors.New("PLATFORM_ADMIN_EMAIL and PLATFORM_ADMIN_PASSWORD must both be set")
	}
	u, err := store.EnsurePasswordUser(ctx, email, password, roleAdmin)
	if err != nil {
		return err
	}
	log.Printf("platform admin user ensured email=%q namespace=%q", u.Email, u.Namespace)
	return nil
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
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Role == "" {
		req.Role = roleUser
	}
	if req.Role != roleUser && req.Role != roleAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
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
			if cleanupErr := s.platform.DeleteUser(r.Context(), u.ID); cleanupErr != nil {
				log.Printf("signup cleanup failed for user %s: %v", u.ID, cleanupErr)
				s.platform.WriteAudit(r.Context(), auditEvent{
					UserID:    u.ID,
					Action:    "signup_cleanup",
					Resource:  "user",
					Namespace: u.Namespace,
					Status:    "error",
					Message:   cleanupErr.Error(),
					ActorIP:   requestIP(r),
				})
			} else {
				s.platform.WriteAudit(r.Context(), auditEvent{
					UserID:    u.ID,
					Action:    "signup_cleanup",
					Resource:  "user",
					Namespace: u.Namespace,
					Status:    "success",
					ActorIP:   requestIP(r),
				})
			}
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
	email := strings.ToLower(strings.TrimSpace(req.Email))
	attemptKey := requestIP(r)
	if email != "" {
		attemptKey += "|" + email
	}
	if !platformLoginAttempts.allow(attemptKey) {
		s.platform.WriteAudit(r.Context(), auditEvent{
			Action:   "login",
			Resource: email,
			Status:   "denied",
			Message:  "rate_limited",
			ActorIP:  requestIP(r),
		})
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests"})
		return
	}
	u, ok, err := s.platform.AuthenticatePassword(r.Context(), email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login_failed"})
		return
	}
	if !ok {
		failures := platformLoginAttempts.recordFailure(attemptKey)
		s.platform.WriteAudit(r.Context(), auditEvent{
			Action:   "login",
			Resource: email,
			Status:   "denied",
			Message:  fmt.Sprintf("invalid credentials (failures=%d)", failures),
			ActorIP:  requestIP(r),
		})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	platformLoginAttempts.recordSuccess(attemptKey)
	token, err := s.platform.CreateAccessToken(u, platformAccessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "login", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(platformAccessTokenTTL.Seconds()), "user": u})
}

func (s *apiServer) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if s.jwks == nil || strings.TrimSpace(s.oidcIssuer) == "" || strings.TrimSpace(s.oidcAudience) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "oidc_not_configured"})
		return
	}

	var req struct {
		IDToken string `json:"id_token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	idToken := strings.TrimSpace(req.IDToken)
	if idToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_id_token"})
		return
	}

	var (
		u   platformUser
		err error
	)
	if oidcLoginHook != nil {
		u, err = oidcLoginHook(r.Context(), s, idToken)
	} else {
		u, err = s.resolveOIDCLoginUser(r.Context(), idToken)
	}
	if err != nil {
		statusCode := http.StatusInternalServerError
		auditStatus := "error"
		auditResource := strings.ToLower(strings.TrimSpace(u.Email))
		if auditResource == "" {
			auditResource = oidcAuditResource(idToken)
		}
		if errors.Is(err, errOIDCUnauthorized) {
			statusCode = http.StatusUnauthorized
			auditStatus = "denied"
		}
		s.platform.WriteAudit(r.Context(), auditEvent{
			Action:   "oidc_login",
			Resource: auditResource,
			Status:   auditStatus,
			Message:  err.Error(),
			ActorIP:  requestIP(r),
		})
		if statusCode == http.StatusUnauthorized {
			writeJSON(w, statusCode, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, statusCode, map[string]string{"error": "login_failed"})
		return
	}

	token, err := s.platform.CreateAccessToken(u, platformAccessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	s.platform.WriteAudit(r.Context(), auditEvent{UserID: u.ID, Action: "oidc_login", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(platformAccessTokenTTL.Seconds()), "user": u})
}

func (s *apiServer) resolveOIDCLoginUser(ctx context.Context, idToken string) (platformUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://oidc.internal/verify", nil)
	if err != nil {
		return platformUser{}, err
	}
	req.Header.Set("authorization", "Bearer "+idToken)

	p, ok, err := s.authenticateRequest(req)
	if err != nil {
		return platformUser{}, err
	}
	if !ok || p.AuthType != "oidc_jwt" {
		return platformUser{}, fmt.Errorf("%w: token authentication failed", errOIDCUnauthorized)
	}
	if p.Subject == "" || p.Email == "" {
		return platformUser{}, fmt.Errorf("%w: token missing identity", errOIDCUnauthorized)
	}

	return platformUser{
		ID:        p.Subject,
		Email:     p.Email,
		Role:      p.Role,
		Namespace: p.Namespace,
	}, nil
}

func oidcAuditResource(idToken string) string {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(strings.TrimSpace(idToken), claims); err != nil {
		return "unknown"
	}
	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "unknown"
	}
	return email
}

func requestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("x-forwarded-for")); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return strings.TrimSpace(host)
	}
	return remote
}
