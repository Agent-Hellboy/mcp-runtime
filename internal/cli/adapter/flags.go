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

// toConfig converts the parsed flags into an agentadapter.Config, validating
// the runtime URL and request timeout on the CLI side so error messages are
// user-readable and stay consistent with agentadapter.loadConfig.
func (f identityFlags) toConfig() (agentadapter.Config, error) {
	cfg := agentadapter.Config{
		HumanID:           strings.TrimSpace(f.humanID),
		AgentID:           strings.TrimSpace(f.agentID),
		TeamID:            strings.TrimSpace(f.teamID),
		SessionID:         strings.TrimSpace(f.sessionID),
		HostHeader:        strings.TrimSpace(f.hostHeader),
		ProtocolVersion:   strings.TrimSpace(f.protocolVersion),
		LogLevel:          strings.TrimSpace(f.logLevel),
		DisableXForwarded: f.disableXFF,
	}
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = agentadapter.DefaultProtocolVersion
	}

	if raw := strings.TrimSpace(f.requestTimeout); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return agentadapter.Config{}, fmt.Errorf("--request-timeout (or $%s) is invalid: %w", agentadapter.EnvRequestTimeout, err)
		}
		if timeout <= 0 {
			return agentadapter.Config{}, fmt.Errorf("--request-timeout (or $%s) must be greater than zero", agentadapter.EnvRequestTimeout)
		}
		cfg.RequestTimeout = timeout
	}

	rawURL := strings.TrimSpace(f.runtimeURL)
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return agentadapter.Config{}, fmt.Errorf("--runtime-url is invalid: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return agentadapter.Config{}, fmt.Errorf("--runtime-url must be an absolute HTTP URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return agentadapter.Config{}, fmt.Errorf("--runtime-url must use http or https")
		}
		cfg.RuntimeURL = parsed
	}
	return cfg, nil
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
