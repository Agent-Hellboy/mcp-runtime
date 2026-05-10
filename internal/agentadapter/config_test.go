package agentadapter

import (
	"strings"
	"testing"
)

func TestLoadProxyConfigRequiresRuntimeIdentityAndSession(t *testing.T) {
	t.Parallel()

	_, err := loadConfig(func(string) string { return "" }, true)
	if err == nil {
		t.Fatal("loadConfig() error = nil, want missing environment error")
	}
	for _, name := range []string{EnvRuntimeURL, EnvHumanID, EnvAgentID, EnvSessionID} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("loadConfig() error = %q, missing %s", err, name)
		}
	}
}

func TestLoadShimConfigRejectsNonHTTPRuntimeURL(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "file:///tmp/mcp.sock",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	_, err := loadConfig(func(key string) string { return env[key] }, false)
	if err == nil {
		t.Fatal("loadConfig() error = nil, want URL scheme error")
	}
	if !strings.Contains(err.Error(), "must be an absolute HTTP URL") && !strings.Contains(err.Error(), "must use http or https") {
		t.Fatalf("loadConfig() error = %q, want HTTP URL error", err)
	}
}

func TestLoadProxyConfigAppliesDefaults(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	cfg, err := loadConfig(func(key string) string { return env[key] }, true)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.ListenAddr != DefaultListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, DefaultListenAddr)
	}
	if cfg.ProtocolVersion != DefaultProtocolVersion {
		t.Fatalf("ProtocolVersion = %q, want %q", cfg.ProtocolVersion, DefaultProtocolVersion)
	}
}
