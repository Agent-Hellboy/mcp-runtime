package cluster

import (
	"time"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/certmanager"
	"mcp-runtime/internal/cli/core"
)

func newClusterCertCmd(mgr *ClusterManager) *cobra.Command {
	certMgr := certmanager.NewCertManager(mgr.KubectlRunner(), mgr.Logger())
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage cert-manager resources",
		Long:  "Manage cert-manager resources required for TLS in the MCP platform",
	}

	cmd.AddCommand(certMgrStatusCmd(certMgr))
	cmd.AddCommand(certMgrApplyCmd(certMgr))
	cmd.AddCommand(certMgrWaitCmd(certMgr))

	return cmd
}

func certMgrStatusCmd(mgr *certmanager.CertManager) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check cert-manager resources",
		Long:  "Check cert-manager installation, CA secret, issuer, and registry certificate",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.Status()
		},
	}
}

func certMgrApplyCmd(mgr *certmanager.CertManager) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply cert-manager resources",
		Long:  "Apply ClusterIssuer and registry Certificate manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.Apply(dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run preflight checks and print the resources that would be applied without modifying the cluster")
	return cmd
}

func certMgrWaitCmd(mgr *certmanager.CertManager) *cobra.Command {
	var timeoutDuration time.Duration
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for registry certificate readiness",
		Long:  "Wait for the registry certificate to reach Ready state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeoutDuration == 0 {
				timeoutDuration = core.GetCertTimeout()
			}
			return mgr.Wait(timeoutDuration)
		},
	}

	cmd.Flags().DurationVar(&timeoutDuration, "timeout", 0, "Timeout for certificate readiness (default from MCP_CERT_TIMEOUT)")
	return cmd
}
