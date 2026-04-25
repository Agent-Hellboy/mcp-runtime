package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfigDoesNotExposeAPIKey(t *testing.T) {
	mux, err := newMux("/api", "http://127.0.0.1:1", "secret")
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
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalled = true
		if got := r.Header.Get("x-api-key"); got != "secret" {
			t.Fatalf("x-api-key = %q, want %q", got, "secret")
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
	proxy := newAPIProxyWithTransport(target, "secret", transport)

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if upstreamCalled {
		t.Fatal("unauthenticated request reached upstream")
	}

	login := httptest.NewRecorder()
	handleLogin("secret").ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"api_key":"secret"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	if strings.Contains(cookies[0].Value, "secret") {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
