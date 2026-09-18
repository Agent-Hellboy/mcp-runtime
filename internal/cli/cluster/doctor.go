package cluster

import (
	"fmt"

	"github.com/spf13/cobra"

	clusterdoctor "mcp-runtime/internal/cli/cluster/doctor"
	"mcp-runtime/internal/cli/core"
)

func newClusterDoctorCmd(mgr *ClusterManager) *cobra.Command {
	var forSetup, afterSetup bool
	var kubeconfig string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose MCP Runtime cluster readiness and installed components",
		Long: "Detect the Kubernetes distribution and check that the registry service, cluster DNS, " +
			"operator/CRD prerequisites, ingress (Traefik) wiring, image pulls, Sentinel, and MCPServer reconciliation are healthy. Prints remediation steps for your distribution " +
			"when something is missing. Use --for-setup before setup for cluster prerequisites, or --after-setup after setup for the complete installed-platform validation. With no lifecycle flag, the command keeps the complete post-setup behavior for backward compatibility. See docs/cluster-readiness.md for the full per-distribution checklist.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if kubeconfig != "" {
				if err := mgr.ConfigureKubeconfig(kubeconfig, ""); err != nil {
					return err
				}
			}
			if forSetup && afterSetup {
				return fmt.Errorf("--for-setup and --after-setup cannot be used together")
			}
			if forSetup {
				report := clusterdoctor.RunSetupDoctorAndPrint(mgr.KubectlRunner())
				if !report.AllOK() {
					return core.NewSetupStepFailedError()
				}
				return nil
			}
			report := clusterdoctor.RunDoctorAndPrint(mgr.KubectlRunner())
			if !report.AllOK() {
				return core.NewSetupStepFailedError()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&forSetup, "for-setup", false, "Run pre-setup readiness checks for ingress, public DNS, and TLS prerequisites")
	cmd.Flags().BoolVar(&afterSetup, "after-setup", false, "Run the complete post-setup validation for installed Runtime components")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (auto-detects ~/.kube/config or /etc/rancher/k3s/k3s.yaml)")
	return cmd
}
