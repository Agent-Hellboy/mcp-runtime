package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestServerManager_ExportServer(t *testing.T) {
	mock := &MockExecutor{
		DefaultOutput: []byte("apiVersion: mcpruntime.org/v1alpha1\nkind: MCPServer\n"),
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewServerManager(kubectl, zap.NewNop())

	outputFile := filepath.Join(t.TempDir(), "exported", "server.yaml")
	if err := mgr.ExportServer("my-server", "team-a", outputFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", "mcpserver", "my-server", "-n", "team-a", "-o", "yaml"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(data) != string(mock.DefaultOutput) {
		t.Fatalf("unexpected output file contents: %q", string(data))
	}
}

func TestServerManager_PatchServerFromFile(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewServerManager(kubectl, zap.NewNop())

	patchFile := filepath.Join(t.TempDir(), "patch.yaml")
	if err := os.WriteFile(patchFile, []byte("spec:\n  policy:\n    mode: allow-list\n    defaultDecision: deny\n"), 0o600); err != nil {
		t.Fatalf("write patch file: %v", err)
	}

	if err := mgr.PatchServer("my-server", "team-a", "merge", "", patchFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"patch", "mcpserver", "my-server", "-n", "team-a", "--type", "merge", "--patch"} {
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

	var patch map[string]any
	if err := json.Unmarshal([]byte(cmd.Args[patchIndex]), &patch); err != nil {
		t.Fatalf("unmarshal normalized patch: %v", err)
	}
	spec, ok := patch["spec"].(map[string]any)
	if !ok {
		t.Fatalf("expected spec object in patch, got %#v", patch)
	}
	policy, ok := spec["policy"].(map[string]any)
	if !ok {
		t.Fatalf("expected policy object in patch, got %#v", spec)
	}
	if policy["mode"] != "allow-list" || policy["defaultDecision"] != "deny" {
		t.Fatalf("unexpected policy patch: %#v", policy)
	}
}

func TestServerManager_InspectServerPolicy(t *testing.T) {
	mock := &MockExecutor{
		DefaultOutput: []byte("{\"policy\":{\"mode\":\"allow-list\"}}"),
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewServerManager(kubectl, zap.NewNop())

	origStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = origStdout
	})

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()

	if err := mgr.InspectServerPolicy("my-server", "team-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = writer.Close()

	output := <-done
	if output == "" {
		t.Fatal("expected rendered policy output")
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"get", "configmap", "my-server-gateway-policy", "-n", "team-a", "-o"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}
