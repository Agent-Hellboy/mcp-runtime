package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcp-analytics-api/internal/analytics"
	"mcp-runtime/pkg/authzmatrix"
	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/platformauth"
)

type matrixEventStub struct{}

func (matrixEventStub) QueryEvents(_ context.Context, limit, _ int) ([]clickhousepkg.EventRow, error) {
	return make([]clickhousepkg.EventRow, limit), nil
}
func (matrixEventStub) QueryStats(context.Context) (uint64, error) { return 0, nil }
func (matrixEventStub) QuerySources(context.Context) ([]clickhousepkg.SourceStat, error) {
	return nil, nil
}
func (matrixEventStub) QueryEventTypes(context.Context) ([]clickhousepkg.EventTypeStat, error) {
	return nil, nil
}
func (matrixEventStub) QueryEventsFiltered(context.Context, clickhousepkg.EventFilters) ([]clickhousepkg.EventRow, error) {
	return nil, nil
}

func TestAuthzMatrixRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "security", "authz-matrix.json")
	rows, err := authzmatrix.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	rows = authzmatrix.Filter(rows, "analytics-api")
	if len(rows) == 0 {
		t.Fatal("expected analytics-api authz rows")
	}

	srv := &server{
		authentic: platformauth.Authenticator{
			Secret:         []byte("test-secret"),
			Audience:       platformauth.AudienceAnalytics,
			ServiceAPIKeys: map[string]struct{}{"test-user": {}, "test-admin": {}},
			AdminAPIKeys:   map[string]struct{}{"test-admin": {}},
		},
		events: analytics.NewHandler(matrixEventStub{}),
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	keys := map[string]string{
		authzmatrix.RoleUserKey:   "test-user",
		authzmatrix.RoleAdminKey:  "test-admin",
		authzmatrix.RoleIngestKey: "test-ingest",
	}

	for _, row := range rows {
		row := row
		t.Run(row.Path+"_"+row.Role, func(t *testing.T) {
			req := httptest.NewRequest(row.Method, row.Path, nil)
			authzmatrix.ApplyRole(req, row.Role, keys)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != row.Expect {
				t.Fatalf("%s %s role=%s status = %d, want %d body=%s", row.Method, row.Path, row.Role, rec.Code, row.Expect, rec.Body.String())
			}
		})
	}
}
