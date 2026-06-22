package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-runtime-api/internal/runtimeapi"
)

func TestRouteOwnershipV1Only(t *testing.T) {
	mux := http.NewServeMux()
	(&server{runtime: &runtimeapi.RuntimeServer{}}).registerRoutes(mux)

	for _, path := range []string{
		"/api/runtime/servers",
		"/api/deployments",
		"/api/user/api-keys",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("legacy path %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	for _, path := range []string{
		"/api/v1/auth/login",
		"/api/v1/stats",
		"/api/v1/admin/namespaces",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("foreign path %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/runtime/servers", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatalf("owned path /api/v1/runtime/servers should be registered")
	}
}
