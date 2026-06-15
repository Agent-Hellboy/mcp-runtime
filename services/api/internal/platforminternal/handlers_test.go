package platforminternal

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-runtime/pkg/platformauth"
	"mcp-sentinel-api/internal/platformstore"
)

type fakeStore struct {
	audit platformstore.AuditEvent
}

func (f *fakeStore) AuthenticateUserAPIKey(context.Context, string) (platformauth.Principal, bool, error) {
	return platformauth.Principal{
		Subject:           "user-1",
		Email:             "user@example.com",
		Teams:             []platformauth.PrincipalTeam{{ID: "team-1", Slug: "core", Namespace: "mcp-team-core", Role: "owner"}},
		AllowedNamespaces: []string{"mcp-team-core", "mcp-servers"},
		AuthType:          "user_api_key",
		APIKeyID:          "key-1",
	}, true, nil
}

func (f *fakeStore) ResolveUserIDs(context.Context, []string) (map[string]string, error) {
	return map[string]string{"user-1": "user@example.com"}, nil
}

func (f *fakeStore) ResolveTeamIDs(context.Context, []string) (map[string]string, error) {
	return map[string]string{"team-1": "core"}, nil
}

func (f *fakeStore) WriteAudit(_ context.Context, event platformstore.AuditEvent) {
	f.audit = event
}

func (f *fakeStore) CreateTeam(_ context.Context, slug, name, _ string) (platformstore.Team, error) {
	return platformstore.Team{ID: "team-1", Slug: slug, Name: name, Namespace: "mcp-team-" + slug}, nil
}

func (f *fakeStore) ListTeams(context.Context) ([]platformstore.Team, error) {
	return []platformstore.Team{{ID: "team-1", Slug: "core", Name: "Core", Namespace: "mcp-team-core"}}, nil
}

func (f *fakeStore) GetTeamBySlug(_ context.Context, slug string) (platformstore.Team, bool, error) {
	return platformstore.Team{ID: "team-1", Slug: slug, Name: "Core", Namespace: "mcp-team-core"}, true, nil
}

func (f *fakeStore) DeleteTeamBySlug(context.Context, string) error {
	return nil
}

func (f *fakeStore) ListNamespaces(context.Context) ([]map[string]any, error) {
	return []map[string]any{{"namespace": "mcp-team-core"}}, nil
}

func (f *fakeStore) GetNamespace(_ context.Context, namespace string) (map[string]any, bool, error) {
	return map[string]any{"namespace": namespace}, true, nil
}

func (f *fakeStore) ListTeamMemberships(context.Context, string) ([]platformstore.TeamMembership, error) {
	return nil, nil
}

func (f *fakeStore) UpsertTeamMembership(context.Context, string, string, string) (platformstore.TeamMembership, error) {
	return platformstore.TeamMembership{}, nil
}

func (f *fakeStore) DeleteTeamMembership(context.Context, string, string) error {
	return nil
}

func (f *fakeStore) CreatePasswordUser(context.Context, string, string, string) (platformstore.User, error) {
	return platformstore.User{ID: "user-1", Email: "user@example.com", Role: platformstore.RoleUser}, nil
}

func (f *fakeStore) OperationsSnapshot(context.Context, platformstore.OperationsFilter) (platformstore.OperationsSnapshot, error) {
	return platformstore.OperationsSnapshot{}, nil
}

func newTestServer(store PlatformStore) http.Handler {
	mux := http.NewServeMux()
	Handler{Store: store, Token: "internal-token"}.Register(mux)
	return mux
}

func TestInternalEndpointsRequireBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`))
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestResolveAuthReturnsEnrichedPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/internal/auth/resolve", bytes.NewBufferString(`{"api_key":"key"}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"slug":"core"`, `"allowed_namespaces":["mcp-team-core","mcp-servers"]`, `"api_key_id":"key-1"`} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("body %s missing %s", rec.Body.String(), want)
		}
	}
}

func TestResolveIDsAndAudit(t *testing.T) {
	store := &fakeStore{}
	handler := newTestServer(store)
	req := httptest.NewRequest(http.MethodPost, "/internal/identity/resolve-ids", bytes.NewBufferString(`{"user_ids":["user-1"],"team_ids":["team-1"]}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"core"`)) {
		t.Fatalf("resolve status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/audit", bytes.NewBufferString(`{"user_id":"user-1","action":"deploy","resource":"server","status":"success"}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("audit status = %d", rec.Code)
	}
	if store.audit.UserID != "user-1" || store.audit.Action != "deploy" {
		t.Fatalf("audit = %#v", store.audit)
	}
}

func TestTeamAndNamespaceRoutes(t *testing.T) {
	handler := newTestServer(&fakeStore{})
	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/internal/identity/teams", "", http.StatusOK},
		{http.MethodPost, "/internal/identity/teams", `{"slug":"core","name":"Core"}`, http.StatusCreated},
		{http.MethodGet, "/internal/identity/teams/core", "", http.StatusOK},
		{http.MethodDelete, "/internal/identity/teams/core", "", http.StatusNoContent},
		{http.MethodGet, "/internal/identity/namespaces", "", http.StatusOK},
		{http.MethodGet, "/internal/identity/namespaces/mcp-team-core", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Authorization", "Bearer internal-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
