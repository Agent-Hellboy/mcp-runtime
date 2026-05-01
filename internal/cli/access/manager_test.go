package access_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/access"
	"mcp-runtime/internal/cli/core"
)

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func TestAccessManager_ListAccessResources(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := access.NewAccessManager(kubectl, zap.NewNop())

	if err := mgr.ListAccessResources(access.GrantResource, "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", access.GrantResource, "-A"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestAccessManager_GetAccessResource(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := access.NewAccessManager(kubectl, zap.NewNop())

	if err := mgr.GetAccessResource(access.SessionResource, "session-a", "team-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", access.SessionResource, "session-a", "-n", "team-a", "-o", "yaml"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestAccessManager_ApplyAccessResource(t *testing.T) {
	var applyCmd *core.MockCommand
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			applyCmd = &core.MockCommand{Args: spec.Args}
			return applyCmd
		},
	}
	kubectl, err := core.NewKubectlClient(mock)
	if err != nil {
		t.Fatalf("NewKubectlClient() error = %v", err)
	}
	mgr := access.NewAccessManager(kubectl, zap.NewNop())

	tmpFile, err := os.CreateTemp("", "access-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString("apiVersion: v1\nkind: ConfigMap\n"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	if err := mgr.ApplyAccessResource(tmpFile.Name()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, "-") {
		t.Fatalf("expected apply -f - args, got %v", cmd.Args)
	}
	captured, err := io.ReadAll(applyCmd.StdinR)
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if string(captured) != "apiVersion: v1\nkind: ConfigMap\n" {
		t.Fatalf("unexpected stdin: %q", string(captured))
	}
}

func TestAccessManager_ToggleAccessResource(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		wantJSON string
	}{
		{name: "disable grant", resource: access.GrantResource, wantJSON: `"disabled":true`},
		{name: "revoke session", resource: access.SessionResource, wantJSON: `"revoked":true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &core.MockExecutor{}
			kubectl := core.NewTestKubectlClient(mock)
			mgr := access.NewAccessManager(kubectl, zap.NewNop())

			if err := mgr.ToggleAccessResource(tt.resource, "obj-a", "team-a", true); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cmd := mock.LastCommand()
			for _, want := range []string{"patch", tt.resource, "obj-a", "-n", "team-a", "--type", "merge", "--patch"} {
				if !contains(cmd.Args, want) {
					t.Fatalf("expected %q in args, got %v", want, cmd.Args)
				}
			}
			patchIndex := -1
			for i, arg := range cmd.Args {
				if arg == "--patch" && i+1 < len(cmd.Args) {
					patchIndex = i + 1
					break
				}
			}
			if patchIndex == -1 {
				t.Fatalf("expected --patch argument, got %v", cmd.Args)
			}
			if !strings.Contains(cmd.Args[patchIndex], tt.wantJSON) {
				t.Fatalf("expected patch payload %q, got %q", tt.wantJSON, cmd.Args[patchIndex])
			}
		})
	}
}
