package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestNewServerCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewServerCmd(logger)

	if cmd == nil {
		t.Fatal("NewServerCmd should not return nil")
	}

	if cmd.Use != "server" {
		t.Errorf("Expected command use 'server', got %q", cmd.Use)
	}

	subcommands := cmd.Commands()
	if len(subcommands) == 0 {
		t.Error("Server command should have subcommands")
	}
}

func TestServerManager_ListServers(t *testing.T) {
	t.Run("calls kubectl with correct args", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("server1\nserver2\n"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.Commands) == 0 {
			t.Fatal("expected kubectl command to be called")
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}

		// Check args contain namespace
		found := false
		for i, arg := range cmd.Args {
			if arg == "-n" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "test-ns" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected -n test-ns in args, got %v", cmd.Args)
		}
	})

	t.Run("trims namespace and passes to kubectl", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers(" test-ns ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		found := false
		for i, arg := range cmd.Args {
			if arg == "-n" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "test-ns" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected trimmed namespace in args, got %v", cmd.Args)
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers("   ")
		if err == nil {
			t.Fatal("expected error for empty namespace")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl with empty namespace")
		}
	})
}

func TestServerManager_DeleteServer(t *testing.T) {
	t.Run("validates server name", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		// Invalid name with special chars
		err := mgr.DeleteServer("bad;name", "test-ns")
		if err == nil {
			t.Fatal("expected error for invalid server name")
		}

		// Should not have called kubectl
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl with invalid name")
		}
	})

	t.Run("calls kubectl delete with correct args", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.DeleteServer("my-server", "test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}

		// Should contain delete, mcpserver, name, namespace
		argsStr := ""
		for _, a := range cmd.Args {
			argsStr += a + " "
		}
		if !contains(cmd.Args, "delete") {
			t.Errorf("expected 'delete' in args: %s", argsStr)
		}
		if !contains(cmd.Args, "mcpserver") {
			t.Errorf("expected 'mcpserver' in args: %s", argsStr)
		}
		if !contains(cmd.Args, "my-server") {
			t.Errorf("expected 'my-server' in args: %s", argsStr)
		}
	})
}

func TestServerManager_GetServer(t *testing.T) {
	t.Run("validates inputs", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.GetServer("invalid|name", "ns")
		if err == nil {
			t.Fatal("expected error for invalid name")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl with invalid input")
		}
	})
}

func TestServerManager_CreateServer(t *testing.T) {
	t.Run("requires image", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "", "latest")
		if err != ErrImageRequired {
			t.Fatalf("expected ErrImageRequired, got %v", err)
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl when image is missing")
		}
	})

	t.Run("validates inputs", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("bad;name", "test-ns", "img", "latest")
		if err == nil {
			t.Fatal("expected error for invalid name")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl with invalid input")
		}
	})

	t.Run("rejects tag with control characters", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "repo/image", "bad\n")
		if err == nil {
			t.Fatal("expected error for invalid tag")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl with invalid tag")
		}
	})

	t.Run("creates manifest and applies via kubectl", func(t *testing.T) {
		var captured []byte
		mockExec := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{
					Args: spec.Args,
					RunFunc: func() error {
						for i, arg := range spec.Args {
							if arg == "-f" && i+1 < len(spec.Args) {
								data, err := os.ReadFile(spec.Args[i+1])
								if err != nil {
									return err
								}
								captured = data
								break
							}
						}
						return nil
					},
				}
			},
		}
		kubectl := &KubectlClient{exec: mockExec, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "repo/image", "v1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mockExec.Commands) == 0 {
			t.Fatal("expected kubectl command to be called")
		}
		cmd := mockExec.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") {
			t.Errorf("expected apply -f args, got %v", cmd.Args)
		}

		var manifest mcpServerManifest
		if err := yaml.Unmarshal(captured, &manifest); err != nil {
			t.Fatalf("failed to parse manifest: %v", err)
		}
		if manifest.Metadata.Name != "my-server" {
			t.Errorf("expected name my-server, got %q", manifest.Metadata.Name)
		}
		if manifest.Metadata.Namespace != "test-ns" {
			t.Errorf("expected namespace test-ns, got %q", manifest.Metadata.Namespace)
		}
		if manifest.Spec.Image != "repo/image" {
			t.Errorf("expected image repo/image, got %q", manifest.Spec.Image)
		}
		if manifest.Spec.ImageTag != "v1" {
			t.Errorf("expected tag v1, got %q", manifest.Spec.ImageTag)
		}
	})
}

func TestServerManager_CreateServerFromFile(t *testing.T) {
	t.Run("rejects missing file", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServerFromFile("does-not-exist.yaml")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl when file is missing")
		}
	})

	t.Run("rejects directory path", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		dir := t.TempDir()
		err := mgr.CreateServerFromFile(dir)
		if err == nil {
			t.Fatal("expected error for directory path")
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl when path is a directory")
		}
	})

	t.Run("applies file via kubectl with absolute path", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		tmpFile, err := os.CreateTemp("", "mcpserver-test-*.yaml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		if _, err := tmpFile.WriteString("apiVersion: v1\nkind: Namespace\n"); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		if err := tmpFile.Close(); err != nil {
			t.Fatalf("failed to close temp file: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

		err = mgr.CreateServerFromFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}
		absPath, err := filepath.Abs(tmpFile.Name())
		if err != nil {
			t.Fatalf("failed to abs temp file path: %v", err)
		}
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, absPath) {
			t.Errorf("expected apply -f %s, got %v", absPath, cmd.Args)
		}
	})
}

