package adapter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mcp-runtime/internal/cli/core"
)

func TestAdapterCommandRegistersProxyAndStdio(t *testing.T) {
	t.Parallel()

	cmd := New(core.NewRuntime(nil))
	subs := map[string]bool{}
	for _, child := range cmd.Commands() {
		subs[child.Use] = true
	}
	for _, want := range []string{"proxy", "stdio"} {
		if !subs[want] {
			t.Fatalf("adapter command missing %q subcommand; got %v", want, subs)
		}
	}
}

func TestProxyCommandValidatesIdentity(t *testing.T) {
	t.Parallel()

	cmd := New(core.NewRuntime(nil))
	cmd.SetArgs([]string{"proxy", "--runtime-url", "http://localhost:18080/demo/mcp", "--human-id", "h", "--agent-id", "a"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want missing session error")
	}
	if !strings.Contains(err.Error(), "MCP_RUNTIME_SESSION_ID") {
		t.Fatalf("Execute() error = %q, want session validation error", err)
	}
}

func TestProxyCommandRejectsBadRuntimeURL(t *testing.T) {
	t.Parallel()

	cmd := New(core.NewRuntime(nil))
	cmd.SetArgs([]string{"proxy", "--runtime-url", "file:///etc/passwd", "--human-id", "h", "--agent-id", "a", "--session-id", "s"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want URL scheme error")
	}
	if !strings.Contains(err.Error(), "http or https") && !strings.Contains(err.Error(), "absolute HTTP URL") {
		t.Fatalf("Execute() error = %q, want URL scheme error", err)
	}
}

func TestIdentityFlagsToConfigParsesTeamAndTimeout(t *testing.T) {
	t.Parallel()

	flags := identityFlags{
		runtimeURL:     "http://localhost:18080/demo/mcp",
		humanID:        "human-1",
		agentID:        "agent-1",
		teamID:         "team-acme",
		sessionID:      "sess-1",
		requestTimeout: 45 * time.Second,
	}
	cfg, err := flags.toConfig()
	if err != nil {
		t.Fatalf("toConfig() error = %v", err)
	}
	if cfg.TeamID != "team-acme" {
		t.Fatalf("TeamID = %q, want team-acme", cfg.TeamID)
	}
	if cfg.RequestTimeout != 45*time.Second {
		t.Fatalf("RequestTimeout = %s, want 45s", cfg.RequestTimeout)
	}
	if cfg.RuntimeURL == nil || cfg.RuntimeURL.Host != "localhost:18080" {
		t.Fatalf("RuntimeURL = %v, want parsed localhost:18080", cfg.RuntimeURL)
	}
}
