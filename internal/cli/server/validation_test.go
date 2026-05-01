package server

import "testing"

func TestValidateManifestValue(t *testing.T) {
	t.Run("trims and returns value", func(t *testing.T) {
		got, err := validateManifestValue("field", "  value  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "value" {
			t.Fatalf("expected trimmed value, got %q", got)
		}
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := validateManifestValue("field", "   ")
		if err == nil {
			t.Fatal("expected error for empty value")
		}
	})

	t.Run("rejects control characters", func(t *testing.T) {
		_, err := validateManifestValue("field", "bad\t")
		if err == nil {
			t.Fatal("expected error for control characters")
		}
	})
}

func TestValidateServerInput(t *testing.T) {
	t.Run("returns sanitized values for valid input", func(t *testing.T) {
		name, namespace, err := validateServerInput("my-server", "test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "my-server" || namespace != "test-ns" {
			t.Fatalf("unexpected values: name=%q namespace=%q", name, namespace)
		}
	})
}

func TestValidateServerInputErrors(t *testing.T) {
	t.Run("rejects invalid namespace", func(t *testing.T) {
		_, _, err := validateServerInput("my-server", "bad\tns")
		if err == nil {
			t.Fatal("expected error for invalid namespace")
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		_, _, err := validateServerInput("my-server", "   ")
		if err == nil {
			t.Fatal("expected error for empty namespace")
		}
	})
}
