package admin

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry"
)

func TestAdminRegistryPushRequiresClusterAccess(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if spec.Name == "kubectl" {
				cmd.OutputData = []byte("Unable to connect to the server")
				cmd.OutputErr = context.Canceled
			}
			return cmd
		},
	}
	mgr := registry.NewRegistryManager(core.NewTestKubectlClient(mock), mock, zap.NewNop())
	err := registry.RunAdminRegistryPush(context.Background(), mgr, "source:tag", "registry.example.com", "demo", "", "direct", "registry")
	if err == nil {
		t.Fatal("expected admin cluster access error")
	}
	if !strings.Contains(err.Error(), "admin registry push requires admin cluster access") {
		t.Fatalf("error = %v", err)
	}
}
