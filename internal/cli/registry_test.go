package cli

import (
	"errors"
	"testing"

	"go.uber.org/zap"
)

func TestNewRegistryCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewRegistryCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewRegistryCmd should not return nil")
		}
		if cmd.Use != "registry" {
			t.Errorf("expected Use='registry', got %q", cmd.Use)
		}
	})

	t.Run("has-subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		expectedSubs := []string{"status", "info", "provision", "push"}
		if len(subcommands) < len(expectedSubs) {
			t.Errorf("expected at least %d subcommands, got %d", len(expectedSubs), len(subcommands))
		}
	})
}

func TestRegistryManager_CheckRegistryStatus(t *testing.T) {
	t.Run("returns error when deployment not found", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultErr: errors.New("not found"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.CheckRegistryStatus("registry")
		if err == nil {
			t.Fatal("expected error when registry not found")
		}
	})

	t.Run("calls kubectl get deployment", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("1"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		_ = mgr.CheckRegistryStatus("registry")

		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}

		// Should query deployment status
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "get") && contains(cmd.Args, "deployment") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl get deployment to be called")
		}
	})
}

func TestRegistryManager_LoginRegistry(t *testing.T) {
	t.Run("calls docker login", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.LoginRegistry("localhost:5000", "user", "pass")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("docker") {
			t.Error("expected docker to be called")
		}

		// Check docker login args
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" && contains(cmd.Args, "login") {
				found = true
				if !contains(cmd.Args, "localhost:5000") {
					t.Errorf("expected registry URL in args, got %v", cmd.Args)
				}
				break
			}
		}
		if !found {
			t.Error("expected docker login to be called")
		}
	})
}

func TestRegistryManager_PushDirect(t *testing.T) {
	t.Run("calls docker tag and push", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushDirect("source:tag", "target:tag")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("docker") {
			t.Error("expected docker to be called")
		}

		// Should call docker tag first, then docker push
		tagFound := false
		pushFound := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" && contains(cmd.Args, "tag") {
				tagFound = true
			}
			if cmd.Name == "docker" && contains(cmd.Args, "push") {
				pushFound = true
			}
		}
		if !tagFound {
			t.Error("expected docker tag to be called")
		}
		if !pushFound {
			t.Error("expected docker push to be called")
		}
	})
}

// Helper functions for image parsing
func TestSplitImage(t *testing.T) {
	tests := []struct {
		image string
		want  string
		tag   string
	}{
		{"registry.example.com/example-mcp-server:latest", "registry.example.com/example-mcp-server", "latest"},
		{"registry.example.com/example-mcp-server", "registry.example.com/example-mcp-server", ""},
		{"example-mcp-server:latest", "example-mcp-server", "latest"},
		{"example-mcp-server", "example-mcp-server", ""},
	}
	for _, test := range tests {
		image, tag := splitImage(test.image)
		if image != test.want {
			t.Errorf("SplitImage(%q) = %q, want %q", test.image, image, test.want)
		}
		if tag != test.tag {
			t.Errorf("SplitImage(%q) tag = %q, want %q", test.image, tag, test.tag)
		}
	}
}

func TestDropRegistryPrefix(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"registry.example.com/example-mcp-server", "example-mcp-server"},
		{"example-mcp-server", "example-mcp-server"},
		{"localhost:5000/my-image", "my-image"},
		{"192.168.1.1:5000/my-image", "my-image"},
		{"my-image", "my-image"},
	}
	for _, test := range tests {
		repo := dropRegistryPrefix(test.repo)
		if repo != test.want {
			t.Errorf("dropRegistryPrefix(%q) = %q, want %q", test.repo, repo, test.want)
		}
	}
}
