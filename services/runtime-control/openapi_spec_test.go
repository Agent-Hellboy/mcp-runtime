package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-runtime-control/internal/platforminternal"
	"mcp-runtime/pkg/openapi"
)

func TestOpenAPISpecLoads(t *testing.T) {
	if _, err := openapi.Load(openAPISpec); err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}
}

func TestHealthResponseMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(openAPISpec)
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	mux := http.NewServeMux()
	(&server{}).registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := openapi.ValidateResponse(doc, http.MethodGet, "/health", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(/health) error = %v", err)
	}
}

func TestInternalAuthResolveUnauthorizedMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(openAPISpec)
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	mux := http.NewServeMux()
	platforminternal.Handler{Token: "internal-token"}.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/internal/auth/resolve status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if err := openapi.ValidateResponse(doc, http.MethodPost, "/internal/auth/resolve", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(/internal/auth/resolve) error = %v", err)
	}
}

func TestOpenAPIEndpointServesEmbeddedSpec(t *testing.T) {
	mux := http.NewServeMux()
	(&server{}).registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/openapi.yaml status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("Content-Type = %q, want application/yaml", got)
	}
	if len(rec.Body.Bytes()) == 0 {
		t.Fatal("expected non-empty OpenAPI document body")
	}
}
