package server

import (
	"testing"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/metadata"
)

// server deploy must send only the auth values the user wrote in .mcp
// metadata; the operator derives an unset audience and issuer at reconcile.
func TestMergeDeployMetadataDoesNotDeriveOAuthAudience(t *testing.T) {
	spec := buildDeployServerSpec("buddy", "registry.example.com/acme/buddy", "v1", 1, 8088, 80)
	mergeDeployMetadata(&spec, &metadata.ServerMetadata{
		Name: "buddy",
		Auth: &metadata.AuthConfig{Mode: metadata.AuthModeOAuth},
	})
	if spec.Auth == nil || spec.Auth.Mode != mcpv1alpha1.AuthModeOAuth {
		t.Fatalf("auth = %+v, want oauth mode", spec.Auth)
	}
	if spec.Auth.Audience != "" || spec.Auth.IssuerURL != "" {
		t.Fatalf("auth = %+v, want audience and issuer unset for reconcile-time derivation", spec.Auth)
	}

	explicit := buildDeployServerSpec("buddy", "registry.example.com/acme/buddy", "v1", 1, 8088, 80)
	mergeDeployMetadata(&explicit, &metadata.ServerMetadata{
		Name: "buddy",
		Auth: &metadata.AuthConfig{Mode: metadata.AuthModeOAuth, Audience: "https://mcp.example.com/custom/mcp"},
	})
	if explicit.Auth.Audience != "https://mcp.example.com/custom/mcp" {
		t.Fatalf("explicit audience = %q, want kept", explicit.Auth.Audience)
	}
}
