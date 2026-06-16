package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcp-runtime-control/internal/runtimeapi"
	"mcp-runtime/pkg/authzmatrix"
	"mcp-runtime/pkg/platformauth"
)

func TestAuthzMatrixRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "security", "authz-matrix.json")
	rows, err := authzmatrix.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	rows = authzmatrix.Filter(rows, "runtime-control")
	if len(rows) == 0 {
		t.Fatal("expected runtime-control authz rows")
	}

	srv := &server{
		runtime: &runtimeapi.RuntimeServer{},
		authentic: platformauth.Authenticator{
			Secret:         []byte("test-secret"),
			Audience:       platformauth.AudienceRuntime,
			ServiceAPIKeys: map[string]struct{}{"test-user": {}, "test-admin": {}},
			AdminAPIKeys:   map[string]struct{}{"test-admin": {}},
		},
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	keys := map[string]string{
		authzmatrix.RoleUserKey:  "test-user",
		authzmatrix.RoleAdminKey: "test-admin",
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
