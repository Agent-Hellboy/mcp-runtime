package registry

import (
	"net/http"
	"testing"
)

func TestParseRegistryCredentialItemPathV1Delete(t *testing.T) {
	id, allowed, valid := parseRegistryCredentialItemPath(
		http.MethodDelete,
		"/api/v1/user/registry-credentials/rk_d95f9bd5-8bc0-4f78-9ed6-e4176fd38ac5",
	)
	if !allowed || !valid {
		t.Fatalf("allowed=%v valid=%v, want true true", allowed, valid)
	}
	if id != "rk_d95f9bd5-8bc0-4f78-9ed6-e4176fd38ac5" {
		t.Fatalf("id = %q, want credential id", id)
	}
}

func TestParseRegistryCredentialItemPathLegacyDelete(t *testing.T) {
	id, allowed, valid := parseRegistryCredentialItemPath(
		http.MethodDelete,
		"/api/user/registry-credentials/rk_test",
	)
	if !allowed || !valid {
		t.Fatalf("allowed=%v valid=%v, want true true", allowed, valid)
	}
	if id != "rk_test" {
		t.Fatalf("id = %q, want rk_test", id)
	}
}

func TestParseRegistryCredentialItemPathV1Revoke(t *testing.T) {
	id, allowed, valid := parseRegistryCredentialItemPath(
		http.MethodPost,
		"/api/v1/user/registry-credentials/rk_test/revoke",
	)
	if !allowed || !valid {
		t.Fatalf("allowed=%v valid=%v, want true true", allowed, valid)
	}
	if id != "rk_test" {
		t.Fatalf("id = %q, want rk_test", id)
	}
}
