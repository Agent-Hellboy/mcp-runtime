package cli

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewSentinelCmd(t *testing.T) {
	cmd := NewSentinelCmd(zap.NewNop())
	if cmd == nil {
		t.Fatal("NewSentinelCmd should not return nil")
	}
	if cmd.Use != "sentinel" {
		t.Fatalf("expected Use='sentinel', got %q", cmd.Use)
	}

	expected := map[string]bool{
		"status":       false,
		"logs":         false,
		"events":       false,
		"port-forward": false,
		"restart":      false,
	}
	for _, sub := range cmd.Commands() {
		name := strings.Fields(sub.Use)[0]
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Fatalf("expected subcommand %q not found", name)
		}
	}
}

func TestSentinelManager_ViewSentinelLogs(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewSentinelManager(kubectl, zap.NewNop())

	if err := mgr.ViewSentinelLogs("api", true, false, 50, "5m"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	if cmd.Name != "kubectl" {
		t.Fatalf("expected kubectl, got %q", cmd.Name)
	}
	for _, want := range []string{"logs", "-n", defaultAnalyticsNamespace, "-l", "app=mcp-sentinel-api", "--all-containers=true", "--prefix=true", "--tail", "50", "--since", "5m", "-f"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestSentinelManager_PortForwardSentinelTarget(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewSentinelManager(kubectl, zap.NewNop())

	if err := mgr.PortForwardSentinelTarget("grafana", 0, "0.0.0.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"port-forward", "-n", defaultAnalyticsNamespace, "service/grafana", "3000:3000", "--address", "0.0.0.0"} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}

func TestSentinelManager_RestartSentinel(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewSentinelManager(kubectl, zap.NewNop())

	if err := mgr.RestartSentinel("processor", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := mock.LastCommand()
	for _, want := range []string{"rollout", "restart", "deployment/mcp-sentinel-processor", "-n", defaultAnalyticsNamespace} {
		if !contains(cmd.Args, want) {
			t.Fatalf("expected %q in args, got %v", want, cmd.Args)
		}
	}
}
