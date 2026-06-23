package adapter

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/agentadapter"
	"mcp-runtime/internal/cli/core"
)

func newStdioCmd(_ *core.Runtime) *cobra.Command {
	var flags identityFlags
	var sessionFlags platformSessionFlags

	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Bridge stdio MCP traffic to the configured runtime route",
		Long: `Read newline-delimited MCP JSON-RPC messages from stdin, forward each to the
configured platform runtime route over Streamable HTTP, and write the JSON-RPC
responses back to stdout. Designed for IDE-style MCP clients (e.g. Cursor,
Claude Desktop) that launch an MCP server as a subprocess.

Configure identity via flags or the matching MCP_RUNTIME_* environment
variables. Flags win when both are set. With --server, the shim fetches an
issued session from the platform API before reading stdin; identity flags
override the result.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := flags.toShimConfig()
			if err != nil {
				return err
			}
			if flags.mtlsEnabled() && (cfg.RuntimeURL == nil || cfg.RuntimeURL.Scheme != "https") {
				return fmt.Errorf("--auth mtls requires an https --runtime-url (or $%s)", agentadapter.EnvRuntimeURL)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			effective, provider, transport, stopAuth, err := resolveAuth(ctx, flags, &sessionFlags, cfg.Identity, cfg.Transport, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			defer stopAuth()
			cfg.Identity = effective
			cfg.IdentityProvider = provider
			cfg.Transport = transport
			if err := cfg.Validate(); err != nil {
				return err
			}

			return agentadapter.RunStdioShim(ctx, cfg, agentadapter.StdioOptions{
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
			})
		},
	}

	bindIdentityFlags(cmd, &flags)
	bindStdioFlags(cmd, &flags)
	bindPlatformSessionFlags(cmd, &sessionFlags)
	return cmd
}
