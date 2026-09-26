package runtimeapi

import (
	"fmt"
	"net/http"
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

// PlatformMode returns the configured platform tenancy mode, defaulting invalid or empty values to tenant mode.
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

// enabledPublishScopes lists the publish scopes the configured platform mode
// accepts. Tenant scope is always available; org and public need the
// matching platform mode.
func enabledPublishScopes() []publishscope.Scope {
	switch PlatformMode() {
	case platformModeOrg:
		return []publishscope.Scope{publishscope.Tenant, publishscope.Org}
	case platformModePublic:
		return []publishscope.Scope{publishscope.Tenant, publishscope.Public}
	default:
		return []publishscope.Scope{publishscope.Tenant}
	}
}

// publishScopeEnabledError returns an actionable error when scope is not
// enabled by the platform mode. Registry push and server deploy share it so
// an image can only be published to a scope it can also be deployed from.
func publishScopeEnabledError(scope publishscope.Scope) error {
	if scope == "" {
		return nil
	}
	enabled := enabledPublishScopes()
	names := make([]string, 0, len(enabled))
	for _, candidate := range enabled {
		if candidate == scope {
			return nil
		}
		names = append(names, string(candidate))
	}
	return fmt.Errorf("%s scope is not enabled on this platform (platform mode %q; enabled scopes: %s); pass --scope tenant, or omit --scope and set scope: tenant in .mcp metadata to publish to your team namespace",
		scope, PlatformMode(), strings.Join(names, ", "))
}

// PublicCatalogEnabled reports whether the runtime should expose public catalog behavior.
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

// PublicCatalogPrincipal returns the synthetic principal used for unauthenticated public catalog reads.
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

// PublicCatalogFallback authenticates anonymous public-mode reads of the catalog collections.
func PublicCatalogFallback(r *http.Request) (Principal, bool) {
	if !PublicCatalogEnabled() || r == nil || r.Method != http.MethodGet {
		return Principal{}, false
	}
	switch strings.TrimRight(r.URL.Path, "/") {
	case "/api/v1/runtime/servers", "/api/v1/runtime/tools":
	default:
		return Principal{}, false
	}
	query := r.URL.Query()
	if strings.TrimSpace(query.Get("namespace")) == "" {
		query.Set("namespace", defaultCatalogNamespaceForMode())
		r.URL.RawQuery = query.Encode()
	}
	return PublicCatalogPrincipal(), true
}
