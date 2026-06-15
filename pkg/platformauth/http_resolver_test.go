package platformauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPUserKeyResolverCachesByKeyHash(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer internal-token" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"principal": Principal{
				Subject:           "user-1",
				Email:             "user@example.com",
				Teams:             []PrincipalTeam{{Slug: "core", Namespace: "mcp-team-core"}},
				AllowedNamespaces: []string{"mcp-team-core"},
			},
		})
	}))
	defer server.Close()

	resolver := &HTTPUserKeyResolver{BaseURL: server.URL, Token: "internal-token"}
	for range 2 {
		principal, ok, err := resolver.ResolveAPIKey(context.Background(), "secret-key")
		if err != nil || !ok {
			t.Fatalf("ResolveAPIKey() ok=%v err=%v", ok, err)
		}
		if principal.Teams[0].Slug != "core" {
			t.Fatalf("principal = %#v", principal)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}
