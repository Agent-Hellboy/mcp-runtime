package cli

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestNewPipelineCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewPipelineCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewPipelineCmd should not return nil")
		}
		if cmd.Use != "pipeline" {
			t.Errorf("expected Use='pipeline', got %q", cmd.Use)
		}
	})

	t.Run("has-subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		if len(subcommands) < 2 {
			t.Errorf("expected at least 2 subcommands (generate, deploy), got %d", len(subcommands))
		}

		expectedSubs := map[string]bool{"generate": false, "deploy": false}
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

func TestPipelineManager_DeployCRDs(t *testing.T) {
	t.Run("returns error when no manifests found", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		// Use empty temp dir
		tmpDir := t.TempDir()

		err := mgr.DeployCRDs(tmpDir, "test-ns")
		if err == nil {
			t.Fatal("expected error when no manifests found")
		}
	})

	t.Run("applies each manifest file", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		// Create temp dir with manifest files
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "server1.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "server2.yml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.DeployCRDs(tmpDir, "test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have called kubectl apply twice
		applyCount := 0
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "apply") {
				applyCount++
			}
		}
		if applyCount != 2 {
			t.Errorf("expected 2 kubectl apply calls, got %d", applyCount)
		}
	})

	t.Run("includes namespace in kubectl args", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.DeployCRDs(tmpDir, "my-namespace")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "-n") || !contains(cmd.Args, "my-namespace") {
			t.Errorf("expected -n my-namespace in args, got %v", cmd.Args)
		}
	})
}

func TestPipelineManager_GenerateCRDsFromMetadata(t *testing.T) {
	t.Run("returns error for missing metadata", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		err := mgr.GenerateCRDsFromMetadata("nonexistent.yaml", "", t.TempDir())
		if err == nil {
			t.Fatal("expected error for missing metadata file")
		}
	})
}
