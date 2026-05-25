package runtimeapi

import (
	"testing"

	"mcp-runtime/pkg/publishscope"
)

func TestRegistryPushAuthContextTenantRequiresTeamScope(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	p := principal{
		Role: roleUser,
		Teams: []principalTeam{{
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      "owner",
		}},
	}
	_, _, err := registryPushAuthContext("registry.example.com/beta/demo:v1", publishscope.Tenant, p)
	if err == nil {
		t.Fatal("expected forbidden team scope")
	}
}

func TestRegistryPushAuthContextTenantAllowsOwnedTeam(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	p := principal{
		Role: roleUser,
		Teams: []principalTeam{{
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      "owner",
		}},
	}
	namespace, teamSlug, err := registryPushAuthContext("registry.example.com/acme/demo:v1", publishscope.Tenant, p)
	if err != nil {
		t.Fatalf("registryPushAuthContext() error = %v", err)
	}
	if namespace != "mcp-team-acme" || teamSlug != "acme" {
		t.Fatalf("namespace=%q teamSlug=%q", namespace, teamSlug)
	}
}

func TestRegistryPushAuthContextPublicRequiresCatalogWrite(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	p := principal{Role: roleUser}
	if _, _, err := registryPushAuthContext("registry.example.com/public/demo:v1", publishscope.Public, p); err == nil {
		t.Fatal("expected public catalog write rejection in tenant mode")
	}
}

func TestRegistryPushAuthContextPublicAllowsWhenCatalogWritable(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	p := principal{Role: roleUser, AllowedNamespaces: []string{defaultPublicCatalogNamespace}}
	namespace, teamSlug, err := registryPushAuthContext("registry.example.com/public/demo:v1", publishscope.Public, p)
	if err != nil {
		t.Fatalf("registryPushAuthContext() error = %v", err)
	}
	if namespace != defaultPublicCatalogNamespace || teamSlug != "" {
		t.Fatalf("namespace=%q teamSlug=%q", namespace, teamSlug)
	}
}

func TestValidateDeployImageRejectsCrossTeamPushTarget(t *testing.T) {
	t.Setenv("MCP_REGISTRY_INGRESS_HOST", "registry.example.com")
	p := principal{
		Role: roleUser,
		Teams: []principalTeam{{
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      "owner",
		}},
	}
	target := "registry.example.com/beta/demo:v1"
	namespace, teamSlug, err := registryPushAuthContext(target, publishscope.Tenant, p)
	if err == nil {
		t.Fatalf("expected auth context error, got namespace=%q teamSlug=%q", namespace, teamSlug)
	}
	if err := ValidateDeployImage(target, "mcp-team-acme", "acme", p.Role); err == nil {
		t.Fatal("expected deploy image validation failure for cross-team repo")
	}
}
