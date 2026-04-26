package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
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
)

type sessionPayload struct {
	ExpiresAt int64 `json:"exp"`
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

var loginAttempts = newLoginAttemptTracker(time.Now)

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
		log.Printf("WARNING: neither API_KEY nor API_KEYS is set; UI login is disabled and all /api requests will be rejected with 401")
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
	sessionKey := deriveSessionKey(apiKey)
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
	configJS := "window.MCP_API_BASE = " + string(baseJSON) + ";\n" +
		"window.MCP_DEFAULTS = " + string(defaultsJSON) + ";"
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/javascript")
		_, _ = w.Write([]byte(configJS))
	})
	mux.HandleFunc("/auth/login", handleLogin(apiKey, sessionKey))
	mux.HandleFunc("/auth/logout", handleLogout)
	mux.HandleFunc("/auth/status", handleStatus(apiKey, sessionKey))

	apiProxy := newAPIProxy(target, upstreamAPIKey, apiKeys, sessionKey)
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
		_, _ = w.Write(data)
	})

	return mux, nil
}

func newAPIProxy(target *url.URL, upstreamAPIKey, apiKeys string, sessionKey []byte) http.Handler {
	return newAPIProxyWithTransport(target, upstreamAPIKey, apiKeys, sessionKey, nil)
}

func newAPIProxyWithTransport(target *url.URL, upstreamAPIKey, apiKeys string, sessionKey []byte, transport http.RoundTripper) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	if transport != nil {
		proxy.Transport = transport
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Del("Cookie")
		req.Header.Del("x-api-key")
		req.Header.Set("x-api-key", upstreamAPIKey)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("api proxy error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "api_unavailable"})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validSession(r, sessionKey) && !validAPIKeyHeader(r, apiKeys) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func handleLogin(apiKey string, sessionKey []byte) http.HandlerFunc {
	type loginRequest struct {
		APIKey string `json:"api_key"`
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
		if apiKey == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "api_key_not_configured"})
			return
		}

		var req loginRequest
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		presented := strings.TrimSpace(req.APIKey)
		if presented == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_api_key"})
			return
		}
		if !hmac.Equal([]byte(presented), []byte(apiKey)) {
			failures := loginAttempts.recordFailure(clientID)
			if failures >= loginFailureLogEvery {
				log.Printf(`auth_login_failure client=%q timestamp=%q failure_count=%d`, clientID, time.Now().UTC().Format(time.RFC3339), failures)
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		priorFailures := loginAttempts.recordSuccess(clientID)
		if priorFailures > 0 {
			log.Printf(`auth_login_success_after_failures timestamp=%q prior_failures=%d`, time.Now().UTC().Format(time.RFC3339), priorFailures)
		}
		http.SetCookie(w, newSessionCookie(r, sessionKey))
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	}
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

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	http.SetCookie(w, expiredSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func handleStatus(apiKey string, sessionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": validSession(r, sessionKey)})
	}
}

func newSessionCookie(r *http.Request, sessionKey []byte) *http.Cookie {
	payload := sessionPayload{ExpiresAt: time.Now().Add(sessionDuration).Unix()}
	payloadBytes, _ := json.Marshal(payload)
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signaturePart := signSession(payloadBytes, sessionKey)

	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    payloadPart + "." + signaturePart,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteStrictMode,
	}
}

func expiredSessionCookie(r *http.Request) *http.Cookie {
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

func validSession(r *http.Request, sessionKey []byte) bool {
	if len(sessionKey) == 0 {
		return false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	if !hmac.Equal([]byte(parts[1]), []byte(signSession(payloadBytes, sessionKey))) {
		return false
	}
	var payload sessionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return false
	}
	return time.Now().Unix() < payload.ExpiresAt
}

func signSession(payload []byte, sessionKey []byte) string {
	mac := hmac.New(sha256.New, sessionKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
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

func deriveSessionKey(apiKey string) []byte {
	if apiKey == "" {
		return nil
	}
	prkMAC := hmac.New(sha256.New, []byte("mcp-sentinel-ui-session-key"))
	_, _ = prkMAC.Write([]byte(apiKey))
	prk := prkMAC.Sum(nil)

	expandMAC := hmac.New(sha256.New, prk)
	_, _ = expandMAC.Write([]byte("mcp-ui-session-cookie"))
	_, _ = expandMAC.Write([]byte{1})
	return expandMAC.Sum(nil)
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

	// Fallback: treat entire endpoint as host:port
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if insecureSet {
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return opts
	}
	return opts
}

// boolEnv parses a boolean environment variable.
// It returns the parsed boolean value and true if parsing succeeded.
// Returns false, false if the variable is not set or parsing failed.
func boolEnv(key string) (bool, bool) {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed, true
		}
	}
	return false, false
}
