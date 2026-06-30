package authzmatrix

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestLoadAndFilter(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "security", "authz-matrix.json")
	rows, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected non-empty authz matrix")
	}
	for _, service := range []string{"platform-api", "runtime-api", "analytics-api"} {
		if len(Filter(rows, service)) == 0 {
			t.Fatalf("expected rows for service %q", service)
		}
	}
}

func TestApplyRoleSetsAPIKeyHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	ApplyRole(req, RoleAdminKey, map[string]string{RoleAdminKey: "admin-secret"})
	if got := req.Header.Get("x-api-key"); got != "admin-secret" {
		t.Fatalf("x-api-key = %q, want admin-secret", got)
	}
}
