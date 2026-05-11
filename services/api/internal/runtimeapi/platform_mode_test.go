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
