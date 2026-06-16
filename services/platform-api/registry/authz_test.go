package registry

import (
	"net/http/httptest"
	"testing"
)

func TestRegistryForwardedPathPrefersRegistryURI(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/registry/authz", nil)
	req.Header.Set("X-Forwarded-Uri", "/api/v1/registry/authz")
	req.Header.Set("X-Forwarded-URL", "/v2/acme/demo/manifests/latest")

	got := RegistryForwardedPath(req)
	want := "/v2/acme/demo/manifests/latest"
	if got != want {
		t.Fatalf("RegistryForwardedPath() = %q, want %q", got, want)
	}
}

func TestRegistryForwardedPathUsesURIWhenRegistryPath(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/registry/authz", nil)
	req.Header.Set("X-Forwarded-Uri", "/v2/acme/demo/manifests/latest")

	got := RegistryForwardedPath(req)
	want := "/v2/acme/demo/manifests/latest"
	if got != want {
		t.Fatalf("RegistryForwardedPath() = %q, want %q", got, want)
	}
}
