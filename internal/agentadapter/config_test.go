package agentadapter

import (
	"strings"
	"testing"
	"time"
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
	if cfg.DisableXForwarded {
		t.Fatal("DisableXForwarded = true, want default proxy X-Forwarded headers enabled")
	}
	if cfg.RequestTimeout != 0 {
		t.Fatalf("RequestTimeout = %s, want unbounded default", cfg.RequestTimeout)
	}
	if cfg.LogLevel != "" {
		t.Fatalf("LogLevel = %q, want empty default", cfg.LogLevel)
	}
}

func TestLoadShimConfigDoesNotSetDefaultHTTPTimeout(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvRuntimeURL: "http://localhost:18080/demo/mcp",
		EnvHumanID:    "human-1",
		EnvAgentID:    "agent-1",
		EnvSessionID:  "session-1",
	}
	cfg, err := loadConfig(func(key string) string { return env[key] }, false)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.HTTPClient != nil {
		t.Fatalf("HTTPClient = %#v, want nil so stdio shim uses an unbounded default client", cfg.HTTPClient)
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
	cfg, err := loadConfig(func(key string) string { return env[key] }, true)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if !cfg.DisableXForwarded {
		t.Fatal("DisableXForwarded = false, want true when MCP_RUNTIME_SET_XFF=false")
	}
	if cfg.RequestTimeout != 300*time.Second {
		t.Fatalf("RequestTimeout = %s, want 300s", cfg.RequestTimeout)
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			for key, val := range baseEnv {
				env[key] = val
			}
			env[tt.key] = tt.val
			_, err := loadConfig(func(key string) string { return env[key] }, true)
			if err == nil {
				t.Fatal("loadConfig() error = nil, want invalid runtime control error")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("loadConfig() error = %q, want %s", err, tt.key)
			}
		})
	}
}
