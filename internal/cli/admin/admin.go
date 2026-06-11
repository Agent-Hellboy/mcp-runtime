// Package admin owns operator-only CLI commands that require direct Kubernetes access.
package admin

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry"
)

// New returns the admin command group.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(registry.NewRegistryManager(runtime.KubectlClient(), runtime.Executor(), runtime.Logger()))
}

// NewWithManager returns the admin command group using the provided registry manager.
func NewWithManager(mgr *registry.RegistryManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "admin",
		Hidden: true,
		Short:  "Operator-only commands that require direct Kubernetes access",
		Long:   "Operator-only commands for debugging and cluster administration. These commands require admin/operator kubectl access and are not part of the normal platform-backed user flow.",
	}

	registryCmd := &cobra.Command{
		Use:   "registry",
		Short: "Direct Kubernetes registry operations",
	}

	var image string
	var name string
	var scope string
	var mode string
	var helperNamespace string
	pushCmd := &cobra.Command{
		Use:   "push",
		Short: "Push an image using direct or in-cluster Kubernetes access",
		Long:  "Push a local image using docker push or an in-cluster skopeo helper pod. Requires admin kubectl access. Normal users should use `mcp-runtime registry push` instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return registry.RunAdminRegistryPush(cmd.Context(), mgr, image, "", name, scope, mode, helperNamespace)
		},
	}
	pushCmd.Flags().StringVar(&image, "image", "", "Local image to push (required)")
	pushCmd.Flags().StringVar(&name, "name", "", "Override target repo/name (default: source name without registry)")
	pushCmd.Flags().StringVar(&scope, "scope", "", "Publish scope: tenant, org, or public")
	pushCmd.Flags().StringVar(&mode, "mode", "in-cluster", "Push mode: in-cluster (skopeo helper) or direct (docker push)")
	pushCmd.Flags().StringVar(&helperNamespace, "namespace", core.NamespaceRegistry, "Namespace to run the in-cluster helper pod")

	registryCmd.AddCommand(pushCmd)
	cmd.AddCommand(registryCmd)
	return cmd
}
