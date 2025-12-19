package cli

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewClusterCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewClusterCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewClusterCmd should not return nil")
		}
		if cmd.Use != "cluster" {
			t.Errorf("expected Use='cluster', got %q", cmd.Use)
		}
	})

	t.Run("has-subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		if len(subcommands) < 4 {
			t.Errorf("expected at least 4 subcommands (init, status, config, provision), got %d", len(subcommands))
		}

		// Check for expected subcommands
		expectedSubs := map[string]bool{
			"init":      false,
			"status":    false,
			"config":    false,
			"provision": false,
		}
		for _, sub := range subcommands {
			if _, ok := expectedSubs[sub.Use]; ok {
				expectedSubs[sub.Use] = true
			}
		}

		for name, found := range expectedSubs {
			if !found {
				t.Errorf("expected subcommand %q not found", name)
			}
		}
	})
}
