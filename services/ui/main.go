package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

//go:embed static/*
var staticFS embed.FS

const (
	sessionCookieName = "mcp_ui_session"
	sessionDuration   = 8 * time.Hour

	defaultLoginRateLimitCapacity = 10
	defaultLoginRateLimitRefill   = time.Minute
	defaultLoginFailureWindow     = 15 * time.Minute
	defaultLoginFailureThreshold  = 5
	defaultLoginLockoutDuration   = 5 * time.Minute
	loginFailureLogEvery          = 3
)

var (
	loginRateLimitCapacity = intEnvOr("UI_LOGIN_RATE_CAPACITY", defaultLoginRateLimitCapacity)
	loginRateLimitRefill   = durationEnvOr("UI_LOGIN_RATE_REFILL", defaultLoginRateLimitRefill)
	loginFailureWindow     = durationEnvOr("UI_LOGIN_FAILURE_WINDOW", defaultLoginFailureWindow)
	loginFailureThreshold  = intEnvOr("UI_LOGIN_FAILURE_THRESHOLD", defaultLoginFailureThreshold)
	loginLockoutDuration   = durationEnvOr("UI_LOGIN_LOCKOUT", defaultLoginLockoutDuration)
	passwordLoginHook      func(context.Context, string, string, string) (sessionPrincipal, string, error)
)

