// Package adapter routes the adapter top-level command for the agent-side
// HTTP proxy and stdio shim that inject issued governance identity headers.
package adapter

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

// New returns the adapter command.
func New(runtime *core.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Run agent-side adapters that inject issued MCP governance headers",
		Long: `Adapter commands forward agent MCP traffic to a configured platform-issued
runtime route while attaching the human, agent, team, and session identity
headers the gateway enforces.

Two transports are available:

  mcp-runtime adapter proxy   Local Streamable HTTP listener (for SDKs that
                              speak MCP over HTTP).
  mcp-runtime adapter stdio   Stdio bridge for IDE-style clients that launch an
                              MCP server as a subprocess.

The adapter does not create grants or sessions. Issue them first with
` + "`mcp-runtime access grant apply`" + ` / ` + "`mcp-runtime access session apply`" + ` (or
through the platform UI/API), then point the adapter at the runtime route with
the returned identity values.`,
	}
	cmd.AddCommand(newProxyCmd(runtime))
	cmd.AddCommand(newStdioCmd(runtime))
	return cmd
}
