package main

import (
	"context"
	"errors"
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

func TestAuthenticateRequestStaticKey_DefaultUserWhenAdminKeysUnset(t *testing.T) {
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
	if p.Role != roleUser {
		t.Fatalf("role = %q, want %q", p.Role, roleUser)
	}
}

func TestAuthenticateRequestStaticKey_LegacyAdminFallback(t *testing.T) {
	srv := &apiServer{
		apiKeys:         map[string]struct{}{"key-1": {}},
		legacyAdminKeys: true,
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

func TestLegacyAdminAPIKeyFallbackEnabledOnlyForExplicitDevTest(t *testing.T) {
	t.Setenv("MCP_RUNTIME_TEST_MODE", "")
	t.Setenv("MCP_LEGACY_ADMIN_API_KEY_FALLBACK", "")
	t.Setenv("LEGACY_ADMIN_API_KEY_FALLBACK", "")
	if legacyAdminAPIKeyFallbackEnabled() {
		t.Fatal("legacy fallback should be disabled by default")
	}

	t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
	if !legacyAdminAPIKeyFallbackEnabled() {
		t.Fatal("legacy fallback should be enabled in runtime test mode")
	}

	t.Setenv("MCP_RUNTIME_TEST_MODE", "")
	t.Setenv("MCP_LEGACY_ADMIN_API_KEY_FALLBACK", "true")
	if !legacyAdminAPIKeyFallbackEnabled() {
		t.Fatal("legacy fallback should honor explicit override")
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
	req = req.WithContext(withPrincipal(req.Context(), principal{Role: roleUser}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAuthOrPublicCatalogAllowsAnonymousPublicServerList(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	srv := &apiServer{}
	handler := srv.authOrPublicCatalog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFromContext(r.Context())
		if !ok {
			t.Fatal("expected public principal")
		}
		if p.AuthType != "public_catalog" || p.Namespace != "mcp-servers-public" {
			t.Fatalf("principal = %+v, want public catalog in mcp-servers-public", p)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runtime/servers", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type fakeRegistryCredentialAuth struct {
	principal principal
	ok        bool
	err       error
	username  string
	password  string
}

func (f *fakeRegistryCredentialAuth) AuthenticateRegistryCredential(_ context.Context, username, password string) (principal, bool, error) {
	f.username = username
	f.password = password
	return f.principal, f.ok, f.err
}

func TestRegistryAuthzAllowsStaticAdminKey(t *testing.T) {
	srv := &apiServer{
		apiKeys:      map[string]struct{}{"admin-key": {}, "user-key": {}},
		adminAPIKeys: map[string]struct{}{"admin-key": {}},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "admin-key")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "user-key")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRegistryAuthzRejectsUserAPIKeyForPersonalRepositoryScope(t *testing.T) {
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:              roleUser,
				Subject:           "user-1",
				Namespace:         "user-1",
				AllowedNamespaces: []string{"user-1"},
				AuthType:          "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/user-1/demo/blobs/uploads/")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzAllowsUserAPIKeyForTeamRepositoryScope(t *testing.T) {
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:      roleUser,
				Subject:   "user-1",
				Namespace: "mcp-team-acme",
				Teams: []principalTeam{{
					Slug:      "acme",
					Namespace: "mcp-team-acme",
					Role:      teamRoleMember,
				}},
				AuthType: "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/acme/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/mcp-team-acme/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("namespace status = %d body = %s, want 204", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzAllowsPublicAliasInPublicMode(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:      roleUser,
				Subject:   "user-1",
				Namespace: "user-1",
				AuthType:  "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/public/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzCachesPlatformModePerServer(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:      roleUser,
				Subject:   "user-1",
				Namespace: "user-1",
				AuthType:  "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/public/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("initial status = %d body = %s, want 204", rec.Code, rec.Body.String())
	}

	t.Setenv("PLATFORM_MODE", "tenant")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/public/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cached status = %d body = %s, want 204", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzRejectsPublicAliasInTenantMode(t *testing.T) {
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:      roleUser,
				Subject:   "user-1",
				Namespace: "user-1",
				AuthType:  "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/public/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzRejectsUserAPIKeyForOtherRepositoryScope(t *testing.T) {
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok: true,
			principal: principal{
				Role:              roleUser,
				Subject:           "user-1",
				Namespace:         "user-1",
				AllowedNamespaces: []string{"user-1"},
				AuthType:          "user_api_key",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/other/demo/manifests/latest")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzRejectsUserAPIKeyForCatalog(t *testing.T) {
	srv := &apiServer{
		userKeys: &fakeUserAPIKeyStore{
			ok:        true,
			principal: principal{Role: roleUser, Subject: "user-1", Namespace: "user-1", AuthType: "user_api_key"},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.Header.Set("x-api-key", "mcpu_user")
	req.Header.Set("X-Forwarded-Uri", "/v2/_catalog")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestRegistryAuthzChallengesAnonymousRequests(t *testing.T) {
	srv := &apiServer{}

	rec := httptest.NewRecorder()
	srv.handleRegistryAuthz(rec, httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != registryAuthChallenge {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, registryAuthChallenge)
	}
}

func TestRegistryAuthzAllowsAdminRegistryCredential(t *testing.T) {
	authn := &fakeRegistryCredentialAuth{
		principal: principal{Role: roleAdmin, Subject: "admin-user", AuthType: "registry_basic"},
		ok:        true,
	}
	srv := &apiServer{registryAuth: authn}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.SetBasicAuth("user-1", "registry-secret")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if authn.username != "user-1" || authn.password != "registry-secret" {
		t.Fatalf("basic credentials = %q/%q, want user-1/registry-secret", authn.username, authn.password)
	}
}

func TestRegistryAuthzRejectsNonAdminRegistryCredential(t *testing.T) {
	authn := &fakeRegistryCredentialAuth{
		principal: principal{Role: roleUser, Subject: "user"},
		ok:        true,
	}
	srv := &apiServer{registryAuth: authn}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.SetBasicAuth("user-1", "registry-secret")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRegistryAuthzReportsAuthErrors(t *testing.T) {
	srv := &apiServer{registryAuth: &fakeRegistryCredentialAuth{err: errors.New("store unavailable")}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/authz", nil)
	req.SetBasicAuth("user-1", "registry-secret")
	srv.handleRegistryAuthz(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