type sessionPrincipal struct {
	Role     string `json:"role,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Email    string `json:"email,omitempty"`
	AuthType string `json:"auth_type,omitempty"`
}

type uiSession struct {
	ID                 string
	ExpiresAt          time.Time
	Principal          sessionPrincipal
	UpstreamAuthHeader string
	UpstreamAPIKey     string
}

type uiSessionStore struct {
	mu       sync.Mutex
	sessions map[string]uiSession
	now      func() time.Time
}

type loginAttemptTracker struct {
	mu      sync.Mutex
	clients map[string]*loginClientState
	now     func() time.Time
}

type loginClientState struct {
	tokens         int
	lastRefill     time.Time
	failures       int
	failuresExpire time.Time
	lockedUntil    time.Time
}

var (
	loginAttempts  = newLoginAttemptTracker(time.Now)
	sessions       = newUISessionStore(time.Now)
	oidcVerifyHook func(context.Context, string, string) (sessionPrincipal, error)
	authHTTPClient = &http.Client{Timeout: 10 * time.Second}
)

// main initializes and starts the MCP Sentinel UI server.
// It serves static web assets and provides a dynamic /config.js endpoint
// with API configuration for the frontend. Includes tracing support.
func main() {
	port := envOr("PORT", "8082")
	apiBase := envOr("API_BASE", "/api")
	apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
	apiKeys := strings.TrimSpace(os.Getenv("API_KEYS"))
	apiUpstream := envOr("API_UPSTREAM", "http://mcp-sentinel-api:8080")
	if apiKey == "" && apiKeys == "" {
		log.Printf("WARNING: neither API_KEY nor API_KEYS is set; UI API-key login is disabled")
	}

	mux, err := newMux(apiBase, apiUpstream, apiKey, apiKeys)
	if err != nil {
		log.Fatalf("invalid API upstream: %v", err)
	}

	shutdown, err := initTracer("mcp-sentinel-ui")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	log.Printf("mcp-sentinel-ui listening on :%s", port)
	handler := otelhttp.NewHandler(logRequests(mux), "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func newMux(apiBase, apiUpstream, apiKey, apiKeys string) (*http.ServeMux, error) {
	apiBase = normalizePathPrefix(apiBase)
	upstreamAPIKey := firstAPIKey(apiKeys)
	if upstreamAPIKey == "" {
		upstreamAPIKey = apiKey
		apiKeys = apiKey
	}
	target, err := url.Parse(apiUpstream)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, url.InvalidHostError(apiUpstream)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defaultNamespace := envOr("UI_DEFAULT_NAMESPACE", "mcp-servers")
	defaultPolicyVersion := envOr("UI_DEFAULT_POLICY_VERSION", "v1")
	baseJSON, err := json.Marshal(apiBase)
	if err != nil {
		return nil, err
	}
	defaultsJSON, err := json.Marshal(map[string]string{
		"namespace":     defaultNamespace,
		"policyVersion": defaultPolicyVersion,
	})
	if err != nil {
		return nil, err
	}
	googleClientIDJSON, err := json.Marshal(strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")))
	if err != nil {
		return nil, err
	}
	configJS := "window.MCP_API_BASE = " + string(baseJSON) + ";\n" +
		"window.MCP_DEFAULTS = " + string(defaultsJSON) + ";\n" +
		"window.MCP_GOOGLE_CLIENT_ID = " + string(googleClientIDJSON) + ";"
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/javascript")
		_, _ = w.Write([]byte(configJS))
	})
	mux.HandleFunc("/auth/login", handleLogin(apiKey, upstreamAPIKey, apiUpstream, sessions))
	mux.HandleFunc("/auth/logout", handleLogout(sessions))
	mux.HandleFunc("/auth/status", handleStatus(sessions))

	apiProxy := newAPIProxy(target, upstreamAPIKey, apiKeys, sessions)
	mux.Handle(apiBase+"/", apiProxy)
	mux.Handle(apiBase, apiProxy)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "static/index.html"
		} else {
			path = filepath.ToSlash(filepath.Join("static", path))
		}

		data, err := staticFS.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if ext := filepath.Ext(path); ext != "" {
			if ct := mime.TypeByExtension(ext); ct != "" {
				w.Header().Set("content-type", ct)
			}
		}
		w.WriteHeader(http.StatusOK)
		// #nosec G705 -- assets are bundled from repository static/ at build time.
		_, _ = w.Write(data)
	})

	return mux, nil
}

func newAPIProxy(target *url.URL, upstreamAPIKey, apiKeys string, store *uiSessionStore) http.Handler {
	return newAPIProxyWithTransport(target, upstreamAPIKey, apiKeys, store, nil)
}

func newAPIProxyWithTransport(target *url.URL, upstreamAPIKey, apiKeys string, store *uiSessionStore, transport http.RoundTripper) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	if transport != nil {
		proxy.Transport = transport
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Del("Cookie")
		if strings.TrimSpace(req.Header.Get("authorization")) == "" && strings.TrimSpace(req.Header.Get("x-api-key")) == "" {
			req.Header.Set("x-api-key", upstreamAPIKey)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("api proxy error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "api_unavailable"})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if validAPIKeyHeader(r, apiKeys) {
			req := r.Clone(r.Context())
			req.Header.Del("x-api-key")
			req.Header.Del("authorization")
			proxy.ServeHTTP(w, req)
			return
		}
		if allowsPublicRead(r) {
			req := r.Clone(r.Context())
			req.Header.Del("x-api-key")
			req.Header.Del("authorization")
			forcePublicRuntimeNamespace(req)
			proxy.ServeHTTP(w, req)
			return
		}

		sess, ok := store.sessionFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		req := r.Clone(r.Context())
		req.Header.Del("x-api-key")
		req.Header.Del("authorization")
		if sess.UpstreamAuthHeader != "" {
			req.Header.Set("authorization", sess.UpstreamAuthHeader)
		} else if sess.UpstreamAPIKey != "" {
			req.Header.Set("x-api-key", sess.UpstreamAPIKey)
		}
		proxy.ServeHTTP(w, req)
	})
}

func allowsPublicRead(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := strings.TrimSpace(strings.TrimSuffix(r.URL.Path, "/"))
	return strings.HasSuffix(path, "/runtime/servers")
}

func forcePublicRuntimeNamespace(r *http.Request) {
	path := strings.TrimSpace(strings.TrimSuffix(r.URL.Path, "/"))
	if !strings.HasSuffix(path, "/runtime/servers") {
		return
	}
	query := r.URL.Query()
	query.Set("namespace", "mcp-servers")
	r.URL.RawQuery = query.Encode()
}

func handleLogin(apiKey, upstreamAPIKey, apiUpstream string, store *uiSessionStore) http.HandlerFunc {
	type loginRequest struct {
		APIKey   string `json:"api_key"`
		IDToken  string `json:"id_token"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
			return
		}
		clientID := loginClientID(r)
		if !loginAttempts.allow(clientID) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests"})
			return
		}

		var req loginRequest
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}

		presentedAPIKey := strings.TrimSpace(req.APIKey)
		idToken := strings.TrimSpace(req.IDToken)
		email := strings.TrimSpace(req.Email)
		password := strings.TrimSpace(req.Password)
		if presentedAPIKey == "" && idToken == "" && (email == "" || password == "") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_credentials"})
			return
		}

		var (
			sess uiSession
			err  error
		)

		if email != "" || password != "" {
			var (
				p         sessionPrincipal
				token     string
				verifyErr error
			)
			if passwordLoginHook != nil {
				p, token, verifyErr = passwordLoginHook(r.Context(), apiUpstream, email, password)
			} else {
				p, token, verifyErr = loginPasswordWithAPI(r.Context(), apiUpstream, email, password)
			}
			if verifyErr != nil {
				failures := loginAttempts.recordFailure(clientID)
				if failures >= loginFailureLogEvery {
					// #nosec G706 -- authentication telemetry log with bounded fields.
					log.Printf(`auth_login_failure client=%q timestamp=%q failure_count=%d mode=%q`, clientID, time.Now().UTC().Format(time.RFC3339), failures, "password")
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			sess, err = store.createSession(r.Context(), uiSession{
				Principal:          p,
				UpstreamAuthHeader: "Bearer " + token,
			})
		} else if idToken != "" {
			var (
				p         sessionPrincipal
				verifyErr error
			)
			if oidcVerifyHook != nil {
				p, verifyErr = oidcVerifyHook(r.Context(), apiUpstream, idToken)
			} else {
				p, verifyErr = verifyOIDCTokenWithAPI(r.Context(), apiUpstream, idToken)
			}
			if verifyErr != nil {
				failures := loginAttempts.recordFailure(clientID)
				if failures >= loginFailureLogEvery {
					// #nosec G706 -- authentication telemetry log with bounded fields.
					log.Printf(`auth_login_failure client=%q timestamp=%q failure_count=%d mode=%q`, clientID, time.Now().UTC().Format(time.RFC3339), failures, "oidc")
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			sess, err = store.createSession(r.Context(), uiSession{
				Principal:          p,
				UpstreamAuthHeader: "Bearer " + idToken,
				ExpiresAt:          idTokenExpiry(idToken),
			})
		} else {
			if apiKey == "" {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "api_key_not_configured"})
				return
			}
			if !hmac.Equal([]byte(presentedAPIKey), []byte(apiKey)) {
				failures := loginAttempts.recordFailure(clientID)
				if failures >= loginFailureLogEvery {
					// #nosec G706 -- authentication telemetry log with bounded fields.
					log.Printf(`auth_login_failure client=%q timestamp=%q failure_count=%d mode=%q`, clientID, time.Now().UTC().Format(time.RFC3339), failures, "api_key")
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			sess, err = store.createSession(r.Context(), uiSession{
				Principal: sessionPrincipal{
					Role:     "admin",
					AuthType: "ui_api_key",
				},
				UpstreamAPIKey: upstreamAPIKey,
			})
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session_create_failed"})
			return
		}

		priorFailures := loginAttempts.recordSuccess(clientID)
		if priorFailures > 0 {
			// #nosec G706 -- authentication telemetry log with bounded fields.
			log.Printf(`auth_login_success_after_failures timestamp=%q prior_failures=%d`, time.Now().UTC().Format(time.RFC3339), priorFailures)
		}
		http.SetCookie(w, newSessionCookie(r, sess.ID, sess.ExpiresAt))
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "principal": sess.Principal})
	}
}

