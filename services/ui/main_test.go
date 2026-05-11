package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConfigDoesNotExposeAPIKey(t *testing.T) {
	mux, err := newMux("/api", "http://127.0.0.1:1", "secret", "api-secret")
	if err != nil {
		t.Fatalf("newMux() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/config.js", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "MCP_API_KEY") || strings.Contains(body, "secret") {
		t.Fatalf("config.js exposed API key material: %q", body)
	}
	if !strings.Contains(body, "MCP_API_BASE") {
		t.Fatalf("config.js missing API base: %q", body)
	}
}

func TestAPIProxyRequiresAuthenticatedSession(t *testing.T) {
	upstreamCalled := false
	store := newUISessionStore(time.Now)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		if got := r.Header.Get("x-api-key"); got != "api-secret" {
			t.Fatalf("x-api-key = %q, want %q", got, "api-secret")
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Fatalf("Cookie header forwarded upstream: %q", got)
		}
		if got := r.URL.Path; got != "/api/dashboard/summary" {
			t.Fatalf("path = %q, want /api/dashboard/summary", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"content-type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})
	target, err := url.Parse("http://api.example")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret", store, transport)

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if upstreamCalled {
		t.Fatal("unauthenticated request reached upstream")
	}

	login := httptest.NewRecorder()
	handleLogin("ui-secret", "api-secret", "http://api.example", store).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"api_key":"ui-secret"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	if strings.Contains(cookies[0].Value, "ui-secret") {
		t.Fatal("session cookie contains raw API key")
	}

	authed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	req.AddCookie(cookies[0])
	proxy.ServeHTTP(authed, req)
	if authed.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d; body=%s", authed.Code, http.StatusOK, authed.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("authenticated request did not reach upstream")
	}
}

func TestAPIProxyAllowsDirectAPIKeyClients(t *testing.T) {
	upstreamCalled := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		if got := r.Header.Get("x-api-key"); got != "api-secret" {
			t.Fatalf("x-api-key forwarded upstream = %q, want %q", got, "api-secret")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"content-type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})
	target, err := url.Parse("http://api.example")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret,backup-secret", newUISessionStore(time.Now), transport)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("x-api-key", "backup-secret")
	proxy.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("direct API-key status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("direct API-key request did not reach upstream")
	}
}

func TestAPIProxyRejectsAnonymousRuntimeServers(t *testing.T) {
	upstreamCalled := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		return nil, fmt.Errorf("anonymous request unexpectedly reached upstream: %s", r.URL.String())
	})
	target, err := url.Parse("http://api.example")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret", newUISessionStore(time.Now), transport)

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://localhost:18080/api/runtime/servers?namespace=user-private", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous runtime servers status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if upstreamCalled {
		t.Fatal("anonymous runtime servers request reached upstream")
	}
}

func TestHandleLoginWithOIDCToken(t *testing.T) {
	now := time.Now().UTC()
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, upstream, token string) (sessionPrincipal, string, time.Time, error) {
		if upstream != "http://api.example" {
			t.Fatalf("upstream = %q, want http://api.example", upstream)
		}
		if token != "id-token" {
			t.Fatalf("token = %q", token)
		}
		return sessionPrincipal{
			Role:     "user",
			Subject:  "user-123",
			AuthType: "platform_jwt",
		}, "platform-token", now.Add(15 * time.Minute), nil
	}
	defer func() { oidcLoginHook = previousHook }()

	store := newUISessionStore(time.Now)
	login := httptest.NewRecorder()
	handleLogin("", "api-secret", "http://api.example", store).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"id_token":"id-token"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	sess, ok := store.get(cookies[0].Value)
	if !ok {
		t.Fatal("expected persisted session")
	}
	if got := sess.UpstreamAuthHeader; got != "Bearer platform-token" {
		t.Fatalf("stored upstream authorization = %q", got)
	}
	if got := sess.UpstreamAuthHeader; strings.Contains(got, "id-token") {
		t.Fatalf("stored upstream authorization leaked raw id token: %q", got)
	}

	upstreamCalled := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		if got := r.Header.Get("authorization"); got != "Bearer platform-token" {
			t.Fatalf("authorization forwarded = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("x-api-key unexpectedly set: %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"content-type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})
	target, _ := url.Parse("http://api.example")
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret", store, transport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/user/api-keys", nil)
	req.AddCookie(cookies[0])
	proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("proxy did not call upstream")
	}
}

