package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewBuildImageCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := newBuildImageCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("newBuildImageCmd should not return nil")
		}
		// Use includes the argument pattern (required arg uses <>)
		if cmd.Use != "image <server-name>" {
			t.Errorf("expected Use='image <server-name>', got %q", cmd.Use)
		}
	})

	t.Run("has-flags", func(t *testing.T) {
		flags := cmd.Flags()
		if flags == nil {
			t.Fatal("newBuildImageCmd should have flags")
		}

		expectedFlags := []string{"dockerfile", "metadata-file", "metadata-dir", "registry", "tag", "context"}
		for _, name := range expectedFlags {
			if flags.Lookup(name) == nil {
				t.Errorf("expected flag %q not found", name)
			}
		}
	})
}

func TestGetGitTag(t *testing.T) {
	// This test runs in a git repo, so it should return a valid SHA or "latest"
	tag := getGitTag()

	if tag == "" {
		t.Error("getGitTag should not return empty string")
	}

	// Should be either a short SHA (7-8 chars) or "latest"
	if tag != "latest" && len(tag) < 7 {
		t.Errorf("getGitTag returned unexpected value: %q", tag)
	}
}

func TestBuildImage(t *testing.T) {
	logger := zap.NewNop()

	t.Run("builds_image_successfully", func(t *testing.T) {
		// Save original executor and restore after test
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		// Create mock executor
		mock := &MockExecutor{}
		execExecutor = mock

		err := buildImage(logger, "test-server", "Dockerfile", "", ".", "test-registry", "test-tag", ".")
		if err != nil {
			t.Fatalf("failed to build image: %v", err)
		}

		// Verify docker build was called
		if !mock.HasCommand("docker") {
			t.Error("expected docker command to be executed")
		}

		// Verify the command arguments
		last := mock.LastCommand()
		if last.Name != "docker" {
			t.Errorf("expected docker command, got %q", last.Name)
		}

		// Check expected args
		expectedArgs := []string{"build", "-f", "Dockerfile", "-t", "test-registry/test-server:test-tag", "."}
		if !equalStringSlices(last.Args, expectedArgs) {
			t.Errorf("docker args = %v, want %v", last.Args, expectedArgs)
		}
	})

	t.Run("returns_error_on_build_failure", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		// Mock that returns error on Run()
		mock := &MockExecutor{
			DefaultRunErr: errors.New("docker build failed"),
		}
		execExecutor = mock

		err := buildImage(logger, "test-server", "Dockerfile", "", ".", "test-registry", "test-tag", ".")
		if err == nil {
			t.Error("expected error when docker build fails")
		}
	})

	t.Run("uses_git_tag_when_tag_empty", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if spec.Name == "git" {
					// Return a mock git SHA
					return &MockCommand{OutputData: []byte("abc1234\n")}
				}
				// Return success for docker
				return &MockCommand{}
			},
		}
		execExecutor = mock

		err := buildImage(logger, "my-server", "Dockerfile", "", ".", "registry.io", "", ".")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check the docker build was called with git tag
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" {
				// Should contain the git SHA in the tag
				found := false
				for _, arg := range cmd.Args {
					if arg == "registry.io/my-server:abc1234" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected image tag with git SHA, got args: %v", cmd.Args)
				}
			}
		}
	})

	t.Run("uses_platform_registry_when_registry_empty", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		// Mock kubectl to return cluster IP and port
		kubectlMock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputData: []byte("10.0.0.1")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputData: []byte("5000")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: kubectlMock, validators: nil}

		mock := &MockExecutor{}
		execExecutor = mock

		err := buildImage(logger, "my-server", "Dockerfile", "", ".", "", "v1.0", ".")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check that docker was called with the platform registry
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" {
				found := false
				for _, arg := range cmd.Args {
					if arg == "10.0.0.1:5000/my-server:v1.0" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected platform registry in image tag, got args: %v", cmd.Args)
				}
			}
		}
	})

	t.Run("returns_error_when_command_validator_fails", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		// Create a mock executor that returns an error from Command()
		mock := &MockExecutor{}
		// We need to use a custom executor that fails validation
		failingExecutor := &validatorFailingExecutor{err: errors.New("validator failed")}
		execExecutor = failingExecutor

		err := buildImage(logger, "test-server", "Dockerfile", "", ".", "registry", "tag", ".")
		if err == nil {
			t.Error("expected error when command validator fails")
		}
		if err.Error() != "validator failed" {
			t.Errorf("unexpected error: %v", err)
		}

		// Restore for cleanup
		execExecutor = mock
	})
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validatorFailingExecutor is a test executor that always fails validation.
type validatorFailingExecutor struct {
	err error
}

func (v *validatorFailingExecutor) Command(name string, args []string, validators ...ExecValidator) (Command, error) {
	return nil, v.err
}

