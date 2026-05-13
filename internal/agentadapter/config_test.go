package agentadapter

import (
	"strings"
	"testing"
	"time"
)

func TestLoadProxyConfigRequiresRuntimeIdentityAndSession(t *testing.T) {
	t.Parallel()

	_, err := loadProxyConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("loadProxyConfig() error = nil, want missing environment error")
	}
	for _, name := range []string{EnvRuntimeURL, EnvHumanID, EnvAgentID, EnvSessionID} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("loadProxyConfig() error = %q, missing %s", err, name)
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
	_, err := loadShimConfig(func(key string) string { return env[key] })
	if err == nil {
		t.Fatal("loadShimConfig() error = nil, want URL scheme error")
	}
	if !strings.Contains(err.Error(), "must be an absolute HTTP URL") && !strings.Contains(err.Error(), "must use http or https") {
		t.Fatalf("loadShimConfig() error = %q, want HTTP URL error", err)
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
	cfg, err := loadProxyConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadProxyConfig() error = %v", err)
	}
	if cfg.ListenAddr != DefaultListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, DefaultListenAddr)
	}
	if cfg.ProtocolVersion != DefaultProtocolVersion {
		t.Fatalf("ProtocolVersion = %q, want %q", cfg.ProtocolVersion, DefaultProtocolVersion)
	}
	if cfg.DisableXForwarded {
		t.Fatal("DisableXForwarded = true, want default proxy X-Forwarded headers enabled")
	}
	if cfg.Transport != nil {
		t.Fatalf("Transport = %#v, want nil so adapters get the default RuntimeTransport", cfg.Transport)
	}
	if cfg.LogLevel != "" {
		t.Fatalf("LogLevel = %q, want empty default", cfg.LogLevel)
	}
}

func TestLoadShimConfigDoesNotSetDefaultTransport(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	cfg, err := loadShimConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadShimConfig() error = %v", err)
	}
	if cfg.Transport != nil {
		t.Fatalf("Transport = %#v, want nil so stdio shim allocates a default RuntimeTransport", cfg.Transport)
	}
}

func TestLoadConfigParsesOptionalTeamID(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvTeamID:     "team-acme",
		EnvSessionID:  "session-1",
	}
	cfg, err := loadShimConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadShimConfig() error = %v", err)
	}
	if cfg.Identity.TeamID != "team-acme" {
		t.Fatalf("Identity.TeamID = %q, want team-acme", cfg.Identity.TeamID)
	}
}

func TestLoadConfigTeamIDIsOptional(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	cfg, err := loadShimConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadShimConfig() error = %v", err)
	}
	if cfg.Identity.TeamID != "" {
		t.Fatalf("Identity.TeamID = %q, want empty default", cfg.Identity.TeamID)
	}
}

func TestLoadConfigParsesOptionalRuntimeControls(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL:     "http://localhost:18080/demo/mcp",
		EnvHumanID:        "human-1",
		EnvAgentID:        "agent-1",
		EnvSessionID:      "session-1",
		EnvSetXForwarded:  "false",
		EnvRequestTimeout: "300s",
		EnvLogLevel:       "info",
	}
	cfg, err := loadProxyConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadProxyConfig() error = %v", err)
	}
	if !cfg.DisableXForwarded {
		t.Fatal("DisableXForwarded = false, want true when MCP_RUNTIME_SET_XFF=false")
	}
	if cfg.Transport == nil || cfg.Transport.Timeout != 300*time.Second {
		t.Fatalf("Transport.Timeout = %v, want 300s", cfg.Transport)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
}

func TestLoadConfigRejectsInvalidRuntimeControls(t *testing.T) {
	t.Parallel()

	baseEnv := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{name: "set xff", key: EnvSetXForwarded, val: "maybe"},
		{name: "request timeout", key: EnvRequestTimeout, val: "0s"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			for key, val := range baseEnv {
				env[key] = val
			}
			env[tt.key] = tt.val
			_, err := loadProxyConfig(func(key string) string { return env[key] })
			if err == nil {
				t.Fatal("loadProxyConfig() error = nil, want invalid runtime control error")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("loadProxyConfig() error = %q, want %s", err, tt.key)
			}
		})
	}
}