func TestServerManager_ViewServerLogs(t *testing.T) {
	t.Run("builds logs command without follow", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ViewServerLogs("my-server", "test-ns", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "logs") || !contains(cmd.Args, "-l") || !contains(cmd.Args, "-n") {
			t.Errorf("unexpected args: %v", cmd.Args)
		}
		if contains(cmd.Args, "-f") {
			t.Errorf("did not expect -f in args: %v", cmd.Args)
		}
	})

	t.Run("adds follow flag when requested", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ViewServerLogs("my-server", "test-ns", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "-f") {
			t.Errorf("expected -f in args: %v", cmd.Args)
		}
	})
}

func TestValidateManifestValue(t *testing.T) {
	t.Run("trims and returns value", func(t *testing.T) {
		got, err := validateManifestValue("field", "  value  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "value" {
			t.Fatalf("expected trimmed value, got %q", got)
		}
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := validateManifestValue("field", "   ")
		if err == nil {
			t.Fatal("expected error for empty value")
		}
	})

	t.Run("rejects control characters", func(t *testing.T) {
		_, err := validateManifestValue("field", "bad\t")
		if err == nil {
			t.Fatal("expected error for control characters")
		}
	})
}

func TestValidateServerInput(t *testing.T) {
	t.Run("returns sanitized values for valid input", func(t *testing.T) {
		name, namespace, err := validateServerInput("my-server", "test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "my-server" || namespace != "test-ns" {
			t.Fatalf("unexpected values: name=%q namespace=%q", name, namespace)
		}
	})
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func TestServerCmdSubcommandRunE(t *testing.T) {
	t.Run("list_cmd_executes", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		cmd := mgr.newServerListCmd()
		err := cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}
	})

	t.Run("get_cmd_executes", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		cmd := mgr.newServerGetCmd()
		err := cmd.RunE(cmd, []string{"my-server"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}
	})

	t.Run("delete_cmd_executes", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		cmd := mgr.newServerDeleteCmd()
		err := cmd.RunE(cmd, []string{"my-server"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}
	})

	t.Run("status_cmd_executes", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("server1|image:tag|1|/path|false\n"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		cmd := mgr.newServerStatusCmd()
		err := cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("logs_cmd_executes", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		cmd := mgr.newServerLogsCmd()
		err := cmd.RunE(cmd, []string{"my-server"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("create_cmd_with_file", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		tmpFile, err := os.CreateTemp("", "mcpserver-*.yaml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.WriteString("apiVersion: v1\nkind: MCPServer\n"); err != nil {
			t.Fatal(err)
		}
		tmpFile.Close()

		cmd := mgr.newServerCreateCmd()
		if err := cmd.Flags().Set("file", tmpFile.Name()); err != nil {
			t.Fatal(err)
		}
		err = cmd.RunE(cmd, []string{"my-server"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(mock.LastCommand().Args, "apply") {
			t.Error("expected apply command")
		}
	})
}

func TestValidateServerInputErrors(t *testing.T) {
	t.Run("rejects invalid namespace", func(t *testing.T) {
		_, _, err := validateServerInput("my-server", "bad\tns")
		if err == nil {
			t.Fatal("expected error for invalid namespace")
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		_, _, err := validateServerInput("my-server", "   ")
		if err == nil {
			t.Fatal("expected error for empty namespace")
		}
	})
}

func TestServerManager_GetServerSuccess(t *testing.T) {
	mock := &MockExecutor{
		DefaultOutput: []byte("apiVersion: v1\nkind: MCPServer"),
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewServerManager(kubectl, zap.NewNop())

	err := mgr.GetServer("my-server", "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(mock.LastCommand().Args, "get") {
		t.Error("expected get command")
	}
}

func TestServerManager_CreateServerErrors(t *testing.T) {
	t.Run("rejects invalid image with control chars", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "bad\nimage", "latest")
		if err == nil {
			t.Fatal("expected error for invalid image")
		}
	})

	t.Run("handles kubectl apply error", func(t *testing.T) {
		mock := &MockExecutor{DefaultRunErr: errors.New("apply failed")}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "repo/image", "latest")
		if err == nil {
			t.Fatal("expected error when kubectl fails")
		}
	})
}

func TestServerManager_ViewServerLogsError(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewServerManager(kubectl, zap.NewNop())

	err := mgr.ViewServerLogs("bad;name", "test-ns", false)
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if len(mock.Commands) > 0 {
		t.Error("should not call kubectl with invalid name")
	}
}

func TestServerManager_ServerStatus(t *testing.T) {
	t.Run("handles empty servers list", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte(""),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No MCP servers found") {
			t.Errorf("expected 'No MCP servers found' message, got: %s", buf.String())
		}
	})

	t.Run("handles server list with provisioned registry", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|true\n")
				} else if contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\npod-1 true Running 0\n")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "provisioned") {
			t.Errorf("expected 'provisioned' in output, got: %s", buf.String())
		}
	})

	t.Run("handles kubectl get mcpserver error", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultErr: errors.New("not found"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err == nil {
			t.Fatal("expected error when kubectl fails")
		}
	})

	t.Run("handles get pods error gracefully", func(t *testing.T) {
		callCount := 0
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				callCount++
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n")
				} else if contains(spec.Args, "pods") {
					cmd.RunErr = errors.New("pods not found")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles whitespace-only lines in server output", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n   \n\n")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles pods command with no pods found", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n")
				} else if contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\n")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
