package adapter

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/agentadapter"
)

// identityFlags binds flags shared by every adapter subcommand. Flag values
// default to the matching environment variable so existing
// MCP_RUNTIME_* deployments keep working.
type identityFlags struct {
	runtimeURL      string
	humanID         string
	agentID         string
	teamID          string
	sessionID       string
	hostHeader      string
	protocolVersion string
	requestTimeout  string
	logLevel        string
	disableXFF      bool
	// stdio-only
	anonymous        bool
	anonymousMethods string
}

func bindIdentityFlags(cmd *cobra.Command, f *identityFlags) {
	cmd.Flags().StringVar(&f.runtimeURL, "runtime-url", os.Getenv(agentadapter.EnvRuntimeURL),
		"Platform-issued absolute MCP runtime URL (default: $"+agentadapter.EnvRuntimeURL+")")
	cmd.Flags().StringVar(&f.humanID, "human-id", os.Getenv(agentadapter.EnvHumanID),
		"Issued human identity (default: $"+agentadapter.EnvHumanID+")")
	cmd.Flags().StringVar(&f.agentID, "agent-id", os.Getenv(agentadapter.EnvAgentID),
		"Issued agent identity (default: $"+agentadapter.EnvAgentID+")")
	cmd.Flags().StringVar(&f.teamID, "team-id", os.Getenv(agentadapter.EnvTeamID),
		"Issued team identity for team-scoped grants (default: $"+agentadapter.EnvTeamID+")")
	cmd.Flags().StringVar(&f.sessionID, "session-id", os.Getenv(agentadapter.EnvSessionID),
		"Issued agent session identity (default: $"+agentadapter.EnvSessionID+")")
	cmd.Flags().StringVar(&f.hostHeader, "host-header", os.Getenv(agentadapter.EnvHostHeader),
		"Override the Host header sent to the runtime (default: $"+agentadapter.EnvHostHeader+")")
	cmd.Flags().StringVar(&f.protocolVersion, "protocol-version", os.Getenv(agentadapter.EnvProtocolVersion),
		"MCP protocol version header to advertise (default: $"+agentadapter.EnvProtocolVersion+" or "+agentadapter.DefaultProtocolVersion+")")
	cmd.Flags().StringVar(&f.logLevel, "log-level", os.Getenv(agentadapter.EnvLogLevel),
		"Adapter log level: info logs runtime denials (default: $"+agentadapter.EnvLogLevel+")")
	cmd.Flags().BoolVar(&f.disableXFF, "no-xforwarded", parseEnvBool(agentadapter.EnvSetXForwarded, false),
		"Do not set X-Forwarded-* headers when forwarding to the runtime")
	cmd.Flags().StringVar(&f.requestTimeout, "request-timeout", os.Getenv(agentadapter.EnvRequestTimeout),
		"HTTP request timeout for adapter→runtime calls, e.g. 30s (default: $"+agentadapter.EnvRequestTimeout+")")
}

// resolved holds the validated cross-cutting pieces of an adapter config —
// identity, runtime URL, transport, and the shared display fields — that
// every subcommand needs before building its transport-specific config.
type resolved struct {
	runtimeURL      *url.URL
	identity        agentadapter.Identity
	transport       *agentadapter.RuntimeTransport
	hostHeader      string
	protocolVersion string
	logLevel        string
}

