// Package setup owns routing for the setup top-level command.
package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	setupplan "mcp-runtime/internal/cli/setup/plan"
	setupplatform "mcp-runtime/internal/cli/setup/platform"
)

// loadEnvFile reads KEY=VALUE pairs from path and sets any that are not already
// present in the process environment. Explicit env vars and CLI flags always
// take precedence over values in the file.
func loadEnvFile(path string) error {
	// #nosec G304 -- path is an explicit user-supplied CLI flag value.
	envs, err := godotenv.Read(path)
	if err != nil {
		return err
	}
	for key, val := range envs {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return fmt.Errorf("setting %s: %w", key, err)
			}
		}
	}
	return nil
}

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
	var envFile string
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
			if envFile != "" {
				if err := loadEnvFile(envFile); err != nil {
					return fmt.Errorf("--env-file %s: %w", envFile, err)
				}
				// DefaultCLIConfig is initialized at package load time, before the env
				// file is applied. Reload it now so registry/ingress host resolution
				// picks up MCP_PLATFORM_DOMAIN and related vars from the env file.
				core.DefaultCLIConfig = core.LoadCLIConfig()
			}

			// Apply env var fallbacks for every flag that was not explicitly set on
			// the command line. Primary names follow the MCP_SETUP_* convention used
			// in config/deployments/mcpruntime-org.env; legacy names are kept for
			// backward compatibility where they already existed.
			envStr := func(flag string, val *string, vars ...string) {
				if cmd.Flags().Changed(flag) {
					return
				}
				for _, envVar := range vars {
					if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
						*val = v
						return
					}
				}
			}
			envBool := func(flag string, val *bool, vars ...string) {
				if cmd.Flags().Changed(flag) {
					return
				}
				for _, envVar := range vars {
					v := strings.TrimSpace(os.Getenv(envVar))
					if v == "1" || strings.EqualFold(v, "true") {
						*val = true
						return
					}
				}
			}

			// Cluster access
			envStr("kubeconfig", &kubeconfig, "MCP_SETUP_KUBECONFIG")
			envStr("context", &kubeContext, "MCP_KUBE_CONTEXT")

			// Registry
			envStr("registry-type", &registryType, "MCP_REGISTRY_TYPE")
			envStr("registry-storage", &registryStorageSize, "MCP_REGISTRY_STORAGE_SIZE")
			envStr("registry-mode", &registryMode, "MCP_SETUP_REGISTRY_MODE", "MCP_REGISTRY_MODE")
			envStr("external-registry-url", &externalRegistryURL, "PROVISIONED_REGISTRY_URL")
			envStr("external-registry-username", &externalRegistryUsername, "PROVISIONED_REGISTRY_USERNAME")
			envStr("external-registry-password", &externalRegistryPassword, "PROVISIONED_REGISTRY_PASSWORD")

			// Storage / platform
			envStr("storage-mode", &storageMode, "MCP_STORAGE_MODE")
			envStr("platform-mode", &platformMode, "MCP_SETUP_PLATFORM_MODE", "MCP_PLATFORM_MODE")

			// Ingress
			envStr("ingress", &ingressMode, "MCP_SETUP_INGRESS")
			envStr("ingress-manifest", &ingressManifest, "MCP_SETUP_INGRESS_MANIFEST")
			envBool("force-ingress-install", &forceIngressInstall, "MCP_FORCE_INGRESS_INSTALL")

			// TLS
			envBool("with-tls", &tlsEnabled, "MCP_SETUP_WITH_TLS")
			envStr("acme-email", &acmeEmail, "MCP_ACME_EMAIL")
			envBool("acme-staging", &acmeStaging, "MCP_ACME_STAGING")
			envStr("tls-cluster-issuer", &tlsClusterIssuer, "MCP_SETUP_TLS_CLUSTER_ISSUER", "MCP_TLS_CLUSTER_ISSUER")
			envBool("skip-cert-manager-install", &skipCertManagerInstall, "MCP_SETUP_SKIP_CERT_MANAGER_INSTALL")

			// Deployment behaviour
			envBool("test-mode", &testMode, "MCP_SETUP_TEST_MODE")
			envBool("parallel-builds", &parallelBuilds, "MCP_PARALLEL_BUILDS")
			envBool("strict-prod", &strictProd, "MCP_STRICT_PROD")
			envBool("without-sentinel", &withoutAnalytics, "MCP_WITHOUT_SENTINEL")

			// Validate after all env vars are applied.
			if err := setupplatform.ValidateStorageMode(storageMode); err != nil {
				return err
			}
			if err := setupplatform.ValidateRegistryMode(registryMode); err != nil {
				return err
			}
			if err := setupplatform.ValidatePlatformMode(platformMode); err != nil {
				return err
			}

			operatorArgs := setupplatform.BuildOperatorArgs(
				operatorMetricsAddr,
				operatorProbeAddr,
				operatorLeaderElect,
				cmd.Flags().Changed("operator-leader-elect"),
			)

			if err := setupplatform.ValidateTLSSetupCLIFlags(tlsEnabled, acmeEmail, tlsClusterIssuer, acmeStaging, skipCertManagerInstall); err != nil {
				return err
			}
			if err := setupplatform.ValidateRegistryTLSMode(registryMode, tlsEnabled, acmeEmail); err != nil {
				return err
			}

			plan := setupplan.Build(setupplan.Input{
				Kubeconfig:             kubeconfig,
				Context:                kubeContext,
				RegistryType:           registryType,
				RegistryStorageSize:    registryStorageSize,
				RegistryMode:           registryMode,
				ExternalRegistryURL:    externalRegistryURL,
				ExternalRegistryUser:   externalRegistryUsername,
				ExternalRegistryPass:   externalRegistryPassword,
				StorageMode:            storageMode,
				PlatformMode:           platformMode,
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
				ACMEmail:               acmeEmail,
				ACMEStaging:            acmeStaging,
				TLSClusterIssuer:       tlsClusterIssuer,
				InstallCertManager:     !skipCertManagerInstall,
			})

			return setupplatform.SetupPlatform(mgr.logger, plan, mgr.clusterMgr)
		},
	}

	cmd.Flags().StringVar(&envFile, "env-file", "", "Path to an env file to source before setup (e.g. config/deployments/mcpruntime-org.env); variables already in the environment are not overridden")
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
	cmd.Flags().StringVar(&tlsClusterIssuer, "tls-cluster-issuer", "", "Use an existing cert-manager ClusterIssuer (e.g. internal CA; setup does not create it). Mutually exclusive with --acme-email. Overrides env MCP_SETUP_TLS_CLUSTER_ISSUER")
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
