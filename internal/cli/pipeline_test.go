package cli

import (
	"bytes"
	"errors"
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

	t.Run("returns error for empty servers", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "empty.yaml")
		if err := os.WriteFile(metadataFile, []byte("version: \"1\"\nservers: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.GenerateCRDsFromMetadata(metadataFile, "", t.TempDir())
		if err == nil {
			t.Fatal("expected error for empty servers")
		}
	})

	t.Run("generates CRDs from file successfully", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		metadataFile := filepath.Join(tmpDir, "servers.yaml")
		content := `version: "1"
servers:
  - name: test-server
    image: test-image:latest
`
		if err := os.WriteFile(metadataFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.GenerateCRDsFromMetadata(metadataFile, "", outputDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check output file was created
		files, _ := filepath.Glob(filepath.Join(outputDir, "*.yaml"))
		if len(files) == 0 {
			t.Error("expected CRD files to be generated")
		}
	})

	t.Run("generates CRDs from directory successfully", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		tmpDir := t.TempDir()
		metadataDir := filepath.Join(tmpDir, ".mcp")
		outputDir := filepath.Join(tmpDir, "output")
		if err := os.MkdirAll(metadataDir, 0o755); err != nil {
			t.Fatal(err)
		}

		metadataFile := filepath.Join(metadataDir, "servers.yaml")
		content := `version: "1"
servers:
  - name: dir-server
    image: dir-image:v1
`
		if err := os.WriteFile(metadataFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		err := mgr.GenerateCRDsFromMetadata("", metadataDir, outputDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error for missing metadata directory", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewPipelineManager(kubectl, zap.NewNop())

		err := mgr.GenerateCRDsFromMetadata("", "/nonexistent/dir", t.TempDir())
		if err == nil {
			t.Fatal("expected error for missing metadata directory")
		}
	})
}

func TestDefaultPipelineManager(t *testing.T) {
	logger := zap.NewNop()
	mgr := DefaultPipelineManager(logger)

	if mgr == nil {
		t.Fatal("DefaultPipelineManager should not return nil")
	}
	if mgr.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestNewPipelineCmdCreatesCommand(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewPipelineCmd(logger)

	if cmd == nil {
		t.Fatal("NewPipelineCmd should not return nil")
	}
	if cmd.Use != "pipeline" {
		t.Errorf("expected Use='pipeline', got %q", cmd.Use)
	}
}

func TestNewPipelineCmdWithManager(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	cmd := NewPipelineCmdWithManager(mgr)

	t.Run("command_created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewPipelineCmdWithManager should not return nil")
		}
		if cmd.Use != "pipeline" {
			t.Errorf("expected Use='pipeline', got %q", cmd.Use)
		}
	})

	t.Run("has_subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		if len(subcommands) != 2 {
			t.Errorf("expected 2 subcommands (generate, deploy), got %d", len(subcommands))
		}
	})
}

func TestPipelineGenerateCmd(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	cmd := mgr.newPipelineGenerateCmd()

	t.Run("command_created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("newPipelineGenerateCmd should not return nil")
		}
		if cmd.Use != "generate" {
			t.Errorf("expected Use='generate', got %q", cmd.Use)
		}
	})

	t.Run("has_flags", func(t *testing.T) {
		flags := cmd.Flags()
		expectedFlags := []string{"file", "dir", "output"}
		for _, name := range expectedFlags {
			if flags.Lookup(name) == nil {
				t.Errorf("expected flag %q not found", name)
			}
		}
	})

	t.Run("executes_generate", func(t *testing.T) {
		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "test.yaml")
		content := `version: "1"
servers:
  - name: gen-test
    image: test:v1
`
		if err := os.WriteFile(metadataFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		outputDir := filepath.Join(tmpDir, "out")
		cmd.SetArgs([]string{"--file", metadataFile, "--output", outputDir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPipelineDeployCmd(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	cmd := mgr.newPipelineDeployCmd()

	t.Run("command_created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("newPipelineDeployCmd should not return nil")
		}
		if cmd.Use != "deploy" {
			t.Errorf("expected Use='deploy', got %q", cmd.Use)
		}
	})

	t.Run("has_flags", func(t *testing.T) {
		flags := cmd.Flags()
		expectedFlags := []string{"dir", "namespace"}
		for _, name := range expectedFlags {
			if flags.Lookup(name) == nil {
				t.Errorf("expected flag %q not found", name)
			}
		}
	})

	t.Run("executes_deploy", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}

		cmd.SetArgs([]string{"--dir", tmpDir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPipelineManager_DeployCRDs_WithoutNamespace(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := mgr.DeployCRDs(tmpDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	// Should not have -n flag when namespace is empty
	for i, arg := range cmd.Args {
		if arg == "-n" {
			t.Errorf("should not have -n flag when namespace is empty, got args: %v at index %d", cmd.Args, i)
		}
	}
}

func TestPipelineManager_DeployCRDs_ApplyError(t *testing.T) {
	mock := &MockExecutor{DefaultRunErr: errors.New("apply failed")}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := mgr.DeployCRDs(tmpDir, "")
	if err == nil {
		t.Fatal("expected error when apply fails")
	}
}

func TestPipelineManager_GenerateCRDsFromMetadata_CRDGenerationError(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	tmpDir := t.TempDir()
	metadataFile := filepath.Join(tmpDir, "servers.yaml")
	content := `version: "1"
servers:
  - name: test-server
    image: test-image:latest
`
	if err := os.WriteFile(metadataFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a file at outputDir path to cause mkdir to fail
	outputPath := filepath.Join(tmpDir, "output")
	if err := os.WriteFile(outputPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := mgr.GenerateCRDsFromMetadata(metadataFile, "", outputPath)
	if err == nil {
		t.Fatal("expected error when CRD generation fails")
	}
}

func TestPipelineManager_DeployCRDs_GlobYamlError(t *testing.T) {
	// Save and restore original filepathGlob
	originalGlob := filepathGlob
	defer func() { filepathGlob = originalGlob }()

	callCount := 0
	filepathGlob = func(pattern string) ([]string, error) {
		callCount++
		if callCount == 1 {
			// First call for *.yaml - return error
			return nil, errors.New("glob error")
		}
		return nil, nil
	}

	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	err := mgr.DeployCRDs("/some/dir", "")
	if err == nil {
		t.Fatal("expected error when glob fails for yaml")
	}
}

func TestPipelineManager_DeployCRDs_GlobYmlError(t *testing.T) {
	// Save and restore original filepathGlob
	originalGlob := filepathGlob
	defer func() { filepathGlob = originalGlob }()

	callCount := 0
	filepathGlob = func(pattern string) ([]string, error) {
		callCount++
		if callCount == 1 {
			// First call for *.yaml - return empty
			return []string{}, nil
		}
		if callCount == 2 {
			// Second call for *.yml - return error
			return nil, errors.New("glob error")
		}
		return nil, nil
	}

	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewPipelineManager(kubectl, zap.NewNop())

	err := mgr.DeployCRDs("/some/dir", "")
	if err == nil {
		t.Fatal("expected error when glob fails for yml")
	}
}