// resolve parses and validates the shared adapter fields on the CLI side so
// error messages reference the user-facing flag name instead of the env var.
func (f identityFlags) resolve() (resolved, error) {
	out := resolved{
		identity: agentadapter.Identity{
			HumanID:   strings.TrimSpace(f.humanID),
			AgentID:   strings.TrimSpace(f.agentID),
			TeamID:    strings.TrimSpace(f.teamID),
			SessionID: strings.TrimSpace(f.sessionID),
		},
		hostHeader:      strings.TrimSpace(f.hostHeader),
		protocolVersion: strings.TrimSpace(f.protocolVersion),
		logLevel:        strings.TrimSpace(f.logLevel),
	}
	if out.protocolVersion == "" {
		out.protocolVersion = agentadapter.DefaultProtocolVersion
	}

	if raw := strings.TrimSpace(f.requestTimeout); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return resolved{}, fmt.Errorf("--request-timeout (or $%s) is invalid: %w", agentadapter.EnvRequestTimeout, err)
		}
		if timeout <= 0 {
			return resolved{}, fmt.Errorf("--request-timeout (or $%s) must be greater than zero", agentadapter.EnvRequestTimeout)
		}
		out.transport = &agentadapter.RuntimeTransport{Timeout: timeout}
	}

	if raw := strings.TrimSpace(f.runtimeURL); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil {
			return resolved{}, fmt.Errorf("--runtime-url is invalid: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return resolved{}, fmt.Errorf("--runtime-url must be an absolute HTTP URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return resolved{}, fmt.Errorf("--runtime-url must use http or https")
		}
		out.runtimeURL = parsed
	}
	return out, nil
}

// toProxyConfig produces an agentadapter.ProxyConfig from the resolved
// shared fields plus proxy-only listener/XFF settings.
func (f identityFlags) toProxyConfig(listenAddr string) (agentadapter.ProxyConfig, error) {
	r, err := f.resolve()
	if err != nil {
		return agentadapter.ProxyConfig{}, err
	}
	listen := strings.TrimSpace(listenAddr)
	if listen == "" {
		listen = agentadapter.DefaultListenAddr
	}
	return agentadapter.ProxyConfig{
		RuntimeURL:        r.runtimeURL,
		Identity:          r.identity,
		Transport:         r.transport,
		HostHeader:        r.hostHeader,
		ListenAddr:        listen,
		ProtocolVersion:   r.protocolVersion,
		LogLevel:          r.logLevel,
		DisableXForwarded: f.disableXFF,
	}, nil
}

// toShimConfig produces an agentadapter.ShimConfig from the resolved shared
// fields plus stdio-only anonymous settings.
func (f identityFlags) toShimConfig() (agentadapter.ShimConfig, error) {
	r, err := f.resolve()
	if err != nil {
		return agentadapter.ShimConfig{}, err
	}
	cfg := agentadapter.ShimConfig{
		RuntimeURL:      r.runtimeURL,
		Identity:        r.identity,
		Transport:       r.transport,
		HostHeader:      r.hostHeader,
		ProtocolVersion: r.protocolVersion,
		LogLevel:        r.logLevel,
		Anonymous:       f.anonymous,
	}
	if f.anonymous && strings.TrimSpace(f.anonymousMethods) != "" {
		cfg.AnonymousMethods = agentadapter.SplitTrimmed(f.anonymousMethods, ",")
	}
	return cfg, nil
}

// bindStdioFlags adds stdio-specific flags on top of the shared identity flags.
func bindStdioFlags(cmd *cobra.Command, f *identityFlags) {
	cmd.Flags().BoolVar(&f.anonymous, "anonymous",
		parseEnvBoolSimple(agentadapter.EnvAnonymous),
		"Forward to the runtime without a session or issued identity (public/read-only routes); "+
			"only methods in --anonymous-methods are forwarded (default: $"+agentadapter.EnvAnonymous+")")
	cmd.Flags().StringVar(&f.anonymousMethods, "anonymous-methods",
		os.Getenv(agentadapter.EnvAnonymousMethods),
		"Comma-separated list of MCP methods allowed in anonymous mode "+
			"(default: $"+agentadapter.EnvAnonymousMethods+" or initialize,notifications/initialized,ping,tools/list,resources/list,prompts/list)")
}

func parseEnvBool(name string, def bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "":
		return def
	case "1", "t", "true", "y", "yes", "on":
		// MCP_RUNTIME_SET_XFF=true means *enable* X-Forwarded, so disableXFF=false.
		return false
	case "0", "f", "false", "n", "no", "off":
		return true
	default:
		return def
	}
}

func parseEnvBoolSimple(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
