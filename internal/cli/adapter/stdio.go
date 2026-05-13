package adapter

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/agentadapter"
	"mcp-runtime/internal/cli/core"
)

func newStdioCmd(_ *core.Runtime) *cobra.Command {
	var flags identityFlags

	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Bridge stdio MCP traffic to the configured runtime route",
		Long: `Read newline-delimited MCP JSON-RPC messages from stdin, forward each to the
configured platform runtime route over Streamable HTTP, and write the JSON-RPC
responses back to stdout. Designed for IDE-style MCP clients (e.g. Cursor,
Claude Desktop) that launch an MCP server as a subprocess.

Configure identity via flags or the matching MCP_RUNTIME_* environment
variables. Flags win when both are set.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := flags.toConfig()
			if err != nil {
				return err
			}
			if err := agentadapter.ValidateConfig(cfg); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return agentadapter.RunStdioShim(ctx, cfg, agentadapter.StdioOptions{
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
			})
		},
	}

	bindIdentityFlags(cmd, &flags)
	return cmd
}
