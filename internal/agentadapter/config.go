package agentadapter

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	EnvRuntimeURL       = "MCP_RUNTIME_URL"
	EnvHumanID          = "MCP_RUNTIME_HUMAN_ID"
	EnvAgentID          = "MCP_RUNTIME_AGENT_ID"
	EnvTeamID           = "MCP_RUNTIME_TEAM_ID"
	EnvSessionID        = "MCP_RUNTIME_SESSION_ID"
	EnvHostHeader       = "MCP_RUNTIME_HOST_HEADER"
	EnvListenAddr       = "MCP_RUNTIME_LISTEN_ADDR"
	EnvProtocolVersion  = "MCP_RUNTIME_PROTOCOL_VERSION"
	EnvSetXForwarded    = "MCP_RUNTIME_SET_XFF"
	EnvRequestTimeout   = "MCP_RUNTIME_REQUEST_TIMEOUT"
	EnvLogLevel         = "MCP_RUNTIME_LOG_LEVEL"
	EnvAnonymous        = "MCP_RUNTIME_ANONYMOUS"
	EnvAnonymousMethods = "MCP_RUNTIME_ANONYMOUS_METHODS"

	DefaultListenAddr      = "127.0.0.1:8099"
	DefaultProtocolVersion = "2025-06-18"

	HumanIDHeader      = "X-MCP-Human-ID"
	AgentIDHeader      = "X-MCP-Agent-ID"
	TeamIDHeader       = "X-MCP-Team-ID"
	AgentSessionHeader = "X-MCP-Agent-Session"
	MCPProtocolHeader  = "Mcp-Protocol-Version"
	MCPSessionHeader   = "Mcp-Session-Id"
)

type envLookup func(string) string

// ProxyConfig configures the local HTTP reverse-proxy adapter that exposes
// Streamable HTTP MCP to an agent SDK.
type ProxyConfig struct {
	RuntimeURL        *url.URL
	Identity          Identity
	Transport         *RuntimeTransport
	HostHeader        string
	ListenAddr        string
	ProtocolVersion   string
	LogLevel          string
	LogWriter         io.Writer
	DisableXForwarded bool
}

// ShimConfig configures the stdio adapter that bridges newline-delimited
// JSON-RPC MCP traffic to the runtime over HTTP.
type ShimConfig struct {
	RuntimeURL      *url.URL
	Identity        Identity
	Transport       *RuntimeTransport
	HostHeader      string
	ProtocolVersion string
	LogLevel        string
	LogWriter       io.Writer
	// Anonymous, when true, relaxes identity validation so the shim can forward
	// to public/read-only runtime routes without a session or human/agent ID.
	// Only methods in AnonymousMethods are forwarded; all others are rejected
	// with a JSON-RPC error before reaching the runtime.
	Anonymous bool
	// AnonymousMethods is the allowlist used when Anonymous is true. When empty
	// the DefaultAnonymousMethods list applies.
	AnonymousMethods []string
}

// DefaultAnonymousMethods is the set of MCP methods the stdio shim allows in
// anonymous mode when no explicit AnonymousMethods list is configured. These
// are read-only discovery methods and the protocol handshake.
var DefaultAnonymousMethods = []string{
	"initialize",
	"notifications/initialized",
	"ping",
	"tools/list",
	"resources/list",
	"prompts/list",
}

// LoadProxyConfigFromEnv loads HTTP proxy configuration from environment
// variables.
func LoadProxyConfigFromEnv() (ProxyConfig, error) { return loadProxyConfig(os.Getenv) }

// LoadShimConfigFromEnv loads stdio shim configuration from environment
// variables.
func LoadShimConfigFromEnv() (ShimConfig, error) { return loadShimConfig(os.Getenv) }

