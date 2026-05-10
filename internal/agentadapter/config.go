package agentadapter

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	EnvRuntimeURL      = "MCP_RUNTIME_URL"
	EnvHumanID         = "MCP_RUNTIME_HUMAN_ID"
	EnvAgentID         = "MCP_RUNTIME_AGENT_ID"
	EnvSessionID       = "MCP_RUNTIME_SESSION_ID"
	EnvHostHeader      = "MCP_RUNTIME_HOST_HEADER"
	EnvListenAddr      = "MCP_RUNTIME_LISTEN_ADDR"
	EnvProtocolVersion = "MCP_RUNTIME_PROTOCOL_VERSION"

	DefaultListenAddr      = "127.0.0.1:8099"
	DefaultProtocolVersion = "2025-06-18"

	HumanIDHeader          = "X-MCP-Human-ID"
	AgentIDHeader          = "X-MCP-Agent-ID"
	AgentSessionHeader     = "X-MCP-Agent-Session"
	MCPProtocolHeader      = "Mcp-Protocol-Version"
	MCPSessionHeader       = "Mcp-Session-Id"
	defaultHTTPClientLimit = 60 * time.Second
)

type envLookup func(string) string

// Config is the shared configuration for agent-side adapters.
type Config struct {
	RuntimeURL      *url.URL
	HumanID         string
	AgentID         string
	SessionID       string
	HostHeader      string
	ListenAddr      string
	ProtocolVersion string
	HTTPClient      *http.Client
}

// LoadProxyConfigFromEnv loads HTTP proxy configuration from environment variables.
func LoadProxyConfigFromEnv() (Config, error) {
	return loadConfig(os.Getenv, true)
}

// LoadShimConfigFromEnv loads stdio shim configuration from environment variables.
func LoadShimConfigFromEnv() (Config, error) {
	return loadConfig(os.Getenv, false)
}

func loadConfig(lookup envLookup, includeListen bool) (Config, error) {
	cfg := Config{
		HumanID:         strings.TrimSpace(lookup(EnvHumanID)),
		AgentID:         strings.TrimSpace(lookup(EnvAgentID)),
		SessionID:       strings.TrimSpace(lookup(EnvSessionID)),
		HostHeader:      strings.TrimSpace(lookup(EnvHostHeader)),
		ProtocolVersion: strings.TrimSpace(lookup(EnvProtocolVersion)),
		HTTPClient:      &http.Client{Timeout: defaultHTTPClientLimit},
	}
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = DefaultProtocolVersion
	}
	if includeListen {
		cfg.ListenAddr = strings.TrimSpace(lookup(EnvListenAddr))
		if cfg.ListenAddr == "" {
			cfg.ListenAddr = DefaultListenAddr
		}
	}

	rawRuntimeURL := strings.TrimSpace(lookup(EnvRuntimeURL))
	if rawRuntimeURL != "" {
		parsed, err := url.Parse(rawRuntimeURL)
		if err != nil {
			return Config{}, fmt.Errorf("%s is invalid: %w", EnvRuntimeURL, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return Config{}, fmt.Errorf("%s must be an absolute HTTP URL", EnvRuntimeURL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return Config{}, fmt.Errorf("%s must use http or https", EnvRuntimeURL)
		}
		cfg.RuntimeURL = parsed
	}

	if err := ValidateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateConfig checks the common adapter invariants without reading process state.
func ValidateConfig(cfg Config) error {
	var missing []string
	if cfg.RuntimeURL == nil {
		missing = append(missing, EnvRuntimeURL)
	}
	if strings.TrimSpace(cfg.HumanID) == "" {
		missing = append(missing, EnvHumanID)
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		missing = append(missing, EnvAgentID)
	}
	if strings.TrimSpace(cfg.SessionID) == "" {
		missing = append(missing, EnvSessionID)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func applyGovernanceHeaders(headers http.Header, cfg Config) {
	headers.Del(HumanIDHeader)
	headers.Del(AgentIDHeader)
	headers.Del(AgentSessionHeader)
	headers.Set(HumanIDHeader, cfg.HumanID)
	headers.Set(AgentIDHeader, cfg.AgentID)
	headers.Set(AgentSessionHeader, cfg.SessionID)
}
