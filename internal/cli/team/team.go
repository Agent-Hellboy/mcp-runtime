package team

import (
	"errors"
	"strings"

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

	var userEmail string
	var userUsername string
	var userPassword string
	var userRole string
	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Manage password-login users for a team",
	}
	userCreateCmd := &cobra.Command{
		Use:   "create [team-slug]",
		Short: "Create or update a password-login user and add them to a team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email, err := resolveTeamUserEmail(userEmail, userUsername)
			if err != nil {
				return err
			}
			return mgr.CreateTeamUser(args[0], email, userPassword, userRole)
		},
	}
	userCreateCmd.Flags().StringVar(&userEmail, "email", "", "Platform account email")
	userCreateCmd.Flags().StringVar(&userUsername, "username", "", "Alias for --email")
	userCreateCmd.Flags().StringVar(&userPassword, "password", "", "Platform account password (prefer a private shell or environment-managed invocation)")
	userCreateCmd.Flags().StringVar(&userRole, "role", "member", "Team role for the user: member or owner")

	userListCmd := &cobra.Command{
		Use:   "list [team-slug]",
		Short: "List users in a team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ListTeamUsers(args[0])
		},
	}
	userCmd.AddCommand(userCreateCmd, userListCmd)

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

	cmd.AddCommand(listCmd, createCmd, userCmd, initCmd)
	return cmd
}

func resolveTeamUserEmail(email, username string) (string, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	if email != "" && username != "" && !strings.EqualFold(email, username) {
		return "", errors.New("--email and --username must match when both are set")
	}
	if email != "" {
		return email, nil
	}
	return username, nil
}
