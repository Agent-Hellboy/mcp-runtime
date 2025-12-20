package cli

import (
	"os"
	"path/filepath"
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
		tmpDir := t.TempDir()
		kubeconfig := filepath.Join(tmpDir, "config")
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatalf("failed to write kubeconfig: %v", err)
		}

		mock := &MockExecutor{
			DefaultOutput: []byte("Switched to context"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.InitCluster(kubeconfig, "my-context")
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

func TestProvisionEKSCluster(t *testing.T) {
	t.Run("uses eksctl with args", func(t *testing.T) {
		mock := &MockExecutor{}
		err := provisionEKSCluster(zap.NewNop(), mock, "us-west-2", 3, "my-eks")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "eksctl" {
			t.Fatalf("expected eksctl command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "create") || !contains(cmd.Args, "cluster") {
			t.Fatalf("expected eksctl create cluster args, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-eks") {
			t.Fatalf("expected --name my-eks, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--region") || !contains(cmd.Args, "us-west-2") {
			t.Fatalf("expected --region us-west-2, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--nodes") || !contains(cmd.Args, "3") {
			t.Fatalf("expected --nodes 3, got %v", cmd.Args)
		}
	})

	t.Run("defaults cluster name when empty", func(t *testing.T) {
		mock := &MockExecutor{}
		err := provisionEKSCluster(zap.NewNop(), mock, "us-west-2", 2, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, defaultClusterName) {
			t.Fatalf("expected --name %s, got %v", defaultClusterName, cmd.Args)
		}
	})
}

func TestConfigureEKSKubeconfig(t *testing.T) {
	t.Run("uses aws eks update-kubeconfig", func(t *testing.T) {
		mock := &MockExecutor{}
		err := configureEKSKubeconfig(mock, "us-west-2", "my-eks", "/tmp/kubeconfig")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "aws" {
			t.Fatalf("expected aws command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "eks") || !contains(cmd.Args, "update-kubeconfig") {
			t.Fatalf("expected aws eks update-kubeconfig args, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-eks") {
			t.Fatalf("expected --name my-eks, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--region") || !contains(cmd.Args, "us-west-2") {
			t.Fatalf("expected --region us-west-2, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--kubeconfig") || !contains(cmd.Args, "/tmp/kubeconfig") {
			t.Fatalf("expected --kubeconfig /tmp/kubeconfig, got %v", cmd.Args)
		}
	})

	t.Run("defaults cluster name when empty", func(t *testing.T) {
		mock := &MockExecutor{}
		err := configureEKSKubeconfig(mock, "us-west-2", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, defaultClusterName) {
			t.Fatalf("expected --name %s, got %v", defaultClusterName, cmd.Args)
		}
	})
}
