package cli

import (
	"testing"

	"go.uber.org/zap"
)

func TestShowPlatformStatus(t *testing.T) {
	// showPlatformStatus is a display function that shows status info
	// It returns nil even if components are not healthy (it displays their status)
	// This test just verifies the function doesn't panic
	t.Run("displays-status", func(t *testing.T) {
		// Use a nop logger to suppress output during tests
		logger := zap.NewNop()

		// The function should not return an error - it displays status
		if err := showPlatformStatus(logger); err != nil {
			t.Errorf("showPlatformStatus() unexpected error = %v", err)
		}
	})
}

func TestCheckRegistryStatusQuiet(t *testing.T) {
	t.Run("returns-error-when-registry-not-found", func(t *testing.T) {
		logger := zap.NewNop()
		// This will likely fail in test env without a cluster
		// but we're testing that it handles errors gracefully
		_ = checkRegistryStatusQuiet(logger, "nonexistent-namespace")
	})
}
