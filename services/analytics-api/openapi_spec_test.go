package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-analytics-api/internal/analytics"
	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/openapi"
	"mcp-runtime/pkg/platformauth"
)

type eventReaderStub struct {
	rowCount int
}

func (eventReaderStub) QueryEvents(_ context.Context, limit, _ int) ([]clickhousepkg.EventRow, error) {
	rows := make([]clickhousepkg.EventRow, limit)
	return rows, nil
}
func (eventReaderStub) QueryStats(context.Context) (uint64, error) { return 0, nil }
func (eventReaderStub) QuerySources(context.Context) ([]clickhousepkg.SourceStat, error) {
	return nil, nil
}
func (eventReaderStub) QueryEventTypes(context.Context) ([]clickhousepkg.EventTypeStat, error) {
	return nil, nil
}
func (eventReaderStub) QueryEventsFiltered(context.Context, clickhousepkg.EventFilters) ([]clickhousepkg.EventRow, error) {
	return nil, nil
}

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

func TestStatsUnauthorizedMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(openAPISpec)
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	srv := &server{
		authentic: platformauth.Authenticator{
			Secret:   []byte("test-secret"),
			Audience: platformauth.AudienceAnalytics,
		},
		events: analytics.NewHandler(eventReaderStub{}),
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api/v1/stats status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if err := openapi.ValidateResponse(doc, http.MethodGet, "/api/v1/stats", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(/api/v1/stats) error = %v", err)
	}
}

func TestEventsInvalidCursorMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(openAPISpec)
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	srv := &server{
		authentic: platformauth.Authenticator{
			Secret:         []byte("test-secret"),
			Audience:       platformauth.AudienceAnalytics,
			ServiceAPIKeys: map[string]struct{}{"admin": {}},
			AdminAPIKeys:   map[string]struct{}{"admin": {}},
		},
		events: analytics.NewHandler(eventReaderStub{}),
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?cursor=bad", nil)
	req.Header.Set("x-api-key", "admin")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("/api/v1/events status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if err := openapi.ValidateResponse(doc, http.MethodGet, "/api/v1/events", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(/api/v1/events) error = %v", err)
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
