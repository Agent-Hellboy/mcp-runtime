package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSplitCSVSet(t *testing.T) {
	got := splitCSVSet(" a, b,,c , ")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if _, ok := got["a"]; !ok {
		t.Fatal("missing a")
	}
}

func TestAuthenticateRequestStaticKey_DefaultAdmin(t *testing.T) {
	srv := &apiServer{
		apiKeys: map[string]struct{}{"key-1": {}},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("x-api-key", "key-1")
	p, ok, err := srv.authenticateRequest(req)
	if err != nil {
		t.Fatalf("authenticateRequest error: %v", err)
	}
	if !ok {
		t.Fatal("expected authenticated")
	}
	if p.Role != roleAdmin {
		t.Fatalf("role = %q, want %q", p.Role, roleAdmin)
	}
}

func TestAuthenticateRequestStaticKey_AdminAllowlist(t *testing.T) {
	srv := &apiServer{
		apiKeys:      map[string]struct{}{"key-user": {}, "key-admin": {}},
		adminAPIKeys: map[string]struct{}{"key-admin": {}},
	}

	userReq := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	userReq.Header.Set("x-api-key", "key-user")
	p, ok, err := srv.authenticateRequest(userReq)
	if err != nil || !ok {
		t.Fatalf("user auth failed: ok=%v err=%v", ok, err)
	}
	if p.Role != roleUser {
		t.Fatalf("user role = %q, want %q", p.Role, roleUser)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	adminReq.Header.Set("x-api-key", "key-admin")
	p, ok, err = srv.authenticateRequest(adminReq)
	if err != nil || !ok {
		t.Fatalf("admin auth failed: ok=%v err=%v", ok, err)
	}
	if p.Role != roleAdmin {
		t.Fatalf("admin role = %q, want %q", p.Role, roleAdmin)
	}
}

func TestRequireRole(t *testing.T) {
	srv := &apiServer{}
	handler := srv.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalContextKey{}, principal{Role: roleUser}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
