package registry

import (
	"strings"
	"testing"
)

func TestRegistryRejectsRemovedPushAlias(t *testing.T) {
	cmd := NewWithManager(nil)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"push"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown command "push"`) {
		t.Fatalf("Execute() error = %v, want unknown command error", err)
	}
}
