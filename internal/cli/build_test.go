package cli

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewBuildImageCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := newBuildImageCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("newBuildImageCmd should not return nil")
		}
		// Use includes the argument pattern
		if cmd.Use != "image [server-name]" {
			t.Errorf("expected Use='image [server-name]', got %q", cmd.Use)
		}
	})

	t.Run("has-flags", func(t *testing.T) {
		flags := cmd.Flags()
		if flags == nil {
			t.Fatal("newBuildImageCmd should have flags")
		}

		expectedFlags := []string{"dockerfile", "metadata-file", "metadata-dir", "registry", "tag", "context"}
		for _, name := range expectedFlags {
			if flags.Lookup(name) == nil {
				t.Errorf("expected flag %q not found", name)
			}
		}
	})
}
