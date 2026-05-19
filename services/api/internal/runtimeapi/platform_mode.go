package runtimeapi

import (
	"os"
	"strings"

	"mcp-runtime/pkg/publishscope"
)

const (
	platformModeTenant = "tenant"
	platformModeOrg    = "org"
	platformModePublic = "public"

	defaultOrgCatalogNamespace    = publishscope.DefaultOrgCatalogNamespace
	defaultPublicCatalogNamespace = publishscope.DefaultPublicCatalogNamespace
)

func PlatformMode() string {
	raw := strings.TrimSpace(os.Getenv("PLATFORM_MODE"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_MODE"))
	}
	switch strings.ToLower(raw) {
	case platformModeOrg:
		return platformModeOrg
	case platformModePublic:
		return platformModePublic
	case "", platformModeTenant:
		return platformModeTenant
	default:
		return platformModeTenant
	}
}

func PublicCatalogEnabled() bool {
	return PlatformMode() == platformModePublic
}

func sharedCatalogWritableForUsers() bool {
	switch PlatformMode() {
	case platformModeOrg, platformModePublic:
		return true
	default:
		return false
	}
}

func modeCatalogNamespaces() []string {
	if PlatformMode() == platformModeTenant {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACES"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACES"))
	}
	if raw == "" && PlatformMode() == platformModePublic {
		raw = strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACES"))
		if raw == "" {
			raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_PUBLIC_NAMESPACES"))
		}
	}
	namespaces := []string{defaultModeCatalogNamespace()}
	for _, namespace := range strings.Split(raw, ",") {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	return dedupeNonEmptyStrings(namespaces)
}

func defaultModeCatalogNamespace() string {
	mode := PlatformMode()
	if mode == platformModeTenant {
		return ""
	}
	if override := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	if override := strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	switch mode {
	case platformModeOrg:
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_ORG_NAMESPACE")); namespace != "" {
			return namespace
		}
		return defaultOrgCatalogNamespace
	case platformModePublic:
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACE")); namespace != "" {
			return namespace
		}
		return defaultPublicCatalogNamespace
	default:
		return ""
	}
}

func defaultCatalogNamespaceForMode() string {
	namespaces := modeCatalogNamespaces()
	if len(namespaces) == 0 {
		return defaultModeCatalogNamespace()
	}
	return namespaces[0]
}

func isModeCatalogNamespace(namespace string) bool {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return false
	}
	for _, candidate := range modeCatalogNamespaces() {
		if candidate == namespace {
			return true
		}
	}
	return false
}

func principalCanReadNamespace(p principal, namespace string) bool {
	if sharedCatalogWritableForUsers() && isModeCatalogNamespace(namespace) {
		return true
	}
	return principalOwnsNamespace(p, namespace)
}

func principalOwnsNamespace(p principal, namespace string) bool {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return false
	}
	if strings.TrimSpace(p.Namespace) == namespace {
		return true
	}
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Namespace) == namespace {
			return true
		}
	}
	for _, allowed := range p.AllowedNamespaces {
		allowed = strings.TrimSpace(allowed)
		if allowed != "" && allowed == namespace {
			return true
		}
	}
	return false
}

func principalCanPublishNamespace(p principal, namespace string) bool {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return false
	}
	if sharedCatalogWritableForUsers() && isModeCatalogNamespace(namespace) {
		return true
	}
	return principalOwnsNamespace(p, namespace)
}

func publishNamespacesForPrincipal(p principal) []string {
	namespaces := make([]string, 0, len(p.AllowedNamespaces)+len(p.Teams)+3)
	if sharedCatalogWritableForUsers() {
		namespaces = append(namespaces, modeCatalogNamespaces()...)
	}
	if namespace := strings.TrimSpace(p.Namespace); namespace != "" {
		namespaces = append(namespaces, namespace)
	}
	for _, team := range p.Teams {
		if namespace := strings.TrimSpace(team.Namespace); namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	for _, namespace := range p.AllowedNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	return dedupeNonEmptyStrings(namespaces)
}

func PublicCatalogPrincipal() Principal {
	namespaces := modeCatalogNamespaces()
	return Principal{
		Role:              roleUser,
		Subject:           "public",
		Namespace:         defaultCatalogNamespaceForMode(),
		AllowedNamespaces: namespaces,
		AuthType:          "public_catalog",
	}
}