func TestHandleLoginWithOIDCTokenCapsSessionToTokenExpiry(t *testing.T) {
	now := time.Now().UTC().Add(2 * time.Minute)
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, upstream, token string) (sessionPrincipal, string, time.Time, error) {
		if upstream != "http://api.example" {
			t.Fatalf("upstream = %q, want http://api.example", upstream)
		}
		if token == "" {
			t.Fatal("token should not be empty")
		}
		return sessionPrincipal{
			Role:     "user",
			Subject:  "user-123",
			AuthType: "platform_jwt",
		}, "platform-token", now.Add(30 * time.Minute), nil
	}
	defer func() { oidcLoginHook = previousHook }()

	store := newUISessionStore(func() time.Time { return now })
	login := httptest.NewRecorder()
	handleLogin("", "api-secret", "http://api.example", store).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"id_token":"id-token"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookies[0].MaxAge > int((33 * time.Minute).Seconds()) {
		t.Fatalf("cookie MaxAge = %d, expected <= 33 minutes", cookies[0].MaxAge)
	}
	sess, ok := store.get(cookies[0].Value)
	if !ok {
		t.Fatal("expected persisted session")
	}
	exp := now.Add(30 * time.Minute)
	if sess.ExpiresAt.After(exp.Add(time.Second)) || sess.ExpiresAt.Before(exp.Add(-1*time.Second)) {
		t.Fatalf("session expiry = %s, want %s", sess.ExpiresAt.Format(time.RFC3339), exp.Format(time.RFC3339))
	}
}

func TestLoginOIDCSessionFallsBackToTokenVerificationWhenPlatformStoreUnavailable(t *testing.T) {
	now := time.Now().UTC().Add(2 * time.Minute)
	exp := now.Add(30 * time.Minute)
	payload := fmt.Sprintf(`{"exp":%d}`, exp.Unix())
	idToken := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"

	var paths []string
	previousClient := authHTTPClient
	authHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/auth/oidc":
			if r.Method != http.MethodPost {
				t.Fatalf("oidc method = %s, want POST", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read oidc body: %v", err)
			}
			if !strings.Contains(string(body), idToken) {
				t.Fatalf("oidc body = %s, want id token", string(body))
			}
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"content-type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":"platform identity database not configured"}`)),
			}, nil
		case "/api/auth/me":
			if got := r.Header.Get("authorization"); got != "Bearer "+idToken {
				t.Fatalf("fallback authorization = %q, want bearer id token", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"content-type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"authenticated":true,"principal":{"role":"user","subject":"user-123","email":"user@example.com"}}`)),
			}, nil
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		return nil, nil
	})}
	t.Cleanup(func() { authHTTPClient = previousClient })

	p, token, expiresAt, err := loginOIDCSession(context.Background(), "http://api.example", idToken)
	if err != nil {
		t.Fatalf("loginOIDCSession() error = %v", err)
	}
	if token != idToken {
		t.Fatalf("token = %q, want original id token", token)
	}
	if p.AuthType != "oidc_jwt" || p.Subject != "user-123" || p.Email != "user@example.com" {
		t.Fatalf("principal = %+v", p)
	}
	if expiresAt.After(exp.Add(time.Second)) || expiresAt.Before(exp.Add(-time.Second)) {
		t.Fatalf("session expiry = %s, want %s", expiresAt.Format(time.RFC3339), exp.Format(time.RFC3339))
	}
	if len(paths) != 2 || paths[0] != "/api/auth/oidc" || paths[1] != "/api/auth/me" {
		t.Fatalf("request paths = %v, want oidc exchange then auth/me fallback", paths)
	}
}

