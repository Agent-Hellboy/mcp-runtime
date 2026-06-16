package platforminternal

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mcp-runtime/pkg/openapi"
)

func loadOpenAPISpec(t *testing.T) []byte {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "openapi.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(openapi.yaml) error = %v", err)
	}
	return data
}

func TestInternalUnauthorizedMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`))
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if err := openapi.ValidateResponse(doc, http.MethodPost, "/internal/auth/resolve", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(unauthorized) error = %v", err)
	}
}

func TestInternalAuthResolveMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := openapi.ValidateResponse(doc, http.MethodPost, "/internal/auth/resolve", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(resolve) error = %v", err)
	}
}

func TestInternalResolveIDsMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/identity/resolve-ids", bytes.NewBufferString(`{"user_ids":["user-1"],"team_ids":["team-1"]}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := openapi.ValidateResponse(doc, http.MethodPost, "/internal/identity/resolve-ids", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(resolve-ids) error = %v", err)
	}
}

func TestInternalTeamsListMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/internal/identity/teams", nil)
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := openapi.ValidateResponse(doc, http.MethodGet, "/internal/identity/teams", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(teams) error = %v", err)
	}
}

func TestInternalAuditMatchesOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/audit", bytes.NewBufferString(`{"action":"deploy","resource":"server","status":"success"}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := openapi.ValidateResponse(doc, http.MethodPost, "/internal/audit", rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
		t.Fatalf("ValidateResponse(audit) error = %v", err)
	}
}
