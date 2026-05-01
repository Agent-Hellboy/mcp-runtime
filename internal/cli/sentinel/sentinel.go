// Package sentinel owns routing for the sentinel top-level command.
package sentinel

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

// New returns the sentinel command.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(DefaultSentinelManager(runtime))
}

// NewWithManager returns the sentinel command using the provided manager.
func NewWithManager(mgr *SentinelManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sentinel",
		Short: "Operate the bundled mcp-sentinel stack",
		Long:  "Commands for inspecting and operating the bundled mcp-sentinel analytics, gateway, and observability stack.",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show mcp-sentinel stack status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ShowSentinelStatus()
		},
	}

	var follow bool
	var previous bool
	var tail int
	var since string
	logsCmd := &cobra.Command{
		Use:       "logs [component]",
		Short:     "View logs for a mcp-sentinel component",
		Args:      cobra.ExactArgs(1),
		ValidArgs: ComponentKeys(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ViewSentinelLogs(args[0], follow, previous, tail, since)
		},
	}
	logsCmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	logsCmd.Flags().BoolVar(&previous, "previous", false, "Show logs from the previous container instance")
	logsCmd.Flags().IntVar(&tail, "tail", 200, "Number of recent log lines to show (-1 for all)")
	logsCmd.Flags().StringVar(&since, "since", "", "Only return logs newer than a relative duration like 5m or 1h")

	eventsCmd := &cobra.Command{
		Use:   "events",
		Short: "Show recent Kubernetes events for mcp-sentinel",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ShowSentinelEvents()
		},
	}

	var localPort int
	var address string
	portForwardCmd := &cobra.Command{
		Use:   "port-forward [target]",
		Short: "Port-forward a common mcp-sentinel service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.PortForwardSentinelTarget(args[0], localPort, address)
		},
	}
	portForwardCmd.Flags().IntVar(&localPort, "port", 0, "Local port to bind (defaults to the target service port)")
	portForwardCmd.Flags().StringVar(&address, "address", "127.0.0.1", "Addresses to listen on")

	var restartAll bool
	restartCmd := &cobra.Command{
		Use:   "restart [component]",
		Short: "Restart one or all mcp-sentinel workloads",
		Args: func(cmd *cobra.Command, args []string) error {
			if restartAll && len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			component := ""
			if len(args) > 0 {
				component = args[0]
			}
			return mgr.RestartSentinel(component, restartAll)
		},
	}
	restartCmd.Flags().BoolVar(&restartAll, "all", false, "Restart every mcp-sentinel workload")

	cmd.AddCommand(statusCmd, logsCmd, eventsCmd, portForwardCmd, restartCmd)
	return cmd
}
