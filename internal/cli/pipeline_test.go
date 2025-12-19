package cli

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewPipelineCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewPipelineCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewPipelineCmd should not return nil")
		}
		if cmd.Use != "pipeline" {
			t.Errorf("expected Use='pipeline', got %q", cmd.Use)
		}
	})

	t.Run("has-subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		if len(subcommands) < 2 {
			t.Errorf("expected at least 2 subcommands (generate, deploy), got %d", len(subcommands))
		}

		// Check for expected subcommands
		expectedSubs := map[string]bool{"generate": false, "deploy": false}
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
