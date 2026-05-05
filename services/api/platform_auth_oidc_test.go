package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
)

func TestHandleOIDCLoginSuccess(t *testing.T) {
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, _ *apiServer, token string) (platformUser, error) {
		if token != "google-id-token" {
			t.Fatalf("id token = %q", token)
		}
		return platformUser{
			ID:        "user-123",
			Email:     "user@example.com",
			Role:      roleUser,
			Namespace: "user-1",
		}, nil
	}
	defer func() { oidcLoginHook = previousHook }()

	server := &apiServer{
		platform:     &platformStore{jwtSecret: []byte("test-secret")},
		jwks:         &keyfunc.JWKS{},
		oidcIssuer:   "https://issuer.example",
		oidcAudience: "client-id",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{"id_token":"google-id-token"}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		AccessToken string       `json:"access_token"`
		TokenType   string       `json:"token_type"`
		ExpiresIn   int          `json:"expires_in"`
		User        platformUser `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if strings.Contains(payload.AccessToken, "google-id-token") {
		t.Fatalf("platform token leaked raw id token: %q", payload.AccessToken)
	}
	if payload.TokenType != "bearer" {
		t.Fatalf("token_type = %q, want bearer", payload.TokenType)
	}
	if payload.ExpiresIn != int(platformAccessTokenTTL.Seconds()) {
		t.Fatalf("expires_in = %d, want %d", payload.ExpiresIn, int(platformAccessTokenTTL.Seconds()))
	}
	if payload.User.ID != "user-123" || payload.User.Email != "user@example.com" {
		t.Fatalf("user payload = %+v", payload.User)
	}

	parsed, err := jwt.Parse(payload.AccessToken, func(t *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("platform token parse failed: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("missing jwt claims")
	}
	if got := strings.TrimSpace(fmt.Sprint(claims["sub"])); got != "user-123" {
		t.Fatalf("subject claim = %q, want user-123", got)
	}
}

func TestHandleOIDCLoginRequiresPlatformStore(t *testing.T) {
	server := &apiServer{
		jwks:         &keyfunc.JWKS{},
		oidcIssuer:   "https://issuer.example",
		oidcAudience: "client-id",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{"id_token":"google-id-token"}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleOIDCLoginRequiresOIDCConfig(t *testing.T) {
	server := &apiServer{
		platform: &platformStore{jwtSecret: []byte("test-secret")},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{"id_token":"google-id-token"}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleOIDCLoginMissingToken(t *testing.T) {
	server := &apiServer{
		platform:     &platformStore{jwtSecret: []byte("test-secret")},
		jwks:         &keyfunc.JWKS{},
		oidcIssuer:   "https://issuer.example",
		oidcAudience: "client-id",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleOIDCLoginInternalError(t *testing.T) {
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, _ *apiServer, _ string) (platformUser, error) {
		return platformUser{Email: "user@example.com"}, errors.New("failed")
	}
	defer func() { oidcLoginHook = previousHook }()

	server := &apiServer{
		platform:     &platformStore{jwtSecret: []byte("test-secret")},
		jwks:         &keyfunc.JWKS{},
		oidcIssuer:   "https://issuer.example",
		oidcAudience: "client-id",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{"id_token":"google-id-token"}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleOIDCLoginInvalidOIDCToken(t *testing.T) {
	previousHook := oidcLoginHook
	oidcLoginHook = func(_ context.Context, _ *apiServer, _ string) (platformUser, error) {
		return platformUser{Email: "user@example.com"}, errOIDCUnauthorized
	}
	defer func() { oidcLoginHook = previousHook }()

	server := &apiServer{
		platform:     &platformStore{jwtSecret: []byte("test-secret")},
		jwks:         &keyfunc.JWKS{},
		oidcIssuer:   "https://issuer.example",
		oidcAudience: "client-id",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc", strings.NewReader(`{"id_token":"google-id-token"}`))
	server.handleOIDCLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestOIDCAuditResourceUsesUnverifiedEmailClaim(t *testing.T) {
	idToken := unsignedTestJWT(`{"email":"USER@example.COM"}`)
	if got := oidcAuditResource(idToken); got != "user@example.com" {
		t.Fatalf("oidcAuditResource() = %q, want user@example.com", got)
	}
}

func TestOIDCAuditResourceFallsBackToUnknown(t *testing.T) {
	for _, idToken := range []string{
		"",
		"not-a-jwt",
		unsignedTestJWT(`{"sub":"user-123"}`),
	} {
		if got := oidcAuditResource(idToken); got != "unknown" {
			t.Fatalf("oidcAuditResource(%q) = %q, want unknown", idToken, got)
		}
	}
}

func unsignedTestJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + body + ".sig"
}
