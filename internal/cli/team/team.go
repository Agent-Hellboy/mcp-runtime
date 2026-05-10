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
		Long:  "Create and list internal teams through the platform API, or initialize Kubernetes team namespaces with RBAC.",
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

	initOpts := InitOptions{}
	initCmd := &cobra.Command{
		Use:   "init [slug]",
		Short: "Initialize a team namespace and RBAC",
		Long:  "Initialize a Kubernetes team namespace, MCP Runtime team-admin RBAC, Traefik watch RBAC, and optionally patch the bundled Traefik watch list.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			initOpts.Slug = args[0]
			return mgr.InitTeam(initOpts)
		},
	}
	initCmd.Flags().StringVar(&initOpts.Namespace, "namespace", "", "Namespace to create (defaults to mcp-team-<slug>)")
	initCmd.Flags().StringVar(&initOpts.Group, "group", "", "Kubernetes group to bind as team admin (defaults to <slug>-mcp-admins)")
	initCmd.Flags().StringSliceVar(&initOpts.Users, "user", nil, "Kubernetes user to bind as team admin (repeatable)")
	initCmd.Flags().StringSliceVar(&initOpts.ServiceAccounts, "service-account", nil, "ServiceAccount to bind as team admin, as name or namespace/name (repeatable)")
	initCmd.Flags().StringVar(&initOpts.RoleName, "role-name", "mcp-runtime-team-admin", "Role name for team-admin permissions")
	initCmd.Flags().StringVar(&initOpts.BindingName, "binding-name", "", "RoleBinding name (defaults to <slug>-mcp-runtime-admins)")
	initCmd.Flags().BoolVar(&initOpts.SkipTraefikWatch, "skip-traefik-watch", false, "Do not create Traefik watch RBAC or patch the bundled Traefik deployment")
	initCmd.Flags().StringVar(&initOpts.TraefikNamespace, "traefik-namespace", "traefik", "Namespace containing the bundled Traefik deployment")
	initCmd.Flags().StringVar(&initOpts.TraefikDeployment, "traefik-deployment", "traefik", "Bundled Traefik deployment name to patch")
	initCmd.Flags().StringVar(&initOpts.TraefikServiceAccount, "traefik-service-account", "traefik", "Bundled Traefik ServiceAccount to bind in the team namespace")
	initCmd.Flags().BoolVar(&initOpts.DryRun, "dry-run", false, "Print the generated namespace/RBAC manifest without applying or patching Traefik")

	cmd.AddCommand(listCmd, createCmd, initCmd)
	return cmd
}
