// Package registry owns routing for the registry top-level command.
package registry

import (
	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

// New returns the registry command.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(NewRegistryManager(runtime.KubectlClient(), runtime.Executor(), runtime.Logger()))
}

// NewWithManager returns the registry command using the provided manager.
func NewWithManager(mgr *RegistryManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage container registry",
		Long:  "Commands for managing the container registry",
	}

	var namespace string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check registry status",
		Long:  "Check the status of the container registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.CheckRegistryStatus(namespace)
		},
	}
	statusCmd.Flags().StringVar(&namespace, "namespace", core.NamespaceRegistry, "Registry namespace")

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show registry information",
		Long:  "Show registry URL and connection information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ShowRegistryInfo()
		},
	}

	var url string
	var username string
	var password string
	var operatorImage string
	var provisionDryRun bool
	provisionCmd := &cobra.Command{
		Use:   "provision",
		Short: "Configure an external registry",
		Long:  "Configure an external registry to be used for operator/runtime images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRegistryProvision(mgr, url, username, password, operatorImage, provisionDryRun)
		},
	}
	provisionCmd.Flags().StringVar(&url, "url", "", "External registry URL (e.g., registry.mcpruntime.com)")
	provisionCmd.Flags().StringVar(&username, "username", "", "Registry username (optional)")
	provisionCmd.Flags().StringVar(&password, "password", "", "Registry password (optional)")
	provisionCmd.Flags().StringVar(&operatorImage, "operator-image", "", "Optional: build and push operator image to this external registry (e.g., <registry>/mcp-runtime-operator:latest)")
	provisionCmd.Flags().BoolVar(&provisionDryRun, "dry-run", false, "Print what would be done without saving config, logging in, or pushing images")

	var image string
	var registryURL string
	var name string
	var scope string
	var mode string
	var helperNamespace string
	pushCmd := &cobra.Command{
		Use:   "push",
		Short: "Retag and push an image to the platform or provisioned registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRegistryPush(mgr, image, registryURL, name, scope, mode, helperNamespace)
		},
	}
	pushCmd.Flags().StringVar(&image, "image", "", "Local image to push (required)")
	pushCmd.Flags().StringVar(&registryURL, "registry", "", "Target registry (defaults to provisioned or internal)")
	pushCmd.Flags().StringVar(&name, "name", "", "Override target repo/name (default: source name without registry)")
	pushCmd.Flags().StringVar(&scope, "scope", "", "Publish scope: tenant, org, or public")
	pushCmd.Flags().StringVar(&mode, "mode", "in-cluster", "Push mode: in-cluster (default, uses skopeo helper) or direct (docker push)")
	pushCmd.Flags().StringVar(&helperNamespace, "namespace", core.NamespaceRegistry, "Namespace to run the in-cluster helper pod")

	cmd.AddCommand(statusCmd, infoCmd, provisionCmd, pushCmd)
	return cmd
}
