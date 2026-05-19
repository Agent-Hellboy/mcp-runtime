package publishscope

import (
	"fmt"
	"strings"
)

type Scope string

const (
	Tenant Scope = "tenant"
	Org    Scope = "org"
	Public Scope = "public"

	DefaultOrgCatalogNamespace    = "mcp-servers-org"
	DefaultPublicCatalogNamespace = "mcp-servers-public"

	OrgRegistryAlias    = "org"
	PublicRegistryAlias = "public"
)

func Normalize(raw string) (Scope, error) {
	value := Scope(strings.ToLower(strings.TrimSpace(raw)))
	switch value {
	case "":
		return "", nil
	case Tenant, Org, Public:
		return value, nil
	default:
		return "", fmt.Errorf("scope %q is invalid (use tenant, org, or public)", raw)
	}
}

func CatalogNamespace(scope Scope) (string, bool) {
	switch scope {
	case Org:
		return DefaultOrgCatalogNamespace, true
	case Public:
		return DefaultPublicCatalogNamespace, true
	default:
		return "", false
	}
}

func RegistryAlias(scope Scope) (string, bool) {
	switch scope {
	case Org:
		return OrgRegistryAlias, true
	case Public:
		return PublicRegistryAlias, true
	default:
		return "", false
	}
}
