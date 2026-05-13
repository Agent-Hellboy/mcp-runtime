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

func newProxyCmd(_ *core.Runtime) *cobra.Command {
	var flags identityFlags
	var listenAddr string

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run a local Streamable HTTP MCP proxy that forwards to the runtime",
		Long: `Start a local HTTP listener that accepts Streamable HTTP MCP traffic from an
agent SDK and forwards each request to the configured platform runtime route,
injecting the issued governance identity headers.

Configure identity via flags or the matching MCP_RUNTIME_* environment
variables. Flags win when both are set.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := flags.toProxyConfig(listenAddr)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Fprintf(cmd.ErrOrStderr(), "mcp-runtime adapter proxy listening on %s -> %s\n",
				cfg.ListenAddr, cfg.RuntimeURL.String())
			return agentadapter.RunHTTPProxy(ctx, cfg)
		},
	}

	bindIdentityFlags(cmd, &flags)
	bindProxyFlags(cmd, &flags)
	cmd.Flags().StringVar(&listenAddr, "listen", os.Getenv(agentadapter.EnvListenAddr),
		"Local listen address (default: $"+agentadapter.EnvListenAddr+" or "+agentadapter.DefaultListenAddr+")")
	return cmd
}
