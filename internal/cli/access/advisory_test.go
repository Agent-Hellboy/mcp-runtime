package access

import (
	"strings"
	"testing"
)

func TestAccessManifestWarningsWarnsForSuspiciousHumanID(t *testing.T) {
	manifest := []byte(`
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: suspicious-grant
  namespace: mcp-team-acme
spec:
  serverRef: {name: demo}
  subject:
    humanID: Alice@team-B
    agentID: acme-bot
`)

	warnings, err := accessManifestWarnings(manifest)
	if err != nil {
		t.Fatalf("accessManifestWarnings() error = %v", err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"suspicious-grant", "email identifier", "case-sensitive"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected warning containing %q, got %q", want, joined)
		}
	}
}

func TestAccessManifestWarningsAllowsOpaqueHumanID(t *testing.T) {
	manifest := []byte(`
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: local-session
spec:
  serverRef: {name: demo}
  subject:
    humanID: user-123
`)

	warnings, err := accessManifestWarnings(manifest)
	if err != nil {
		t.Fatalf("accessManifestWarnings() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}
