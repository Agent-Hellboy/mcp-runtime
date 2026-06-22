package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-analytics-api/internal/analytics"
	"mcp-analytics-api/internal/usage"
)

func TestRouteOwnershipV1Only(t *testing.T) {
	mux := http.NewServeMux()
	(&server{
		usage:  &usage.Service{},
		events: analytics.NewHandler(nil),
	}).registerRoutes(mux)

	for _, path := range []string{
		"/api/stats",
		"/api/events",
		"/api/user/analytics/usage",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("legacy path %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	for _, path := range []string{
		"/api/v1/runtime/servers",
		"/api/v1/auth/login",
		"/api/v1/admin/audit",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("foreign path %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatalf("owned path /api/v1/stats should be registered")
	}
}
