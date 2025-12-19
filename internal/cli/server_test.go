package cli

import (
	"testing"

	"go.uber.org/zap"
)

// Note: Most server functions execute kubectl commands and are better tested
// through integration tests or by mocking execCommand.
// Unit tests here would require mocking execCommand which is complex.

func TestNewServerCmd(t *testing.T) {
	// Test that the command structure is created correctly
	logger := zap.NewNop()
	cmd := NewServerCmd(logger)

	if cmd == nil {
		t.Fatal("NewServerCmd should not return nil")
	}

	if cmd.Use != "server" {
		t.Errorf("Expected command use 'server', got %q", cmd.Use)
	}

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	if len(subcommands) == 0 {
		t.Error("Server command should have subcommands")
	}
}