func TestUISessionStateIsEphemeralAcrossStoreRestart(t *testing.T) {
	now := time.Now().UTC()
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, upstream, token string) (sessionPrincipal, string, time.Time, error) {
		if upstream != "http://api.example" {
			t.Fatalf("upstream = %q, want http://api.example", upstream)
		}
		if token == "" {
			t.Fatal("token should not be empty")
		}
		return sessionPrincipal{Role: "user", Subject: "user-123", AuthType: "platform_jwt"}, "platform-token", now.Add(10 * time.Minute), nil
	}
	defer func() { oidcLoginHook = previousHook }()

	originalStore := newUISessionStore(func() time.Time { return now })
	login := httptest.NewRecorder()
	handleLogin("", "api-secret", "http://api.example", originalStore).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"id_token":"id-token"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}

	beforeRestart := httptest.NewRecorder()
	beforeReq := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	beforeReq.AddCookie(cookies[0])
	handleStatus(originalStore).ServeHTTP(beforeRestart, beforeReq)
	if !strings.Contains(beforeRestart.Body.String(), `"authenticated":true`) {
		t.Fatalf("status before restart = %s", beforeRestart.Body.String())
	}

	restartedStore := newUISessionStore(func() time.Time { return now })
	afterRestart := httptest.NewRecorder()
	afterReq := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	afterReq.AddCookie(cookies[0])
	handleStatus(restartedStore).ServeHTTP(afterRestart, afterReq)
	if !strings.Contains(afterRestart.Body.String(), `"authenticated":false`) {
		t.Fatalf("status after restart = %s", afterRestart.Body.String())
	}
}

func TestHandleLoginWithPassword(t *testing.T) {
	previousHook := passwordLoginHook
	passwordLoginHook = func(_ context.Context, upstream, email, password string) (sessionPrincipal, string, error) {
		if upstream != "http://api.example" {
			t.Fatalf("upstream = %q, want http://api.example", upstream)
		}
		if email != "admin@example.com" || password != "test-password" {
			t.Fatalf("credentials = %q/%q", email, password)
		}
		return sessionPrincipal{
			Role:     "admin",
			Subject:  "user-1",
			Email:    "admin@example.com",
			AuthType: "platform_jwt",
		}, "platform-token", nil
	}
	defer func() { passwordLoginHook = previousHook }()

	store := newUISessionStore(time.Now)
	login := httptest.NewRecorder()
	handleLogin("", "api-secret", "http://api.example", store).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"test-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}

	upstreamCalled := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		if got := r.Header.Get("authorization"); got != "Bearer platform-token" {
			t.Fatalf("authorization forwarded = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"content-type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})
	target, _ := url.Parse("http://api.example")
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret", store, transport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/user/api-keys", nil)
	req.AddCookie(cookies[0])
	proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("proxy did not call upstream")
	}
}

func TestLoginClientIDUsesForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("x-forwarded-for", "203.0.113.10, 10.0.0.2")

	if got := loginClientID(req); got != "203.0.113.10" {
		t.Fatalf("loginClientID() = %q, want forwarded client", got)
	}
}

func TestHandleLoginLocksOutRepeatedFailures(t *testing.T) {
	restore := useLoginAttemptTrackerForTest(t)
	defer restore()

	handler := handleLogin("secret", "api-secret", "http://api.example", newUISessionStore(time.Now))
	for i := 0; i < loginFailureThreshold; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, loginRequestFrom("198.51.100.10", `{"api_key":"wrong"}`))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want %d", i+1, recorder.Code, http.StatusUnauthorized)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, loginRequestFrom("198.51.100.10", `{"api_key":"secret"}`))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-out status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}

func TestHandleLoginSuccessResetsFailureCounter(t *testing.T) {
	restore := useLoginAttemptTrackerForTest(t)
	defer restore()

	handler := handleLogin("secret", "api-secret", "http://api.example", newUISessionStore(time.Now))
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, loginRequestFrom("198.51.100.11", `{"api_key":"wrong"}`))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want %d", i+1, recorder.Code, http.StatusUnauthorized)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, loginRequestFrom("198.51.100.11", `{"api_key":"secret"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	loginAttempts.mu.Lock()
	defer loginAttempts.mu.Unlock()
	if got := loginAttempts.clients["198.51.100.11"].failures; got != 0 {
		t.Fatalf("failure count after success = %d, want 0", got)
	}
}

