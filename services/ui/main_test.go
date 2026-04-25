package main

import (
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
	sessionKey := deriveSessionKey("ui-secret")
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
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret", sessionKey, transport)

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if upstreamCalled {
		t.Fatal("unauthenticated request reached upstream")
	}

	login := httptest.NewRecorder()
	handleLogin("ui-secret", sessionKey).ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"api_key":"ui-secret"}`)))
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
	proxy := newAPIProxyWithTransport(target, "api-secret", "api-secret,backup-secret", deriveSessionKey("ui-secret"), transport)

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

	handler := handleLogin("secret", deriveSessionKey("secret"))
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

	handler := handleLogin("secret", deriveSessionKey("secret"))
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
