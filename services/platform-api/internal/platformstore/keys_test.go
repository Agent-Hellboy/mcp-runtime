package platformstore

import "testing"

func TestRegistryCredentialUsernameMatches(t *testing.T) {
	p := Principal{
		Subject:           "user-id",
		Email:             "admin@example.com",
		Namespace:         "mcp-team-core",
		AllowedNamespaces: []string{"mcp-team-core"},
	}

	for _, username := range []string{"mcp-team-core", "admin@example.com", "user-id"} {
		if !registryCredentialUsernameMatches(p, username) {
			t.Fatalf("expected username %q to match principal", username)
		}
	}

	for _, username := range []string{"", "user-1", "user-2", "other@example.com"} {
		if registryCredentialUsernameMatches(p, username) {
			t.Fatalf("expected username %q not to match principal", username)
		}
	}
}
