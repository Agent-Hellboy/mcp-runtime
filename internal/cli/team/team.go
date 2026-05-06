package team

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(NewManager(runtime.Logger()))
}

func NewWithManager(mgr *Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Manage internal platform teams",
		Long:  "Create and list internal teams through the platform API.",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ListTeams()
		},
	}

	var name string
	createCmd := &cobra.Command{
		Use:   "create [slug]",
		Short: "Create a team and managed namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.CreateTeam(args[0], name)
		},
	}
	createCmd.Flags().StringVar(&name, "name", "", "Display name for the team (defaults to slug)")

	cmd.AddCommand(listCmd, createCmd)
	return cmd
}
