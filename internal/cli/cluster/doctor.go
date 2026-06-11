package cluster

import (
	"github.com/spf13/cobra"

	clusterdoctor "mcp-runtime/internal/cli/cluster/doctor"
	"mcp-runtime/internal/cli/core"
)

func newClusterDoctorCmd(mgr *ClusterManager) *cobra.Command {
	var forSetup bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose MCP Runtime cluster readiness and installed components",
		Long: "Detect the Kubernetes distribution and check that the registry service, cluster DNS, " +
			"operator/CRD prerequisites, ingress (Traefik) wiring, image pulls, Sentinel, and MCPServer reconciliation are healthy. Prints remediation steps for your distribution " +
			"when something is missing. Use --for-setup to run pre-setup ingress, public DNS, and TLS prerequisite checks. See docs/cluster-readiness.md for the full per-distribution checklist.",
		RunE: func(cmd *cobra.Command, args []string) error {
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
	return cmd
}
