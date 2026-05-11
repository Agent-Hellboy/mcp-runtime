package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
)

func TestHandleEventsRejectsWhitespaceOnlyRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "source",
			body: `{"source":"   ","event_type":"tool.call","payload":{"ok":true}}`,
		},
		{
			name: "event type",
			body: `{"source":"mcp-proxy","event_type":"   ","payload":{"ok":true}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := &ingestServer{}
			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.handleEvents(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if !strings.Contains(recorder.Body.String(), `"error":"missing_fields"`) {
				t.Fatalf("body = %q, want missing_fields", recorder.Body.String())
			}
		})
	}
}

func TestHandleReadyWithoutKafkaReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	server := &ingestServer{}
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	recorder := httptest.NewRecorder()

	server.handleReady(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), `"error":"kafka_unavailable"`) {
		t.Fatalf("body = %q, want kafka_unavailable", recorder.Body.String())
	}
}

func TestAuthRejectsJWKSOnlyBearerToken(t *testing.T) {
	t.Parallel()

	issuer := newTestIngestJWTIssuer(t)
	jwks, err := keyfunc.Get(issuer.server.URL+"/keys", keyfunc.Options{})
	if err != nil {
		t.Fatalf("keyfunc.Get() error = %v", err)
	}
	server := &ingestServer{jwks: jwks}
	called := false
	handler := server.auth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	req.Header.Set("Authorization", "Bearer "+issuer.sign(t, jwt.MapClaims{
		"iss": "https://issuer.example.com",
		"aud": "mcp-runtime",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if called {
		t.Fatal("handler should not be called when issuer and audience are both unset")
	}
}

type testIngestJWTIssuer struct {
	privateKey *rsa.PrivateKey
	server     *httptest.Server
}

func newTestIngestJWTIssuer(t *testing.T) *testIngestJWTIssuer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	issuer := &testIngestJWTIssuer{privateKey: privateKey}
	issuer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{ingestRSAJWK(&privateKey.PublicKey)},
		})
	}))
	t.Cleanup(issuer.server.Close)
	return issuer
}

func (i *testIngestJWTIssuer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(i.privateKey)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return signed
}

func ingestRSAJWK(publicKey *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": "test-key",
		"n":   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
	}
}
