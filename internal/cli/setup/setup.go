// Package setup owns routing for the setup top-level command.
package setup

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	setupplan "mcp-runtime/internal/cli/setup/plan"
	setupplatform "mcp-runtime/internal/cli/setup/platform"
)

type manager struct {
	logger     *zap.Logger
	clusterMgr setupplatform.ClusterManagerAPI
}

func newManager(runtime *core.Runtime, clusterMgr setupplatform.ClusterManagerAPI) *manager {
	return &manager{logger: runtime.Logger(), clusterMgr: clusterMgr}
}

// New returns the setup command. clusterMgr is the cluster operator that setup
// uses for cluster init and ingress configuration; it is supplied by the
// composition root so setup does not import the cluster command package.
func New(runtime *core.Runtime, clusterMgr setupplatform.ClusterManagerAPI) *cobra.Command {
	var registryType string
	var registryStorageSize string
	var registryMode string
	var externalRegistryURL string
	var externalRegistryUsername string
	var externalRegistryPassword string
	var storageMode string
	var platformMode string
	var kubeconfig string
	var kubeContext string
	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool
	var tlsEnabled bool
	var testMode bool
	var parallelBuilds bool
	var strictProd bool
	var withoutAnalytics bool
	var operatorMetricsAddr string
	var operatorProbeAddr string
	var operatorLeaderElect bool
	var acmeEmail string
	var acmeStaging bool
	var tlsClusterIssuer string
	var skipCertManagerInstall bool
	mgr := newManager(runtime, clusterMgr)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup the complete MCP platform",
		Long: `Setup the complete MCP platform including:
- Kubernetes cluster initialization
- Internal container registry deployment (Docker Registry)
- Operator deployment
- Ingress controller configuration

The platform deploys an internal Docker registry by default, which teams
will use to push and pull container images.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setupplatform.ValidateStorageMode(storageMode); err != nil {
				return err
			}
			registryModeResolved := strings.TrimSpace(registryMode)
			if !cmd.Flags().Changed("registry-mode") {
				if envMode := strings.TrimSpace(os.Getenv("MCP_REGISTRY_MODE")); envMode != "" {
					registryModeResolved = envMode
				}
			}
			if err := setupplatform.ValidateRegistryMode(registryModeResolved); err != nil {
				return err
			}
			platformModeResolved := strings.TrimSpace(platformMode)
			if !cmd.Flags().Changed("platform-mode") {
				if envMode := strings.TrimSpace(os.Getenv("MCP_PLATFORM_MODE")); envMode != "" {
					platformModeResolved = envMode
				}
			}
			if err := setupplatform.ValidatePlatformMode(platformModeResolved); err != nil {
				return err
			}

			operatorArgs := setupplatform.BuildOperatorArgs(
				operatorMetricsAddr,
				operatorProbeAddr,
				operatorLeaderElect,
				cmd.Flags().Changed("operator-leader-elect"),
			)

			acmeEmailResolved := strings.TrimSpace(acmeEmail)
			if acmeEmailResolved == "" {
				acmeEmailResolved = strings.TrimSpace(os.Getenv("MCP_ACME_EMAIL"))
			}
			acmeStagingResolved := acmeStaging
			if v := strings.TrimSpace(os.Getenv("MCP_ACME_STAGING")); v == "1" || strings.EqualFold(v, "true") {
				acmeStagingResolved = true
			}
			tlsCIResolved := strings.TrimSpace(tlsClusterIssuer)
			if tlsCIResolved == "" {
				tlsCIResolved = strings.TrimSpace(os.Getenv("MCP_TLS_CLUSTER_ISSUER"))
			}
			if err := setupplatform.ValidateTLSSetupCLIFlags(tlsEnabled, acmeEmailResolved, tlsCIResolved, acmeStagingResolved, skipCertManagerInstall); err != nil {
				return err
			}
			if err := setupplatform.ValidateRegistryTLSMode(registryModeResolved, tlsEnabled, acmeEmailResolved); err != nil {
				return err
			}

			plan := setupplan.Build(setupplan.Input{
				Kubeconfig:             kubeconfig,
				Context:                kubeContext,
				RegistryType:           registryType,
				RegistryStorageSize:    registryStorageSize,
				RegistryMode:           registryModeResolved,
				ExternalRegistryURL:    externalRegistryURL,
				ExternalRegistryUser:   externalRegistryUsername,
				ExternalRegistryPass:   externalRegistryPassword,
				StorageMode:            storageMode,
				PlatformMode:           platformModeResolved,
				IngressMode:            ingressMode,
				IngressManifest:        ingressManifest,
				IngressManifestChanged: cmd.Flags().Changed("ingress-manifest"),
				ForceIngressInstall:    forceIngressInstall,
				TLSEnabled:             tlsEnabled,
				TestMode:               testMode,
				ParallelBuilds:         parallelBuilds,
				StrictProd:             strictProd,
				DeployAnalytics:        !withoutAnalytics,
				OperatorArgs:           operatorArgs,
				ACMEmail:               acmeEmailResolved,
				ACMEStaging:            acmeStagingResolved,
				TLSClusterIssuer:       tlsCIResolved,
				InstallCertManager:     !skipCertManagerInstall,
			})

			return setupplatform.SetupPlatform(mgr.logger, plan, mgr.clusterMgr)
		},
	}

	cmd.Flags().StringVar(&registryType, "registry-type", "docker", "Registry type (docker; harbor coming soon)")
	cmd.Flags().StringVar(&registryStorageSize, "registry-storage", "20Gi", "Registry storage size (default: 20Gi)")
	cmd.Flags().StringVar(&registryMode, "registry-mode", "auto", "Registry setup mode (auto|bundled-http|bundled-https|external). auto uses a provisioned registry config when present, otherwise the bundled registry")
	cmd.Flags().StringVar(&externalRegistryURL, "external-registry-url", "", "External/provisioned registry URL for --registry-mode external (overrides PROVISIONED_REGISTRY_URL and registry provision config)")
	cmd.Flags().StringVar(&externalRegistryUsername, "external-registry-username", "", "External/provisioned registry username for --registry-mode external")
	cmd.Flags().StringVar(&externalRegistryPassword, "external-registry-password", "", "External/provisioned registry password for --registry-mode external (prefer PROVISIONED_REGISTRY_PASSWORD for shells)")
	cmd.Flags().StringVar(&storageMode, "storage-mode", "dynamic", "Storage mode for local/dev clusters (dynamic|hostpath). Use hostpath for single-node k3s/minikube/kind without a provisioner.")
	cmd.Flags().StringVar(&platformMode, "platform-mode", "tenant", "Platform access model (tenant|org|public). public exposes the catalog without login and lets signed-in users publish to the public preview namespace.")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install automatically during setup (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/http", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force repo-managed ingress install when only an IngressClass exists; refuses active external Traefik")
	cmd.Flags().BoolVar(&tlsEnabled, "with-tls", false, "Enable TLS overlays (ingress/registry). Use --acme-email for public Let's Encrypt, --tls-cluster-issuer for an org ClusterIssuer, or the bundled mcp-runtime-ca private CA (no ACME) when neither is set")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Contact email for Let's Encrypt (HTTP-01 via cert-manager). Mutually exclusive with --tls-cluster-issuer. Overrides env MCP_ACME_EMAIL")
	cmd.Flags().StringVar(&tlsClusterIssuer, "tls-cluster-issuer", "", "Use an existing cert-manager ClusterIssuer (e.g. internal CA; setup does not create it). Mutually exclusive with --acme-email. Overrides env MCP_TLS_CLUSTER_ISSUER")
	cmd.Flags().BoolVar(&acmeStaging, "acme-staging", false, "Use Let's Encrypt staging CA (also set MCP_ACME_STAGING=1)")
	cmd.Flags().BoolVar(&skipCertManagerInstall, "skip-cert-manager-install", false, "Do not install cert-manager; require CRDs to already exist")
	cmd.Flags().BoolVar(&testMode, "test-mode", false, "Test mode for local Kind/dev installs; builds and pushes latest-tag runtime images while relaxing production guardrails")
	cmd.Flags().BoolVar(&parallelBuilds, "parallel-builds", false, "Build and publish setup images in parallel; keeps cluster, registry, TLS, and rollout sequencing unchanged")
	cmd.Flags().BoolVar(&strictProd, "strict-prod", false, "Require production-style registry and TLS validation for non-test setup")
	cmd.Flags().BoolVar(&withoutAnalytics, "without-sentinel", false, "Skip deploying the bundled mcp-sentinel stack")
	cmd.Flags().BoolVar(&withoutAnalytics, "without-analytics", false, "Deprecated alias for --without-sentinel")
	_ = cmd.Flags().MarkDeprecated("without-analytics", "use --without-sentinel")
	_ = cmd.Flags().MarkHidden("without-analytics")
	cmd.Flags().StringVar(&operatorMetricsAddr, "operator-metrics-addr", "", "Operator metrics bind address (default: :8080 from manager.yaml)")
	cmd.Flags().StringVar(&operatorProbeAddr, "operator-probe-addr", "", "Operator health probe bind address (default: :8081 from manager.yaml)")
	cmd.Flags().BoolVar(&operatorLeaderElect, "operator-leader-elect", false, "Override operator leader election when set")

	return cmd
}
