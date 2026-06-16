package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcp-runtime/pkg/authzmatrix"
)

func TestAuthzMatrixRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "security", "authz-matrix.json")
	rows, err := authzmatrix.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	rows = authzmatrix.Filter(rows, "platform-api")
	if len(rows) == 0 {
		t.Fatal("expected platform-api authz rows")
	}

	srv := &apiServer{
		apiKeys:      map[string]struct{}{"test-user": {}, "test-admin": {}},
		adminAPIKeys: map[string]struct{}{"test-admin": {}},
	}
	testAuthenticator(srv)

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