func loginPasswordWithAPI(ctx context.Context, apiUpstream, email, password string) (sessionPrincipal, string, error) {
	loginURL, err := apiUpstreamURL(apiUpstream, "api", "auth", "login")
	if err != nil {
		return sessionPrincipal{}, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return sessionPrincipal{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(string(body)))
	if err != nil {
		return sessionPrincipal{}, "", err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return sessionPrincipal{}, "", err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return sessionPrincipal{}, "", fmt.Errorf("password auth failed: status %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			Namespace string `json:"namespace"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return sessionPrincipal{}, "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return sessionPrincipal{}, "", errors.New("missing access token")
	}
	role := strings.TrimSpace(payload.User.Role)
	if role == "" {
		role = "user"
	}
	return sessionPrincipal{
		Role:     role,
		Subject:  strings.TrimSpace(payload.User.ID),
		Email:    strings.TrimSpace(payload.User.Email),
		AuthType: "platform_jwt",
	}, payload.AccessToken, nil
}

func idTokenExpiry(idToken string) time.Time {
	parts := strings.Split(strings.TrimSpace(idToken), ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return time.Time{}
	}
	if claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0).UTC()
}

func verifyOIDCTokenWithAPI(ctx context.Context, apiUpstream, idToken string) (sessionPrincipal, error) {
	meURL, err := apiUpstreamURL(apiUpstream, "api", "auth", "me")
	if err != nil {
		return sessionPrincipal{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meURL, nil)
	if err != nil {
		return sessionPrincipal{}, err
	}
	req.Header.Set("authorization", "Bearer "+idToken)

	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return sessionPrincipal{}, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return sessionPrincipal{}, fmt.Errorf("auth check failed: status %d", resp.StatusCode)
	}

	var payload struct {
		Authenticated bool             `json:"authenticated"`
		Principal     sessionPrincipal `json:"principal"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return sessionPrincipal{}, err
	}
	if !payload.Authenticated {
		return sessionPrincipal{}, errors.New("not authenticated")
	}
	if strings.TrimSpace(payload.Principal.Role) == "" {
		payload.Principal.Role = "user"
	}
	if payload.Principal.AuthType == "" {
		payload.Principal.AuthType = "oidc_jwt"
	}
	return payload.Principal, nil
}

func apiUpstreamURL(apiUpstream string, parts ...string) (string, error) {
	base := strings.TrimSpace(apiUpstream)
	if base == "" {
		return "", errors.New("api upstream is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("api upstream must include scheme and host")
	}
	return url.JoinPath(base, parts...)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func loginClientID(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("x-forwarded-for")); forwardedFor != "" {
		client, _, _ := strings.Cut(forwardedFor, ",")
		if client = strings.TrimSpace(client); client != "" {
			return client
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func newLoginAttemptTracker(now func() time.Time) *loginAttemptTracker {
	return &loginAttemptTracker{clients: map[string]*loginClientState{}, now: now}
}

func (t *loginAttemptTracker) allow(clientID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateForLocked(clientID)
	now := t.now()
	refillLoginTokens(state, now)
	if now.Before(state.lockedUntil) {
		return false
	}
	if state.tokens <= 0 {
		return false
	}
	state.tokens--
	return true
}

func (t *loginAttemptTracker) recordFailure(clientID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateForLocked(clientID)
	now := t.now()
	if now.After(state.failuresExpire) {
		state.failures = 0
	}
	state.failures++
	state.failuresExpire = now.Add(loginFailureWindow)
	if state.failures >= loginFailureThreshold {
		state.lockedUntil = now.Add(loginLockoutDuration)
	}
	return state.failures
}

func (t *loginAttemptTracker) recordSuccess(clientID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateForLocked(clientID)
	prior := state.failures
	state.failures = 0
	state.failuresExpire = time.Time{}
	state.lockedUntil = time.Time{}
	return prior
}

func (t *loginAttemptTracker) stateForLocked(clientID string) *loginClientState {
	state := t.clients[clientID]
	if state == nil {
		now := t.now()
		state = &loginClientState{tokens: loginRateLimitCapacity, lastRefill: now}
		t.clients[clientID] = state
	}
	return state
}

func refillLoginTokens(state *loginClientState, now time.Time) {
	if state.lastRefill.IsZero() {
		state.lastRefill = now
	}
	elapsed := now.Sub(state.lastRefill)
	if elapsed < loginRateLimitRefill {
		return
	}
	refill := int(elapsed / loginRateLimitRefill)
	state.tokens += refill
	if state.tokens > loginRateLimitCapacity {
		state.tokens = loginRateLimitCapacity
	}
	state.lastRefill = state.lastRefill.Add(time.Duration(refill) * loginRateLimitRefill)
}

func newUISessionStore(now func() time.Time) *uiSessionStore {
	return &uiSessionStore{sessions: map[string]uiSession{}, now: now}
}

func (s *uiSessionStore) createSession(_ context.Context, session uiSession) (uiSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked()
	id, err := randomURLToken(24)
	if err != nil {
		return uiSession{}, err
	}
	session.ID = id
	maxExpiry := s.now().Add(sessionDuration)
	if session.ExpiresAt.IsZero() || session.ExpiresAt.After(maxExpiry) {
		session.ExpiresAt = maxExpiry
	}
	if !session.ExpiresAt.After(s.now()) {
		return uiSession{}, errors.New("session expiry is in the past")
	}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *uiSessionStore) sessionFromRequest(r *http.Request) (uiSession, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return uiSession{}, false
	}
	sessionID := strings.TrimSpace(cookie.Value)
	if sessionID == "" {
		return uiSession{}, false
	}
	return s.get(sessionID)
}

func (s *uiSessionStore) get(id string) (uiSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked()
	sess, ok := s.sessions[id]
	if !ok {
		return uiSession{}, false
	}
	return sess, true
}

func (s *uiSessionStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *uiSessionStore) purgeExpiredLocked() {
	now := s.now()
	for id, sess := range s.sessions {
		if !sess.ExpiresAt.After(now) {
			delete(s.sessions, id)
		}
	}
}

func handleLogout(store *uiSessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
			return
		}
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			store.delete(strings.TrimSpace(cookie.Value))
		}
		http.SetCookie(w, expiredSessionCookie(r))
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
	}
}

func handleStatus(store *uiSessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := store.sessionFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"principal":     sess.Principal,
		})
	}
}

func newSessionCookie(r *http.Request, sessionID string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = int(sessionDuration.Seconds())
	}
	// #nosec G124 -- Secure is enabled automatically for TLS / x-forwarded-proto=https; HttpOnly and SameSite are set.
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteStrictMode,
	}
}

func expiredSessionCookie(r *http.Request) *http.Cookie {
	// #nosec G124 -- Secure is enabled automatically for TLS / x-forwarded-proto=https; HttpOnly and SameSite are set.
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteStrictMode,
	}
}

func randomURLToken(rawBytes int) (string, error) {
	b := make([]byte, rawBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validAPIKeyHeader(r *http.Request, apiKeys string) bool {
	presented := strings.TrimSpace(r.Header.Get("x-api-key"))
	if presented == "" {
		return false
	}
	for _, key := range strings.Split(apiKeys, ",") {
		if hmac.Equal([]byte(presented), []byte(strings.TrimSpace(key))) {
			return true
		}
	}
	return false
}

func firstAPIKey(apiKeys string) string {
	for _, key := range strings.Split(apiKeys, ",") {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func secureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("x-forwarded-proto"), "https")
}

func normalizePathPrefix(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "/api"
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" && parsed.Path != trimmed {
		trimmed = parsed.Path
	}
	trimmed = "/" + strings.Trim(trimmed, "/")
	if trimmed == "/" {
		return "/api"
	}
	return trimmed
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("writeJSON encode error (status=%d): %v", status, err)
	}
}

// logRequests is middleware that logs HTTP requests.
// It logs the HTTP method, URL path, response status, and duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		// #nosec G706 -- request path/method logging for operational diagnostics.
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// initTracer initializes OpenTelemetry tracing for the service.
// It configures OTLP HTTP exporter and sets up the tracer provider.
// Returns a shutdown function to clean up resources and any initialization error.
// If no OTEL_EXPORTER_OTLP_ENDPOINT is configured, returns a no-op shutdown function.
func initTracer(serviceName string) (func(context.Context) error, error) {
	if envName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); envName != "" {
		serviceName = envName
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	opts := otlpTraceOptions(endpoint)
	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// envOr returns the value of an environment variable or a fallback if not set.
// If the environment variable is set to a non-empty value, it returns that value.
// Otherwise, it returns the provided fallback value.
func envOr(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func intEnvOr(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed <= 0 {
		// #nosec G706 -- fixed-format env validation log for local operator diagnostics.
		log.Printf("invalid %s=%q; using default %d", key, val, fallback)
		return fallback
	}
	return parsed
}

func durationEnvOr(key string, fallback time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(val)
	if err != nil || parsed <= 0 {
		// #nosec G706 -- fixed-format env validation log for local operator diagnostics.
		log.Printf("invalid %s=%q; using default %s", key, val, fallback)
		return fallback
	}
	return parsed
}

// otlpTraceOptions configures OTLP HTTP exporter options.
// It sets up the endpoint URL and configures secure/insecure connections
// based on whether the endpoint uses HTTPS or HTTP.
func otlpTraceOptions(endpoint string) []otlptracehttp.Option {
	insecure, insecureSet := boolEnv("OTEL_EXPORTER_OTLP_INSECURE")
	if u, err := url.Parse(endpoint); err == nil {
		// Handle URLs with schemes (http://host:port/path)
		if u.Scheme != "" && u.Host == "" {
			// This is a scheme-less endpoint, fall through to treat as host:port
		} else if u.Scheme != "" && u.Host != "" {
			opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
			if u.Path != "" {
				opts = append(opts, otlptracehttp.WithURLPath(u.Path))
			}
			if insecureSet {
				if insecure {
					opts = append(opts, otlptracehttp.WithInsecure())
				}
				return opts
			}
			if u.Scheme == "http" {
				opts = append(opts, otlptracehttp.WithInsecure())
			}
			return opts
		}
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if insecureSet {
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return opts
	}
	if strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return opts
}

func boolEnv(key string) (bool, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		// #nosec G706 -- fixed-format env validation log for local operator diagnostics.
		log.Printf("invalid %s=%q; ignoring", key, v)
		return false, false
	}
}
