package access

import "testing"

func TestValidateAccessResourceInput(t *testing.T) {
	name, namespace, err := validateAccessResourceInput("grant-one", "mcp-servers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "grant-one" || namespace != "mcp-servers" {
		t.Fatalf("unexpected values: name=%q namespace=%q", name, namespace)
	}
}

func TestValidateAccessResourceInputErrors(t *testing.T) {
	t.Run("rejects invalid name", func(t *testing.T) {
		_, _, err := validateAccessResourceInput("GrantOne", "mcp-servers")
		if err == nil {
			t.Fatal("expected invalid resource name error")
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		_, _, err := validateAccessResourceInput("grant-one", "   ")
		if err == nil {
			t.Fatal("expected empty namespace error")
		}
	})
}
