// Package access owns routing for the access top-level command.
package access

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

// New returns the access command.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(DefaultAccessManager(runtime))
}

// NewWithManager returns the access command using the provided manager.
func NewWithManager(mgr *AccessManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Manage grants and agent sessions",
		Long: `Commands for managing MCPAccessGrant and MCPAgentSession resources that feed the gateway policy layer.

With mcp-runtime auth login, commands use the platform API by default for normal
user and admin workflows. Use --use-kube only for admin/dev/test direct
Kubernetes operations; it requires kubectl plus admin/operator kubeconfig and
RBAC access. For platform workflows, run mcp-runtime auth login --api-url
<platform-url> and stay on the platform API path.`,
	}

	mgr.BindUseKubeFlag(cmd)

	cmd.AddCommand(newGrantCmd(mgr), newSessionCmd(mgr))
	return cmd
}

func newGrantCmd(mgr *AccessManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Manage MCPAccessGrant resources",
	}
	cmd.AddCommand(newListCmd(mgr, GrantResource, "grants"))
	cmd.AddCommand(newGetCmd(mgr, GrantResource, "grant"))
	cmd.AddCommand(newApplyCmd(mgr, "grant"))
	cmd.AddCommand(newDeleteCmd(mgr, GrantResource, "grant"))
	cmd.AddCommand(newToggleCmd(mgr, GrantResource, "disable", "Disable a grant", true))
	cmd.AddCommand(newToggleCmd(mgr, GrantResource, "enable", "Enable a grant", false))
	return cmd
}

func newSessionCmd(mgr *AccessManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage MCPAgentSession resources",
	}
	cmd.AddCommand(newListCmd(mgr, SessionResource, "sessions"))
	cmd.AddCommand(newGetCmd(mgr, SessionResource, "session"))
	cmd.AddCommand(newApplyCmd(mgr, "session"))
	cmd.AddCommand(newDeleteCmd(mgr, SessionResource, "session"))
	cmd.AddCommand(newToggleCmd(mgr, SessionResource, "revoke", "Revoke an agent session", true))
	cmd.AddCommand(newToggleCmd(mgr, SessionResource, "unrevoke", "Clear the revoked flag on an agent session", false))
	return cmd
}

func newListCmd(mgr *AccessManager, resource, label string) *cobra.Command {
	var namespace string
	var allNamespaces bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access " + label,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ListAccessResources(resource, namespace, allNamespaces)
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to inspect")
	cmd.Flags().BoolVar(&allNamespaces, "all-namespaces", true, "List resources across all namespaces when no namespace is specified")
	return cmd
}

func newGetCmd(mgr *AccessManager, resource, label string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "get [name]",
		Short: "Get an access " + label,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.GetAccessResource(resource, args[0], namespace)
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", core.NamespaceMCPServers, "Namespace")
	return cmd
}

func newApplyCmd(mgr *AccessManager, label string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a " + label + " manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ApplyAccessResource(file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Manifest file to apply")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newDeleteCmd(mgr *AccessManager, resource, label string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete an access " + label,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.DeleteAccessResource(resource, args[0], namespace)
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", core.NamespaceMCPServers, "Namespace")
	return cmd
}

func newToggleCmd(mgr *AccessManager, resource, use, short string, value bool) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   use + " [name]",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ToggleAccessResource(resource, args[0], namespace, value)
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", core.NamespaceMCPServers, "Namespace")
	return cmd
}