func TestNewBuildImageCmdRunE(t *testing.T) {
	logger := zap.NewNop()

	t.Run("executes_build_image", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		mock := &MockExecutor{}
		execExecutor = mock

		cmd := newBuildImageCmd(logger)
		cmd.SetArgs([]string{"my-server", "--registry", "test-registry", "--tag", "v1.0"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("docker") {
			t.Error("expected docker command to be executed")
		}
	})

	t.Run("fails_without_server_name", func(t *testing.T) {
		cmd := newBuildImageCmd(logger)
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		if err == nil {
			t.Error("expected error when server name is missing")
		}
	})
}

func TestGetGitTagWithMock(t *testing.T) {
	t.Run("returns_latest_when_git_fails", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if spec.Name == "git" {
					return &MockCommand{OutputErr: errors.New("git not found")}
				}
				return &MockCommand{}
			},
		}
		execExecutor = mock

		tag := getGitTag()
		if tag != "latest" {
			t.Errorf("expected 'latest' when git fails, got %q", tag)
		}
	})

	t.Run("returns_latest_when_output_empty", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if spec.Name == "git" {
					return &MockCommand{OutputData: []byte("")}
				}
				return &MockCommand{}
			},
		}
		execExecutor = mock

		tag := getGitTag()
		if tag != "latest" {
			t.Errorf("expected 'latest' when output empty, got %q", tag)
		}
	})

	t.Run("returns_trimmed_sha", func(t *testing.T) {
		originalExecutor := execExecutor
		defer func() { execExecutor = originalExecutor }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if spec.Name == "git" {
					return &MockCommand{OutputData: []byte("  abc1234  \n")}
				}
				return &MockCommand{}
			},
		}
		execExecutor = mock

		tag := getGitTag()
		if tag != "abc1234" {
			t.Errorf("expected 'abc1234', got %q", tag)
		}
	})
}

