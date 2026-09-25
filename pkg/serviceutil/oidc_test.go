package serviceutil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func oidcDiscoveryServer(t *testing.T, handler func(w http.ResponseWriter, issuer string)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		handler(w, server.URL)
	}))
	t.Cleanup(server.Close)
	return server
}

func writeDiscovery(w http.ResponseWriter, issuer, jwksURI string) {
	_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "jwks_uri": jwksURI})
}

func TestDiscoverOIDCJWKSURLReturnsJWKSURI(t *testing.T) {
	server := oidcDiscoveryServer(t, func(w http.ResponseWriter, issuer string) {
		writeDiscovery(w, issuer, issuer+"/keys")
	})
	got, err := DiscoverOIDCJWKSURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DiscoverOIDCJWKSURL: %v", err)
	}
	if got != server.URL+"/keys" {
		t.Fatalf("jwks = %q", got)
	}
}

func TestDiscoverOIDCJWKSURLRejectsIssuerMismatch(t *testing.T) {
	server := oidcDiscoveryServer(t, func(w http.ResponseWriter, _ string) {
		writeDiscovery(w, "https://other.example", "https://other.example/keys")
	})
	if _, err := DiscoverOIDCJWKSURL(context.Background(), server.URL); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want issuer mismatch", err)
	}
}

func TestDiscoverOIDCJWKSURLRequiresHTTPSKeysForHTTPSIssuer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(server.Close)
	issuer := server.URL
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDiscovery(w, issuer, "http://keys.example/jwks")
	})
	// The helper builds its own client, so trust the test CA through the
	// default transport for this call.
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	if _, err := DiscoverOIDCJWKSURL(context.Background(), issuer); err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("err = %v, want an https jwks_uri requirement", err)
	}
}

func TestDiscoverOIDCJWKSURLWithRetryRecoversFromTransientFailures(t *testing.T) {
	var calls int32
	server := oidcDiscoveryServer(t, func(w http.ResponseWriter, issuer string) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeDiscovery(w, issuer, issuer+"/keys")
	})
	got, err := DiscoverOIDCJWKSURLWithRetry(context.Background(), server.URL, 5, time.Millisecond)
	if err != nil {
		t.Fatalf("DiscoverOIDCJWKSURLWithRetry: %v", err)
	}
	if got != server.URL+"/keys" || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("jwks = %q after %d calls, want success on the third", got, calls)
	}
}

func TestDiscoverOIDCJWKSURLWithRetryDoesNotRetryConfigurationErrors(t *testing.T) {
	var calls int32
	server := oidcDiscoveryServer(t, func(w http.ResponseWriter, _ string) {
		atomic.AddInt32(&calls, 1)
		writeDiscovery(w, "https://other.example", "https://other.example/keys")
	})
	if _, err := DiscoverOIDCJWKSURLWithRetry(context.Background(), server.URL, 5, time.Millisecond); err == nil {
		t.Fatal("expected issuer mismatch error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1: a mismatched issuer is not transient", calls)
	}
}

func TestRefuseOIDCDowngradeBlocksHTTPSToHTTP(t *testing.T) {
	httpsReq, _ := http.NewRequest(http.MethodGet, "https://idp.example/.well-known/openid-configuration", nil)
	httpReq, _ := http.NewRequest(http.MethodGet, "http://idp.example/.well-known/openid-configuration", nil)
	if err := refuseOIDCDowngrade(httpReq, []*http.Request{httpsReq}); err == nil {
		t.Fatal("an https to http redirect must be refused")
	}
	if err := refuseOIDCDowngrade(httpsReq, []*http.Request{httpsReq}); err != nil {
		t.Fatalf("an https to https redirect should be allowed: %v", err)
	}
}
