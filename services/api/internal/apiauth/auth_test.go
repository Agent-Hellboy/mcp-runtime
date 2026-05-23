package apiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIPUsesRemoteAddrWithoutForwardedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:4321"

	if got := RequestIP(req); got != "203.0.113.10" {
		t.Fatalf("RequestIP() = %q, want remote address", got)
	}
}

func TestRequestIPIgnoresSpoofedForwardedForFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.25")

	if got := RequestIP(req); got != "203.0.113.10" {
		t.Fatalf("RequestIP() = %q, want untrusted remote address", got)
	}
}

func TestRequestIPReturnsLastUntrustedForwardedHop(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.99, 198.51.100.25, 10.0.0.11")

	if got := RequestIP(req); got != "198.51.100.25" {
		t.Fatalf("RequestIP() = %q, want last untrusted forwarded address", got)
	}
}

func TestRequestIPExplicitTrustedProxyCIDRsOverridePrivateDefaults(t *testing.T) {
	t.Setenv("PLATFORM_TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.10.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.25")

	if got := RequestIP(req); got != "192.168.10.10" {
		t.Fatalf("RequestIP() = %q, want remote address outside explicit trusted CIDRs", got)
	}
}