func loadProxyConfig(lookup envLookup) (ProxyConfig, error) {
	parsed, err := parseSharedEnv(lookup)
	if err != nil {
		return ProxyConfig{}, err
	}
	cfg := ProxyConfig{
		RuntimeURL:      parsed.runtimeURL,
		Identity:        parsed.identity,
		Transport:       parsed.transport,
		HostHeader:      parsed.hostHeader,
		ProtocolVersion: parsed.protocolVersion,
		LogLevel:        parsed.logLevel,
		ListenAddr:      strings.TrimSpace(lookup(EnvListenAddr)),
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if raw := strings.TrimSpace(lookup(EnvSetXForwarded)); raw != "" {
		setXForwarded, err := parseAdapterBool(raw)
		if err != nil {
			return ProxyConfig{}, fmt.Errorf("%s is invalid: %w", EnvSetXForwarded, err)
		}
		cfg.DisableXForwarded = !setXForwarded
	}
	if err := cfg.Validate(); err != nil {
		return ProxyConfig{}, err
	}
	return cfg, nil
}

func loadShimConfig(lookup envLookup) (ShimConfig, error) {
	parsed, err := parseSharedEnv(lookup)
	if err != nil {
		return ShimConfig{}, err
	}
	cfg := ShimConfig{
		RuntimeURL:      parsed.runtimeURL,
		Identity:        parsed.identity,
		Transport:       parsed.transport,
		HostHeader:      parsed.hostHeader,
		ProtocolVersion: parsed.protocolVersion,
		LogLevel:        parsed.logLevel,
	}
	if raw := strings.TrimSpace(lookup(EnvAnonymous)); raw != "" {
		anon, err := parseAdapterBool(raw)
		if err != nil {
			return ShimConfig{}, fmt.Errorf("%s is invalid: %w", EnvAnonymous, err)
		}
		cfg.Anonymous = anon
	}
	if raw := strings.TrimSpace(lookup(EnvAnonymousMethods)); raw != "" {
		cfg.AnonymousMethods = SplitTrimmed(raw, ",")
	}
	if err := cfg.Validate(); err != nil {
		return ShimConfig{}, err
	}
	return cfg, nil
}

// Validate enforces the runtime identity invariants for the HTTP proxy.
func (cfg ProxyConfig) Validate() error {
	return validateRequiredIdentity(cfg.RuntimeURL, cfg.Identity)
}

// Validate enforces the runtime identity invariants for the stdio shim.
// In anonymous mode only the runtime URL is required.
func (cfg ShimConfig) Validate() error {
	if cfg.Anonymous {
		if cfg.RuntimeURL == nil {
			return fmt.Errorf("missing required environment variable: %s", EnvRuntimeURL)
		}
		return nil
	}
	return validateRequiredIdentity(cfg.RuntimeURL, cfg.Identity)
}

// transportOrDefault returns the configured transport, allocating a default
// (no base, no timeout) when the caller did not provide one.
func (cfg ProxyConfig) transportOrDefault() *RuntimeTransport {
	if cfg.Transport != nil {
		return cfg.Transport
	}
	return &RuntimeTransport{}
}

type sharedEnv struct {
	runtimeURL      *url.URL
	identity        Identity
	transport       *RuntimeTransport
	hostHeader      string
	protocolVersion string
	logLevel        string
}

func parseSharedEnv(lookup envLookup) (sharedEnv, error) {
	out := sharedEnv{
		identity: Identity{
			HumanID:   strings.TrimSpace(lookup(EnvHumanID)),
			AgentID:   strings.TrimSpace(lookup(EnvAgentID)),
			TeamID:    strings.TrimSpace(lookup(EnvTeamID)),
			SessionID: strings.TrimSpace(lookup(EnvSessionID)),
		},
		hostHeader:      strings.TrimSpace(lookup(EnvHostHeader)),
		protocolVersion: strings.TrimSpace(lookup(EnvProtocolVersion)),
		logLevel:        strings.TrimSpace(lookup(EnvLogLevel)),
	}
	if out.protocolVersion == "" {
		out.protocolVersion = DefaultProtocolVersion
	}

	if raw := strings.TrimSpace(lookup(EnvRuntimeURL)); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil {
			return sharedEnv{}, fmt.Errorf("%s is invalid: %w", EnvRuntimeURL, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return sharedEnv{}, fmt.Errorf("%s must be an absolute HTTP URL", EnvRuntimeURL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return sharedEnv{}, fmt.Errorf("%s must use http or https", EnvRuntimeURL)
		}
		out.runtimeURL = parsed
	}
	if raw := strings.TrimSpace(lookup(EnvRequestTimeout)); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return sharedEnv{}, fmt.Errorf("%s is invalid: %w", EnvRequestTimeout, err)
		}
		if timeout <= 0 {
			return sharedEnv{}, fmt.Errorf("%s must be greater than zero", EnvRequestTimeout)
		}
		out.transport = &RuntimeTransport{Timeout: timeout}
	}
	return out, nil
}

func validateRequiredIdentity(runtimeURL *url.URL, id Identity) error {
	var missing []string
	if runtimeURL == nil {
		missing = append(missing, EnvRuntimeURL)
	}
	if strings.TrimSpace(id.HumanID) == "" {
		missing = append(missing, EnvHumanID)
	}
	if strings.TrimSpace(id.AgentID) == "" {
		missing = append(missing, EnvAgentID)
	}
	if strings.TrimSpace(id.SessionID) == "" {
		missing = append(missing, EnvSessionID)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func parseAdapterBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected true or false")
	}
}

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func SplitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