func useLoginAttemptTrackerForTest(t *testing.T) func() {
	t.Helper()
	previous := loginAttempts
	loginAttempts = newLoginAttemptTracker(func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	return func() {
		loginAttempts = previous
	}
}

func loginRequestFrom(clientID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("x-forwarded-for", clientID)
	return req
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSecurityHeadersMiddlewareAlwaysSetsBaselineHeaders(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	wantContains := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "interest-cohort=()",
		"Content-Security-Policy": "frame-ancestors 'none'",
	}
	for header, fragment := range wantContains {
		got := rec.Header().Get(header)
		if !strings.Contains(got, fragment) {
			t.Fatalf("%s = %q, want substring %q", header, got, fragment)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' https://accounts.google.com https://apis.google.com") {
		t.Fatalf("Content-Security-Policy script-src = %q, want external scripts without unsafe-inline", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy script-src allows unsafe-inline: %q", csp)
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("HSTS should not be set on plain HTTP, got %q", rec.Header().Get("Strict-Transport-Security"))
	}
}

func TestSecurityHeadersMiddlewareSetsHSTSWhenForwardedHTTPS(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=") {
		t.Fatalf("Strict-Transport-Security = %q, want max-age", got)
	}
}

func TestSecurityHeadersMiddlewareSetsCacheControlOnAPI(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control on /api = %q, want no-store, no-cache, must-revalidate", got)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control on /auth/ = %q, want no-store, no-cache, must-revalidate", got)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/styles.css", nil))
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("Cache-Control on static asset = %q, want empty", got)
	}
}

func TestHTTPSRedirectMiddlewareAutoModeRedirectsPublicHTTP(t *testing.T) {
	handler := httpsRedirectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when redirecting")
	}), "auto")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard?x=1", nil)
	req.Host = "platform.example.com"
	req.Header.Set("X-Forwarded-Proto", "http")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPermanentRedirect)
	}
	if got := rec.Header().Get("Location"); got != "https://platform.example.com/dashboard?x=1" {
		t.Fatalf("Location = %q", got)
	}
}

func TestHTTPSRedirectMiddlewareAutoModeSkipsLocalhost(t *testing.T) {
	called := false
	handler := httpsRedirectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}), "auto")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "localhost:18080"
	req.Header.Set("X-Forwarded-Proto", "http")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !called {
		t.Fatalf("expected pass-through for localhost, got status=%d called=%v", rec.Code, called)
	}
}

func TestHTTPSRedirectMiddlewareDisabledMode(t *testing.T) {
	called := false
	handler := httpsRedirectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}), "false")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "platform.example.com"
	req.Header.Set("X-Forwarded-Proto", "http")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected pass-through when UI_REQUIRE_HTTPS=false")
	}
}

func TestHTTPSRedirectMiddlewareForcedAliasesNoForwardedProto(t *testing.T) {
	for _, mode := range []string{"true", "on", "1", "yes"} {
		t.Run(mode, func(t *testing.T) {
			handler := httpsRedirectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Fatal("next handler must not be called when redirecting")
			}), mode)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			req.Host = "platform.example.com"
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusPermanentRedirect {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusPermanentRedirect)
			}
			if got := rec.Header().Get("Location"); got != "https://platform.example.com/dashboard" {
				t.Fatalf("Location = %q", got)
			}
		})
	}
}

func TestHTTPSRedirectMiddlewarePassesThroughHTTPS(t *testing.T) {
	called := false
	handler := httpsRedirectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}), "auto")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "platform.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected HTTPS request to pass through")
	}
}

func TestSecureCookieCanBeForcedByConfig(t *testing.T) {
	previous := forceSecureCookie
	forceSecureCookie = true
	t.Cleanup(func() {
		forceSecureCookie = previous
	})

	req := httptest.NewRequest(http.MethodGet, "http://platform.example.com/", nil)
	req.Header.Set("X-Forwarded-Proto", "http")

	if !secureCookie(req) {
		t.Fatal("secureCookie() = false, want true when forceSecureCookie=true")
	}
}
