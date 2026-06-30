package platformauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPUserKeyResolverCachesNegativeResults(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer internal-token" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false})
	}))
	defer server.Close()

	resolver := &HTTPUserKeyResolver{BaseURL: server.URL, Token: "internal-token"}
	for range 2 {
		_, ok, err := resolver.ResolveAPIKey(context.Background(), "missing-key")
		if err != nil || ok {
			t.Fatalf("ResolveAPIKey() ok=%v err=%v", ok, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

func TestHTTPUserKeyResolverRevalidatesSuccessfulAuth(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"principal": Principal{
					Subject: "user-1",
					Role:    RoleUser,
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false})
	}))
	defer server.Close()

	resolver := &HTTPUserKeyResolver{BaseURL: server.URL, Token: "internal-token"}
	principal, ok, err := resolver.ResolveAPIKey(context.Background(), "revoked-key")
	if err != nil || !ok || principal.Subject != "user-1" {
		t.Fatalf("first ResolveAPIKey() principal=%#v ok=%v err=%v", principal, ok, err)
	}

	_, ok, err = resolver.ResolveAPIKey(context.Background(), "revoked-key")
	if err != nil || ok {
		t.Fatalf("second ResolveAPIKey() ok=%v err=%v, want revoked key to revalidate as false", ok, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want 2", got)
	}
}
