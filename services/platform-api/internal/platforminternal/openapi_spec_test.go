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

func authorizedRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer internal-token")
	return req
}

func TestInternalEndpointsMatchOpenAPISpec(t *testing.T) {
	doc, err := openapi.Load(loadOpenAPISpec(t))
	if err != nil {
		t.Fatalf("Load(openAPISpec) error = %v", err)
	}
	handler := newTestServer(&fakeStore{})

	tests := []struct {
		name   string
		req    *http.Request
		path   string
		method string
	}{
		{
			name:   "unauthorized resolve",
			req:    httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`)),
			path:   "/internal/auth/resolve",
			method: http.MethodPost,
		},
		{
			name:   "resolve",
			req:    authorizedRequest(http.MethodPost, "/internal/auth/resolve", `{"api_key":"key"}`),
			path:   "/internal/auth/resolve",
			method: http.MethodPost,
		},
		{
			name:   "principal",
			req:    authorizedRequest(http.MethodPost, "/internal/identity/principal", `{"user_id":"user-1"}`),
			path:   "/internal/identity/principal",
			method: http.MethodPost,
		},
		{
			name:   "resolve-ids",
			req:    authorizedRequest(http.MethodPost, "/internal/identity/resolve-ids", `{"user_ids":["user-1"],"team_ids":["team-1"]}`),
			path:   "/internal/identity/resolve-ids",
			method: http.MethodPost,
		},
		{
			name:   "audit",
			req:    authorizedRequest(http.MethodPost, "/internal/audit", `{"action":"deploy","resource":"server","status":"success"}`),
			path:   "/internal/audit",
			method: http.MethodPost,
		},
		{
			name:   "teams list",
			req:    authorizedRequest(http.MethodGet, "/internal/identity/teams", ""),
			path:   "/internal/identity/teams",
			method: http.MethodGet,
		},
		{
			name:   "teams create",
			req:    authorizedRequest(http.MethodPost, "/internal/identity/teams", `{"slug":"beta","name":"Beta"}`),
			path:   "/internal/identity/teams",
			method: http.MethodPost,
		},
		{
			name:   "namespaces list",
			req:    authorizedRequest(http.MethodGet, "/internal/identity/namespaces", ""),
			path:   "/internal/identity/namespaces",
			method: http.MethodGet,
		},
		{
			name:   "create user",
			req:    authorizedRequest(http.MethodPost, "/internal/identity/users", `{"email":"user@example.com","password":"secret"}`),
			path:   "/internal/identity/users",
			method: http.MethodPost,
		},
		{
			name:   "operations snapshot",
			req:    authorizedRequest(http.MethodGet, "/internal/operations/snapshot", ""),
			path:   "/internal/operations/snapshot",
			method: http.MethodGet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, tt.req)
			if err := openapi.ValidateResponse(doc, tt.method, tt.path, rec.Code, rec.Body.Bytes(), "application/json"); err != nil {
				t.Fatalf("ValidateResponse(%s %s) status=%d error = %v body=%s", tt.method, tt.path, rec.Code, err, rec.Body.String())
			}
		})
	}
}
