package server

import (
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

func TestServerCommandIncludesPushWorkflow(t *testing.T) {
	cmd := NewWithManager(NewServerManager(core.NewTestKubectlClient(&core.MockExecutor{}), zap.NewNop()))

	var pushFound bool
	for _, child := range cmd.Commands() {
		if child.Use != "push" {
			continue
		}
		pushFound = true
		for _, flag := range []string{"image", "name", "scope"} {
			if child.Flags().Lookup(flag) == nil {
				t.Fatalf("server push is missing --%s", flag)
			}
		}
	}
	if !pushFound {
		t.Fatal("server command must expose the authenticated image push workflow")
	}
}