func TestGetPlatformRegistryURLWithMock(t *testing.T) {
	logger := zap.NewNop()

	t.Run("returns_cluster_ip_and_port", func(t *testing.T) {
		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputData: []byte("10.96.0.100")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputData: []byte("5000")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		url := getPlatformRegistryURL(logger)
		if url != "10.96.0.100:5000" {
			t.Errorf("expected '10.96.0.100:5000', got %q", url)
		}
	})

	t.Run("returns_default_when_ip_command_fails", func(t *testing.T) {
		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputErr: errors.New("kubectl error")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputData: []byte("5000")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		url := getPlatformRegistryURL(logger)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})

	t.Run("returns_default_when_port_command_fails", func(t *testing.T) {
		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputData: []byte("10.96.0.100")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputErr: errors.New("kubectl error")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		url := getPlatformRegistryURL(logger)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})

	t.Run("returns_default_when_ip_empty", func(t *testing.T) {
		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputData: []byte("")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputData: []byte("5000")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		url := getPlatformRegistryURL(logger)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})

	t.Run("returns_default_when_port_empty", func(t *testing.T) {
		originalKubectl := kubectlClient
		defer func() { kubectlClient = originalKubectl }()

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				for _, arg := range spec.Args {
					if arg == "jsonpath={.spec.clusterIP}" {
						return &MockCommand{OutputData: []byte("10.96.0.100")}
					}
					if arg == "jsonpath={.spec.ports[0].port}" {
						return &MockCommand{OutputData: []byte("")}
					}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		url := getPlatformRegistryURL(logger)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})
}

func TestUpdateMetadataImage(t *testing.T) {
	t.Run("updates_with_explicit_metadata_file", func(t *testing.T) {
		// Create temp directory
		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "servers.yaml")

		// Write initial metadata
		initialContent := `version: "1"
servers:
  - name: my-server
    image: old-registry/my-server
    imageTag: old-tag
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write initial metadata: %v", err)
		}

		err := updateMetadataImage("my-server", "new-registry/my-server", "new-tag", metadataFile, "")
		if err != nil {
			t.Fatalf("updateMetadataImage failed: %v", err)
		}

		// Read and verify
		content, err := os.ReadFile(metadataFile)
		if err != nil {
			t.Fatalf("failed to read updated metadata: %v", err)
		}

		if !strings.Contains(string(content), "new-registry/my-server") {
			t.Errorf("expected new image in metadata, got: %s", content)
		}
		if !strings.Contains(string(content), "new-tag") {
			t.Errorf("expected new tag in metadata, got: %s", content)
		}
	})

	t.Run("finds_metadata_in_directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataDir := filepath.Join(tmpDir, ".mcp")
		if err := os.MkdirAll(metadataDir, 0o755); err != nil {
			t.Fatalf("failed to create metadata dir: %v", err)
		}

		metadataFile := filepath.Join(metadataDir, "servers.yaml")
		initialContent := `version: "1"
servers:
  - name: discovered-server
    image: old-image
    imageTag: old
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}

		err := updateMetadataImage("discovered-server", "new-image", "v2.0", "", metadataDir)
		if err != nil {
			t.Fatalf("updateMetadataImage failed: %v", err)
		}

		content, err := os.ReadFile(metadataFile)
		if err != nil {
			t.Fatalf("failed to read metadata: %v", err)
		}

		if !strings.Contains(string(content), "new-image") {
			t.Errorf("expected new image, got: %s", content)
		}
	})

	t.Run("finds_yml_files", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataDir := filepath.Join(tmpDir, ".mcp")
		if err := os.MkdirAll(metadataDir, 0o755); err != nil {
			t.Fatalf("failed to create metadata dir: %v", err)
		}

		metadataFile := filepath.Join(metadataDir, "servers.yml")
		initialContent := `version: "1"
servers:
  - name: yml-server
    image: old-image
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}

		err := updateMetadataImage("yml-server", "new-image", "v1.0", "", metadataDir)
		if err != nil {
			t.Fatalf("updateMetadataImage failed: %v", err)
		}
	})

	t.Run("returns_error_when_file_not_found", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := updateMetadataImage("nonexistent-server", "image", "tag", "", tmpDir)
		if err == nil {
			t.Error("expected error when metadata file not found")
		}
		if !strings.Contains(err.Error(), "metadata file not found") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns_error_when_server_not_in_metadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "servers.yaml")

		initialContent := `version: "1"
servers:
  - name: other-server
    image: some-image
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}

		err := updateMetadataImage("missing-server", "image", "tag", metadataFile, "")
		if err == nil {
			t.Error("expected error when server not found in metadata")
		}
		if !strings.Contains(err.Error(), "not found in metadata") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns_error_when_metadata_file_invalid", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "invalid.yaml")

		if err := os.WriteFile(metadataFile, []byte("not: valid: yaml: content:::"), 0o600); err != nil {
			t.Fatalf("failed to write invalid metadata: %v", err)
		}

		err := updateMetadataImage("server", "image", "tag", metadataFile, "")
		if err == nil {
			t.Error("expected error when metadata file is invalid")
		}
	})

	t.Run("skips_invalid_files_in_directory_search", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataDir := filepath.Join(tmpDir, ".mcp")
		if err := os.MkdirAll(metadataDir, 0o755); err != nil {
			t.Fatalf("failed to create metadata dir: %v", err)
		}

		// Write invalid file first (should be skipped)
		invalidFile := filepath.Join(metadataDir, "invalid.yaml")
		if err := os.WriteFile(invalidFile, []byte("not: valid: yaml:::"), 0o600); err != nil {
			t.Fatalf("failed to write invalid file: %v", err)
		}

		// Write valid file with our server
		validFile := filepath.Join(metadataDir, "valid.yaml")
		validContent := `version: "1"
servers:
  - name: target-server
    image: old-image
`
		if err := os.WriteFile(validFile, []byte(validContent), 0o600); err != nil {
			t.Fatalf("failed to write valid file: %v", err)
		}

		err := updateMetadataImage("target-server", "new-image", "v1.0", "", metadataDir)
		if err != nil {
			t.Fatalf("updateMetadataImage should skip invalid files: %v", err)
		}
	})

	t.Run("returns_error_when_file_write_fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "servers.yaml")

		initialContent := `version: "1"
servers:
  - name: my-server
    image: old-image
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}

		// Make the file read-only to cause write failure
		if err := os.Chmod(metadataFile, 0o400); err != nil {
			t.Fatalf("failed to chmod file: %v", err)
		}
		// Restore permissions for cleanup
		defer func() { _ = os.Chmod(metadataFile, 0o600) }()

		err := updateMetadataImage("my-server", "new-image", "v1.0", metadataFile, "")
		if err == nil {
			t.Error("expected error when file write fails")
		}
		if !strings.Contains(err.Error(), "failed to write metadata") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns_error_when_yaml_marshal_fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataFile := filepath.Join(tmpDir, "servers.yaml")

		initialContent := `version: "1"
servers:
  - name: my-server
    image: old-image
`
		if err := os.WriteFile(metadataFile, []byte(initialContent), 0o600); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}

		// Save and restore original yamlMarshal
		originalMarshal := yamlMarshal
		defer func() { yamlMarshal = originalMarshal }()

		// Mock yamlMarshal to return error
		yamlMarshal = func(v interface{}) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}

		err := updateMetadataImage("my-server", "new-image", "v1.0", metadataFile, "")
		if err == nil {
			t.Error("expected error when yaml marshal fails")
		}
		if !strings.Contains(err.Error(), "failed to marshal metadata") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
