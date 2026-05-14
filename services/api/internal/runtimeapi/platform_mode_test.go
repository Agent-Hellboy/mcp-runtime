package runtimeapi

import "testing"

func TestDefaultCatalogNamespaceForModeIgnoresCatalogOverrideInTenantMode(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	t.Setenv("PLATFORM_CATALOG_NAMESPACE", "custom-catalog")

	if got := defaultCatalogNamespaceForMode(); got != "" {
		t.Fatalf("default catalog namespace = %q, want empty tenant namespace", got)
	}
}

func TestDefaultCatalogNamespaceForModeUsesCatalogOverrideInSharedMode(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "org")
	t.Setenv("PLATFORM_CATALOG_NAMESPACE", "custom-catalog")

	if got := defaultCatalogNamespaceForMode(); got != "custom-catalog" {
		t.Fatalf("default catalog namespace = %q, want custom-catalog", got)
	}
}

func TestPlatformModeUsesStrictValues(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenent")

	if got := PlatformMode(); got != platformModeTenant {
		t.Fatalf("platform mode = %q, want tenant fallback", got)
	}
}

func TestPublicModePrincipalCanReadCatalogAndOwnedNamespaces(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	p := principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}
	if !principalCanReadNamespace(p, defaultPublicCatalogNamespace) {
		t.Fatalf("expected user to read public catalog namespace %q", defaultPublicCatalogNamespace)
	}
	if !principalCanReadNamespace(p, "user-1") {
		t.Fatal("expected user to read owned namespace in public mode")
	}
	if principalCanReadNamespace(p, "other-user") {
		t.Fatal("did not expect user to read unrelated namespace")
	}
}
