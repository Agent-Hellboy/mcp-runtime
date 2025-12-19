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

func TestClusterManager_CheckClusterStatus(t *testing.T) {
	t.Run("calls kubectl cluster-info", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("Kubernetes control plane is running"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.CheckClusterStatus()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}

		// Should call cluster-info
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "cluster-info") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl cluster-info to be called")
		}
	})
}

func TestClusterManager_EnsureNamespace(t *testing.T) {
	t.Run("calls kubectl apply with namespace yaml via stdin", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.EnsureNamespace("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}
		// Uses apply -f - (stdin)
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, "-") {
			t.Errorf("expected 'apply -f -' in args, got %v", cmd.Args)
		}
	})
}

func TestClusterManager_InitCluster(t *testing.T) {
	t.Run("sets kubeconfig context when provided", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("Switched to context"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.InitCluster("", "my-context")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should call use-context
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "use-context") && contains(cmd.Args, "my-context") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl config use-context my-context to be called")
		}
	})
}
