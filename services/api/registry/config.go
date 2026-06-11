package registry

import (
	"os"
	"strings"

	"mcp-runtime/pkg/publishscope"
)

// NewAuthzConfigFromEnv builds registry Traefik forward-auth settings from platform env.
func NewAuthzConfigFromEnv() *AuthzConfig {
	mode := platformModeFromEnv()
	catalogNamespaces := make(map[string]struct{})
	for _, namespace := range modeCatalogNamespacesFromEnv(mode) {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			catalogNamespaces[namespace] = struct{}{}
		}
	}
	return &AuthzConfig{
		mode:              mode,
		catalogNamespaces: catalogNamespaces,
	}
}

func platformModeFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("PLATFORM_MODE"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_MODE"))
	}
	switch strings.ToLower(raw) {
	case string(publishscope.Org):
		return string(publishscope.Org)
	case string(publishscope.Public):
		return string(publishscope.Public)
	case "", "tenant":
		return "tenant"
	default:
		return "tenant"
	}
}

func modeCatalogNamespacesFromEnv(mode string) []string {
	switch mode {
	case "tenant":
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACES"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACES"))
	}
	if raw == "" && mode == string(publishscope.Public) {
		raw = strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACES"))
		if raw == "" {
			raw = strings.TrimSpace(os.Getenv("MCP_PLATFORM_PUBLIC_NAMESPACES"))
		}
	}
	namespaces := []string{defaultModeCatalogNamespace(mode)}
	for _, namespace := range strings.Split(raw, ",") {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	return dedupeNonEmptyStrings(namespaces)
}

func defaultModeCatalogNamespace(mode string) string {
	if mode == "tenant" {
		return ""
	}
	if override := strings.TrimSpace(os.Getenv("PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	if override := strings.TrimSpace(os.Getenv("MCP_PLATFORM_CATALOG_NAMESPACE")); override != "" {
		return override
	}
	switch mode {
	case string(publishscope.Org):
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_ORG_NAMESPACE")); namespace != "" {
			return namespace
		}
		return publishscope.DefaultOrgCatalogNamespace
	case string(publishscope.Public):
		if namespace := strings.TrimSpace(os.Getenv("PLATFORM_PUBLIC_NAMESPACE")); namespace != "" {
			return namespace
		}
		return publishscope.DefaultPublicCatalogNamespace
	default:
		return ""
	}
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
