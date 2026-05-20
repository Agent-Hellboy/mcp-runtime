package server

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/core"
)

func TestServerManager_ListServers(t *testing.T) {
	t.Run("calls kubectl with correct args", func(t *testing.T) {
		mock := &core.MockExecutor{
			DefaultOutput: []byte("server1\nserver2\n"),
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers("test-ns", "")
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers(" test-ns ", "")
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers("   ", "")
		if err != nil {
			t.Fatalf("unexpected error for empty namespace: %v", err)
		}
		cmd := mock.LastCommand()
		found := false
		for i, arg := range cmd.Args {
			if arg == "-n" && i+1 < len(cmd.Args) && cmd.Args[i+1] == core.NamespaceMCPServers {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected default namespace %q in args, got %v", core.NamespaceMCPServers, cmd.Args)
		}
	})

	t.Run("rejects --team when using kubectl mode", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ListServers("", "team-a")
		if err == nil {
			t.Fatal("expected error when team is set in --use-kube mode")
		}
		if !strings.Contains(err.Error(), "cannot use --team with --use-kube") {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 0 {
			t.Fatalf("expected no kubectl command on validation error, got %d", len(mock.Commands))
		}
	})
}

func TestServerManager_DeleteServer(t *testing.T) {
	t.Run("validates server name", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "", "latest")
		if err != core.ErrImageRequired {
			t.Fatalf("expected core.ErrImageRequired, got %v", err)
		}
		if len(mock.Commands) > 0 {
			t.Error("should not call kubectl when image is missing")
		}
	})

	t.Run("validates inputs", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		var applyCmd *core.MockCommand
		mockExec := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				applyCmd = &core.MockCommand{Args: spec.Args}
				return applyCmd
			},
		}
		kubectl, err := core.NewKubectlClient(mockExec)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
		mgr := NewServerManager(kubectl, zap.NewNop())

		err = mgr.CreateServer("my-server", "test-ns", "repo/image", "v1")
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
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, "-") {
			t.Errorf("expected apply -f args, got %v", cmd.Args)
		}
		captured, err := io.ReadAll(applyCmd.StdinR)
		if err != nil {
			t.Fatalf("failed to read manifest from stdin: %v", err)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
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

	t.Run("applies file via kubectl stdin with default validators", func(t *testing.T) {
		var applyCmd *core.MockCommand
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				applyCmd = &core.MockCommand{Args: spec.Args}
				return applyCmd
			},
		}
		kubectl, err := core.NewKubectlClient(mock)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
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
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, "-") {
			t.Errorf("expected apply -f - args, got %v", cmd.Args)
		}
		captured, err := io.ReadAll(applyCmd.StdinR)
		if err != nil {
			t.Fatalf("failed to read manifest from stdin: %v", err)
		}
		if string(captured) != "apiVersion: v1\nkind: Namespace\n" {
			t.Fatalf("unexpected manifest contents: %q", string(captured))
		}
	})
}

func TestServerManager_ViewServerLogs(t *testing.T) {
	t.Run("builds logs command without follow", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ViewServerLogs("my-server", "test-ns", false, false, 200, "")
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.ViewServerLogs("my-server", "test-ns", true, false, 200, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "-f") {
			t.Errorf("expected -f in args: %v", cmd.Args)
		}
	})
}

func TestServerManager_GetServerSuccess(t *testing.T) {
	mock := &core.MockExecutor{
		DefaultOutput: []byte("apiVersion: v1\nkind: MCPServer"),
	}
	kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "bad\nimage", "latest")
		if err == nil {
			t.Fatal("expected error for invalid image")
		}
	})

	t.Run("handles kubectl apply error", func(t *testing.T) {
		mock := &core.MockExecutor{DefaultRunErr: errors.New("apply failed")}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		err := mgr.CreateServer("my-server", "test-ns", "repo/image", "latest")
		if err == nil {
			t.Fatal("expected error when kubectl fails")
		}
	})
}

func TestServerManager_ViewServerLogsError(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewServerManager(kubectl, zap.NewNop())

	err := mgr.ViewServerLogs("bad;name", "test-ns", false, false, 200, "")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if len(mock.Commands) > 0 {
		t.Error("should not call kubectl with invalid name")
	}
}

func TestServerManager_ServerStatus(t *testing.T) {
	t.Run("handles empty servers list", func(t *testing.T) {
		mock := &core.MockExecutor{
			DefaultOutput: []byte(""),
		}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|true\n")
				} else if contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\npod-1 true Running 0\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{
			DefaultErr: errors.New("not found"),
		}
		kubectl := core.NewTestKubectlClient(mock)
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
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				callCount++
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n")
				} else if contains(spec.Args, "pods") {
					cmd.RunErr = errors.New("pods not found")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles whitespace-only lines in server output", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n   \n\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles pods command with no pods found", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/path|false\n")
				} else if contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewServerManager(kubectl, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ServerStatus("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func setDefaultPrinterWriter(t *testing.T, w *bytes.Buffer) {
	t.Helper()
	orig := core.DefaultPrinter.Writer
	core.DefaultPrinter.Writer = w
	t.Cleanup(func() {
		core.DefaultPrinter.Writer = orig
	})
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func TestBuildDeployServerSpecEnablesGateway(t *testing.T) {
	spec := buildDeployServerSpec("demo", "registry.example.com/team/demo", "v1.0.0", 2, 8088, 80)
	if spec.Gateway == nil || !spec.Gateway.Enabled {
		t.Fatalf("gateway = %#v, want enabled", spec.Gateway)
	}
	if spec.IngressPath != "/demo/mcp" {
		t.Fatalf("ingressPath = %q, want /demo/mcp", spec.IngressPath)
	}
	if len(spec.EnvVars) != 1 || spec.EnvVars[0].Name != "MCP_PATH" || spec.EnvVars[0].Value != "/demo/mcp" {
		t.Fatalf("envVars = %#v", spec.EnvVars)
	}
}

func TestApplyDeployMetadataDefaultsUsesSingleLocalMetadataServer(t *testing.T) {
	tmp := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.Mkdir(".mcp", 0o750); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	if err := os.WriteFile(".mcp/servers.yaml", []byte(`version: v1
servers:
  - name: workspace-assistant-mcp
    description: Workspace assistant metadata
    tools:
      - name: add
        description: Add two numbers.
        requiredTrust: low
        sideEffect: read
      - name: echo
        requiredTrust: low
        sideEffect: read
`), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	spec := buildDeployServerSpec("acme-tools", "registry.example.com/acme/go-example", "v0.1.0", 1, 8088, 80)
	if err := applyDeployMetadataDefaults(&spec, "acme-tools"); err != nil {
		t.Fatalf("applyDeployMetadataDefaults() error = %v", err)
	}
	if spec.Description != "Workspace assistant metadata" {
		t.Fatalf("description = %q", spec.Description)
	}
	if len(spec.Tools) != 2 || spec.Tools[0].Name != "add" || spec.Tools[0].SideEffect != "read" {
		t.Fatalf("tools = %#v", spec.Tools)
	}
	if spec.IngressPath != "/acme-tools/mcp" {
		t.Fatalf("deploy route should stay based on requested name, got %q", spec.IngressPath)
	}
}
