package cli

import (
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewAccessCmd(t *testing.T) {
	cmd := NewAccessCmd(zap.NewNop())
	if cmd == nil {
		t.Fatal("NewAccessCmd should not return nil")
	}
	if cmd.Use != "access" {
		t.Fatalf("expected Use='access', got %q", cmd.Use)
	}

	expected := map[string]bool{"grant": false, "session": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Use]; ok {
			expected[sub.Use] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Fatalf("expected subcommand %q not found", name)
		}
	}
}

func TestAccessManager_ListAccessResources(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewAccessManager(kubectl, zap.NewNop())

	if err := mgr.ListAccessResources(accessGrantResource, "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", accessGrantResource, "-A"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestAccessManager_GetAccessResource(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewAccessManager(kubectl, zap.NewNop())

	if err := mgr.GetAccessResource(accessSessionResource, "session-a", "team-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", accessSessionResource, "session-a", "-n", "team-a", "-o", "yaml"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestAccessManager_ApplyAccessResource(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewAccessManager(kubectl, zap.NewNop())

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
	if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") {
		t.Fatalf("expected apply -f args, got %v", cmd.Args)
	}
}

func TestAccessManager_ToggleAccessResource(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		wantJSON string
	}{
		{name: "disable grant", resource: accessGrantResource, wantJSON: `"disabled":true`},
		{name: "revoke session", resource: accessSessionResource, wantJSON: `"revoked":true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockExecutor{}
			kubectl := &KubectlClient{exec: mock, validators: nil}
			mgr := NewAccessManager(kubectl, zap.NewNop())

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
