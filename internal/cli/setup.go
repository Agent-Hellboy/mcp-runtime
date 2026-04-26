package cli

// This file implements the "setup" command for installing and configuring the MCP platform.
// It handles cluster initialization, registry deployment, operator installation, and TLS setup.
// The setup process is organized as a series of steps with dependency injection for testability.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"mcp-runtime/pkg/manifest"
)

const defaultRegistrySecretName = "mcp-runtime-registry-creds" // #nosec G101 -- default secret name, not a credential.
const testModeOperatorImage = "docker.io/library/mcp-runtime-operator:latest"
const defaultGatewayProxyRepository = "mcp-sentinel-mcp-proxy"
const defaultAnalyticsNamespace = "mcp-sentinel"
const defaultAnalyticsIngestURL = "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
const gatewayProxyDockerfilePath = "services/mcp-proxy/Dockerfile"
const gatewayProxyBuildContext = "."

var setupImageTagResolver = getGitTag

type analyticsComponent struct {
	Name         string
	Repository   string
	Dockerfile   string
	BuildContext string
}

type AnalyticsImageSet struct {
	Ingest        string
	API           string
	Processor     string
	UI            string
	Traefik       string
	ClickHouse    string
	Zookeeper     string
	Kafka         string
	Prometheus    string
	OTelCollector string
	Tempo         string
	Loki          string
	Promtail      string
	Grafana       string
}

var analyticsComponents = []analyticsComponent{
	{
		Name:         "ingest",
		Repository:   "mcp-sentinel-ingest",
		Dockerfile:   "services/ingest/Dockerfile",
		BuildContext: "services/ingest",
	},
	{
		Name:         "api",
		Repository:   "mcp-sentinel-api",
		Dockerfile:   "services/api/Dockerfile",
		BuildContext: ".",
	},
	{
		Name:         "processor",
		Repository:   "mcp-sentinel-processor",
		Dockerfile:   "services/processor/Dockerfile",
		BuildContext: "services/processor",
	},
	{
		Name:         "ui",
		Repository:   "mcp-sentinel-ui",
		Dockerfile:   "services/ui/Dockerfile",
		BuildContext: "services/ui",
	},
}

type ClusterManagerAPI interface {
	InitCluster(kubeconfig, context string) error
	ConfigureCluster(opts ingressOptions) error
}

type RegistryManagerAPI interface {
	ShowRegistryInfo() error
	PushInCluster(source, target, helperNS string) error
}

type SetupDeps struct {
	ResolveExternalRegistryConfig   func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error)
	ClusterManager                  ClusterManagerAPI
	RegistryManager                 RegistryManagerAPI
	LoginRegistry                   func(logger *zap.Logger, registryURL, username, password string) error
	DeployRegistry                  func(logger *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error
	WaitForDeploymentAvailable      func(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error
	PrintDeploymentDiagnostics      func(deploy, namespace, selector string)
	SetupTLS                        func(logger *zap.Logger, plan SetupPlan) error
	BuildOperatorImage              func(image string) error
	PushOperatorImage               func(image string) error
	BuildGatewayProxyImage          func(image string) error
	PushGatewayProxyImage           func(image string) error
	BuildAnalyticsImage             func(image, dockerfilePath, buildContext string) error
	PushAnalyticsImage              func(image string) error
	EnsureNamespace                 func(namespace string) error
	GetPlatformRegistryURL          func(logger *zap.Logger) string
	PushOperatorImageToInternal     func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	PushGatewayProxyImageToInternal func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	PushAnalyticsImageToInternal    func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	DeployOperatorManifests         func(logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error
	DeployAnalyticsManifests        func(logger *zap.Logger, images AnalyticsImageSet, storageMode string) error
	ConfigureProvisionedRegistryEnv func(ext *ExternalRegistryConfig, secretName string) error
	RestartDeployment               func(name, namespace string) error
	CheckCRDInstalled               func(name string) error
	GetDeploymentTimeout            func() time.Duration
	GetRegistryPort                 func() int
	OperatorImageFor                func(ext *ExternalRegistryConfig) string
	GatewayProxyImageFor            func(ext *ExternalRegistryConfig) string
}

func (d SetupDeps) withDefaults(logger *zap.Logger) SetupDeps {
	if d.ResolveExternalRegistryConfig == nil {
		d.ResolveExternalRegistryConfig = resolveExternalRegistryConfig
	}
	if d.ClusterManager == nil {
		d.ClusterManager = DefaultClusterManager(logger)
	}
	if d.RegistryManager == nil {
		d.RegistryManager = DefaultRegistryManager(logger)
	}
	if d.LoginRegistry == nil {
		d.LoginRegistry = loginRegistry
	}
	if d.DeployRegistry == nil {
		d.DeployRegistry = deployRegistry
	}
	if d.WaitForDeploymentAvailable == nil {
		d.WaitForDeploymentAvailable = waitForDeploymentAvailable
	}
	if d.PrintDeploymentDiagnostics == nil {
		d.PrintDeploymentDiagnostics = printDeploymentDiagnostics
	}
	if d.SetupTLS == nil {
		d.SetupTLS = func(l *zap.Logger, p SetupPlan) error { return setupTLSWithKubectlAndPlan(kubectlClient, l, p) }
	}
	if d.BuildOperatorImage == nil {
		d.BuildOperatorImage = buildOperatorImage
	}
	if d.PushOperatorImage == nil {
		d.PushOperatorImage = pushOperatorImage
	}
	if d.BuildGatewayProxyImage == nil {
		d.BuildGatewayProxyImage = buildGatewayProxyImage
	}
	if d.PushGatewayProxyImage == nil {
		d.PushGatewayProxyImage = pushGatewayProxyImage
	}
	if d.BuildAnalyticsImage == nil {
		d.BuildAnalyticsImage = buildAnalyticsImage
	}
	if d.PushAnalyticsImage == nil {
		d.PushAnalyticsImage = pushAnalyticsImage
	}
	if d.EnsureNamespace == nil {
		d.EnsureNamespace = ensureNamespace
	}
	if d.GetPlatformRegistryURL == nil {
		d.GetPlatformRegistryURL = getPlatformRegistryURL
	}
	if d.PushOperatorImageToInternal == nil {
		d.PushOperatorImageToInternal = pushOperatorImageToInternalRegistry
	}
	if d.PushGatewayProxyImageToInternal == nil {
		d.PushGatewayProxyImageToInternal = pushGatewayProxyImageToInternalRegistry
	}
	if d.PushAnalyticsImageToInternal == nil {
		d.PushAnalyticsImageToInternal = pushAnalyticsImageToInternalRegistry
	}
	if d.DeployOperatorManifests == nil {
		d.DeployOperatorManifests = deployOperatorManifests
	}
	if d.DeployAnalyticsManifests == nil {
		d.DeployAnalyticsManifests = deployAnalyticsManifests
	}
	if d.ConfigureProvisionedRegistryEnv == nil {
		d.ConfigureProvisionedRegistryEnv = configureProvisionedRegistryEnv
	}
	if d.RestartDeployment == nil {
		d.RestartDeployment = restartDeployment
	}
	if d.CheckCRDInstalled == nil {
		d.CheckCRDInstalled = checkCRDInstalled
	}
	if d.GetDeploymentTimeout == nil {
		d.GetDeploymentTimeout = GetDeploymentTimeout
	}
	if d.GetRegistryPort == nil {
		d.GetRegistryPort = GetRegistryPort
	}
	if d.OperatorImageFor == nil {
		d.OperatorImageFor = getOperatorImage
	}
	if d.GatewayProxyImageFor == nil {
		d.GatewayProxyImageFor = getGatewayProxyImage
	}
	return d
}

// validateTLSSetupCLIFlags enforces ACME / internal-issuer mutual exclusion and
// requires --with-tls when any TLS or cert-manager-related options are set.
func validateTLSSetupCLIFlags(
	tlsEnabled bool,
	acmeEmailResolved, tlsCIResolved string,
	acmeStagingResolved, skipCertManagerInstall bool,
) error {
	if acmeEmailResolved != "" && tlsCIResolved != "" {
		return newWithSentinel(ErrFieldRequired, "use either --acme-email (or MCP_ACME_EMAIL) for public Let's Encrypt, or --tls-cluster-issuer (or MCP_TLS_CLUSTER_ISSUER) for an existing internal ClusterIssuer, not both")
	}
	if !tlsEnabled && (tlsCIResolved != "" || acmeEmailResolved != "" || acmeStagingResolved || skipCertManagerInstall) {
		return newWithSentinel(ErrFieldRequired, "--with-tls is required when using --acme-email, --tls-cluster-issuer, --acme-staging, --skip-cert-manager-install, or related environment variables (MCP_ACME_EMAIL, MCP_ACME_STAGING, MCP_TLS_CLUSTER_ISSUER)")
	}
	return nil
}

// NewSetupCmd constructs the top-level setup command for installing the platform.
func NewSetupCmd(logger *zap.Logger) *cobra.Command {
	var registryType string
	var registryStorageSize string
	var storageMode string
	var kubeconfig string
	var kubeContext string
	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool
	var tlsEnabled bool
	var testMode bool
	var strictProd bool
	var withoutAnalytics bool
	var operatorMetricsAddr string
	var operatorProbeAddr string
	var operatorLeaderElect bool
	var acmeEmail string
	var acmeStaging bool
	var tlsClusterIssuer string
	var skipCertManagerInstall bool
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
			if err := validateStorageMode(storageMode); err != nil {
				logStructuredError(logger, err, "Invalid --storage-mode")
				return err
			}

			// Build operator args from flags
			operatorArgs := buildOperatorArgs(
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
			if err := validateTLSSetupCLIFlags(tlsEnabled, acmeEmailResolved, tlsCIResolved, acmeStagingResolved, skipCertManagerInstall); err != nil {
				return err
			}

			plan := BuildSetupPlan(SetupPlanInput{
				Kubeconfig:             kubeconfig,
				Context:                kubeContext,
				RegistryType:           registryType,
				RegistryStorageSize:    registryStorageSize,
				StorageMode:            storageMode,
				IngressMode:            ingressMode,
				IngressManifest:        ingressManifest,
				IngressManifestChanged: cmd.Flags().Changed("ingress-manifest"),
				ForceIngressInstall:    forceIngressInstall,
				TLSEnabled:             tlsEnabled,
				TestMode:               testMode,
				StrictProd:             strictProd,
				DeployAnalytics:        !withoutAnalytics,
				OperatorArgs:           operatorArgs,
				ACMEmail:               acmeEmailResolved,
				ACMEStaging:            acmeStagingResolved,
				TLSClusterIssuer:       tlsCIResolved,
				InstallCertManager:     !skipCertManagerInstall,
			})

			return setupPlatform(logger, plan)
		},
	}

	cmd.Flags().StringVar(&registryType, "registry-type", "docker", "Registry type (docker; harbor coming soon)")
	cmd.Flags().StringVar(&registryStorageSize, "registry-storage", "20Gi", "Registry storage size (default: 20Gi)")
	cmd.Flags().StringVar(&storageMode, "storage-mode", "dynamic", "Storage mode for local/dev clusters (dynamic|hostpath). Use hostpath for single-node k3s/minikube/kind without a provisioner.")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install automatically during setup (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/http", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force ingress install even if an ingress class already exists")
	cmd.Flags().BoolVar(&tlsEnabled, "with-tls", false, "Enable TLS overlays (ingress/registry). Use --acme-email for public Let's Encrypt, --tls-cluster-issuer for an org ClusterIssuer, or the bundled mcp-runtime-ca private CA (no ACME) when neither is set")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Contact email for Let's Encrypt (HTTP-01 via cert-manager). Mutually exclusive with --tls-cluster-issuer. Overrides env MCP_ACME_EMAIL")
	cmd.Flags().StringVar(&tlsClusterIssuer, "tls-cluster-issuer", "", "Use an existing cert-manager ClusterIssuer (e.g. internal CA; setup does not create it). Mutually exclusive with --acme-email. Overrides env MCP_TLS_CLUSTER_ISSUER")
	cmd.Flags().BoolVar(&acmeStaging, "acme-staging", false, "Use Let's Encrypt staging CA (also set MCP_ACME_STAGING=1)")
	cmd.Flags().BoolVar(&skipCertManagerInstall, "skip-cert-manager-install", false, "Do not install cert-manager; require CRDs to already exist")
	cmd.Flags().BoolVar(&testMode, "test-mode", false, "Test mode: skip operator/gateway image builds and use kind-loaded images")
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

// buildOperatorArgs constructs operator command-line arguments from flags.
// Only includes flags that were explicitly set.
func buildOperatorArgs(metricsAddr, probeAddr string, leaderElect, leaderElectChanged bool) []string {
	var args []string

	if metricsAddr != "" {
		args = append(args, "--metrics-bind-address="+metricsAddr)
	}
	if probeAddr != "" {
		args = append(args, "--health-probe-bind-address="+probeAddr)
	}
	if leaderElectChanged {
		args = append(args, fmt.Sprintf("--leader-elect=%t", leaderElect))
	}

	return args
}

func validateStorageMode(mode string) error {
	switch mode {
	case StorageModeDynamic, StorageModeHostpath:
		return nil
	default:
		return wrapWithSentinel(ErrFieldRequired, fmt.Errorf("invalid storage mode %q", mode), "invalid --storage-mode; expected dynamic or hostpath")
	}
}

func setupPlatform(logger *zap.Logger, plan SetupPlan) error {
	return setupPlatformWithDeps(logger, plan, SetupDeps{}.withDefaults(logger))
}

func setupPlatformWithDeps(logger *zap.Logger, plan SetupPlan, deps SetupDeps) error {
	deps = deps.withDefaults(logger)
	Section("MCP Runtime Setup")

	// Propagate test mode to build helpers so they can choose faster/safer build paths.
	if plan.TestMode {
		_ = os.Setenv("MCP_RUNTIME_TEST_MODE", "1")
	} else {
		_ = os.Unsetenv("MCP_RUNTIME_TEST_MODE")
	}

	extRegistry, usingExternalRegistry, registrySecretName := resolveRegistrySetup(logger, deps)
	if err := validateNonTestSetup(plan, extRegistry, usingExternalRegistry); err != nil {
		logStructuredError(logger, err, "Invalid non-test setup configuration")
		return err
	}
	applySetupPlanToCLIConfig(plan)
	for _, warning := range setupWarnings(plan, extRegistry, usingExternalRegistry) {
		Warn(warning)
	}
	ctx := &SetupContext{
		Plan:                  plan,
		ExternalRegistry:      extRegistry,
		UsingExternalRegistry: usingExternalRegistry,
		RegistrySecretName:    registrySecretName,
	}
	if err := runSetupSteps(logger, deps, ctx, buildSetupSteps(ctx)); err != nil {
		return err
	}

	Success("Platform setup complete")
	fmt.Println(Green("\nPlatform is ready. Use 'mcp-runtime status' to check everything."))
	return nil
}

func resolveRegistrySetup(logger *zap.Logger, deps SetupDeps) (*ExternalRegistryConfig, bool, string) {
	extRegistry, err := deps.ResolveExternalRegistryConfig(nil)
	if err != nil {
		Warn(fmt.Sprintf("Could not load external registry config: %v", err))
	}
	usingExternalRegistry := extRegistry != nil
	return extRegistry, usingExternalRegistry, defaultRegistrySecretName
}

func validateNonTestSetup(plan SetupPlan, extRegistry *ExternalRegistryConfig, usingExternalRegistry bool) error {
	if plan.TestMode {
		return nil
	}
	if !plan.StrictProd {
		return nil
	}
	if !plan.TLSEnabled {
		return newWithSentinel(
			ErrSetupStepFailed,
			"strict production setup requires --with-tls; use normal setup for local HTTP/internal registry flows",
		)
	}
	if usingExternalRegistry && extRegistry != nil && strings.TrimSpace(extRegistry.URL) != "" {
		if isDevRegistryURL(extRegistry.URL) {
			return newWithSentinel(
				ErrSetupStepFailed,
				fmt.Sprintf("strict production setup requires a stable production registry, got dev-only registry URL %q", extRegistry.URL),
			)
		}
		return nil
	}
	if isDevRegistryURL(GetRegistryEndpoint()) {
		return newWithSentinel(
			ErrSetupStepFailed,
			fmt.Sprintf("strict production setup requires a stable internal registry endpoint; set MCP_REGISTRY_ENDPOINT (current %q)", GetRegistryEndpoint()),
		)
	}
	return nil
}

func setupWarnings(plan SetupPlan, extRegistry *ExternalRegistryConfig, usingExternalRegistry bool) []string {
	if plan.TestMode {
		return nil
	}

	var warnings []string
	if !plan.TLSEnabled {
		warnings = append(warnings, "Non-test setup is running without TLS. This is fine for local/internal registries but not recommended for production.")
	}

	if usingExternalRegistry && extRegistry != nil && strings.TrimSpace(extRegistry.URL) != "" {
		registryURL := strings.TrimSpace(extRegistry.URL)
		if strings.HasPrefix(strings.ToLower(registryURL), "http://") {
			warnings = append(warnings, fmt.Sprintf("External registry %q is using HTTP. This is acceptable for local environments but not recommended for production.", registryURL))
		}
		if isDevRegistryURL(registryURL) {
			warnings = append(warnings, fmt.Sprintf("External registry %q looks local/internal. Normal setup allows this, but use --strict-prod to enforce production-style validation.", registryURL))
		}
		return warnings
	}

	registryEndpoint := strings.TrimSpace(GetRegistryEndpoint())
	if registryEndpoint == "" {
		warnings = append(warnings, "Internal registry host is empty; setup will fall back to service DNS. This is fine for local clusters but not recommended for production.")
		return warnings
	}
	if isDevRegistryURL(registryEndpoint) {
		warnings = append(warnings, fmt.Sprintf("Internal registry endpoint %q looks local/internal. Normal setup allows this for local clusters, but use --strict-prod to enforce production-style validation.", registryEndpoint))
	}
	return warnings
}

func isDevRegistryURL(raw string) bool {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") {
		return true
	}

	host := trimmed
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
	}
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if idx := strings.LastIndex(host, ":"); idx >= 0 && strings.Count(host, ":") == 1 {
		host = host[:idx]
	}

	host = strings.ToLower(strings.Trim(host, "[]"))
	switch host {
	case "", "localhost", "registry.local":
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".svc.cluster.local") {
		return true
	}
	return net.ParseIP(host) != nil
}

func setupImageTag() string {
	if os.Getenv("MCP_RUNTIME_TEST_MODE") == "1" {
		return "latest"
	}
	return setupImageTagResolver()
}

func setupClusterSteps(logger *zap.Logger, kubeconfig, context string, ingressOpts ingressOptions, deps SetupDeps) error {
	// Step 1: Initialize cluster
	Step("Step 1: Initialize cluster")
	Info("Installing CRD")
	if err := deps.ClusterManager.InitCluster(kubeconfig, context); err != nil {
		wrappedErr := wrapWithSentinel(ErrClusterInitFailed, err, fmt.Sprintf("failed to initialize cluster: %v", err))
		Error("Cluster initialization failed")
		logStructuredError(logger, wrappedErr, "Cluster initialization failed")
		return wrappedErr
	}
	Info("Cluster initialized")

	// Step 2: Configure cluster
	Step("Step 2: Configure cluster")
	Info("Checking ingress controller")
	if err := deps.ClusterManager.ConfigureCluster(ingressOpts); err != nil {
		wrappedErr := wrapWithSentinel(ErrClusterConfigFailed, err, fmt.Sprintf("cluster configuration failed: %v", err))
		Error("Cluster configuration failed")
		logStructuredError(logger, wrappedErr, "Cluster configuration failed")
		return wrappedErr
	}
	Info("Cluster configuration complete")
	return nil
}

func setupTLSStep(logger *zap.Logger, plan SetupPlan, deps SetupDeps) error {
	// Step 3: Configure TLS (if enabled)
	Step("Step 3: Configure TLS")
	if !plan.TLSEnabled {
		Info("Skipped (TLS disabled, use --with-tls to enable)")
		return nil
	}
	if err := deps.SetupTLS(logger, plan); err != nil {
		wrappedErr := wrapWithSentinel(ErrTLSSetupFailed, err, fmt.Sprintf("TLS setup failed: %v", err))
		Error("TLS setup failed")
		logStructuredError(logger, wrappedErr, "TLS setup failed")
		return wrappedErr
	}
	Success("TLS configured successfully")
	return nil
}

func setupRegistryStep(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry bool, registryType, registryStorageSize, registryManifest string, tlsEnabled bool, deps SetupDeps) error {
	// Step 4: Deploy internal container registry
	Step("Step 4: Configure registry")
	if usingExternalRegistry {
		Info(fmt.Sprintf("Using external registry: %s", extRegistry.URL))
		if extRegistry.Username != "" || extRegistry.Password != "" {
			Info("Logging into external registry")
			if err := deps.LoginRegistry(logger, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
				wrappedErr := wrapWithSentinel(ErrRegistryLoginFailed, err, fmt.Sprintf("failed to login to registry %q: %v", extRegistry.URL, err))
				Error("Registry login failed")
				logStructuredError(logger, wrappedErr, "Registry login failed")
				return wrappedErr
			}
		}
		return nil
	}

	Info(fmt.Sprintf("Type: %s", registryType))
	if tlsEnabled {
		Info("TLS: enabled (registry overlay)")
	} else {
		Info("TLS: disabled (dev HTTP mode)")
	}
	if err := deps.DeployRegistry(logger, "registry", deps.GetRegistryPort(), registryType, registryStorageSize, registryManifest); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrDeployRegistryFailed,
			err,
			fmt.Sprintf("failed to deploy registry (type: %s, manifest: %s): %v", registryType, registryManifest, err),
			map[string]any{
				"namespace":     "registry",
				"registry_type": registryType,
				"manifest_path": registryManifest,
				"storage_size":  registryStorageSize,
				"registry_port": deps.GetRegistryPort(),
			},
		)
		Error("Registry deployment failed")
		logStructuredError(logger, wrappedErr, "Registry deployment failed")
		return wrappedErr
	}

	Info("Waiting for registry to be ready...")
	if err := deps.WaitForDeploymentAvailable(logger, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
		regCtx := map[string]any{
			"deployment": "registry",
			"namespace":  "registry",
			"selector":   "app=registry",
			"component":  "registry",
		}
		mergeDeploymentDebugDiagnosticsIfNeeded(kubectlClient, regCtx, "registry", "registry", "app=registry")
		wrappedErr := wrapWithSentinelAndContext(
			ErrRegistryNotReady,
			err,
			fmt.Sprintf("registry deployment not ready in namespace %q: %v", "registry", err),
			regCtx,
		)
		Error("Registry failed to become ready")
		logStructuredError(logger, wrappedErr, "Registry failed to become ready")
		return wrappedErr
	}

	if err := deps.RegistryManager.ShowRegistryInfo(); err != nil {
		Warn(fmt.Sprintf("Failed to show registry info: %v", err))
	}
	return nil
}

func prepareDeploymentImages(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, string, error) {
	Step("Step 5: Publish runtime images")

	operatorImage, err := prepareOperatorImage(logger, extRegistry, usingExternalRegistry, testMode, deps)
	if err != nil {
		return "", "", err
	}
	gatewayProxyImage, err := prepareGatewayProxyImage(logger, extRegistry, usingExternalRegistry, testMode, deps)
	if err != nil {
		return "", "", err
	}
	return operatorImage, gatewayProxyImage, nil
}

func prepareOperatorImage(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, error) {
	operatorImage := deps.OperatorImageFor(extRegistry)
	Info(fmt.Sprintf("Operator image: %s", operatorImage))

	Info("Building operator image")
	if err := deps.BuildOperatorImage(operatorImage); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrOperatorImageBuildFailed,
			err,
			fmt.Sprintf("operator image build failed for image %q: %v", operatorImage, err),
			map[string]any{
				"image":     operatorImage,
				"component": "operator",
			},
		)
		Error("Operator image build failed")
		logStructuredError(logger, wrappedErr, "Operator image build failed")
		return "", wrappedErr
	}

	if usingExternalRegistry {
		if testMode {
			Info("Test mode: pushing operator image to external registry")
		} else {
			Info("Pushing operator image to external registry")
		}
		if err := deps.PushOperatorImage(operatorImage); err != nil {
			Warn(fmt.Sprintf("Could not push image to external registry: %v", err))
		}
		return operatorImage, nil
	}

	Info("Pushing operator image to internal registry")
	internalRegistryURL := deps.GetPlatformRegistryURL(logger)
	_, operatorTag := splitImage(operatorImage)
	if operatorTag == "" {
		operatorTag = setupImageTag()
	}
	internalOperatorImage := fmt.Sprintf("%s/mcp-runtime-operator:%s", internalRegistryURL, operatorTag)

	if err := deps.EnsureNamespace("registry"); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrEnsureRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure registry namespace: %v", err),
			map[string]any{"namespace": "registry", "component": "setup"},
		)
		Error("Failed to ensure registry namespace")
		logStructuredError(logger, wrappedErr, "Failed to ensure registry namespace")
		return "", wrappedErr
	}

	if err := deps.PushOperatorImageToInternal(logger, operatorImage, internalOperatorImage, "registry"); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrPushOperatorImageInternalFailed,
			err,
			fmt.Sprintf("failed to push operator image %q to internal registry %q: %v", operatorImage, internalOperatorImage, err),
			map[string]any{
				"source_image": operatorImage,
				"target_image": internalOperatorImage,
				"namespace":    "registry",
				"component":    "operator",
			},
		)
		Error("Failed to push operator image to internal registry")
		logStructuredError(logger, wrappedErr, "Failed to push operator image to internal registry")
		return "", wrappedErr
	}
	Info(fmt.Sprintf("Using internal registry image: %s", internalOperatorImage))
	return internalOperatorImage, nil
}

func prepareGatewayProxyImage(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, error) {
	gatewayProxyImage := deps.GatewayProxyImageFor(extRegistry)
	Info(fmt.Sprintf("Gateway proxy image: %s", gatewayProxyImage))

	Info("Building gateway proxy image")
	if err := deps.BuildGatewayProxyImage(gatewayProxyImage); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrGatewayProxyImageBuildFailed,
			err,
			fmt.Sprintf("gateway proxy image build failed for image %q: %v", gatewayProxyImage, err),
			map[string]any{
				"image":     gatewayProxyImage,
				"component": "gateway-proxy",
			},
		)
		Error("Gateway proxy image build failed")
		logStructuredError(logger, wrappedErr, "Gateway proxy image build failed")
		return "", wrappedErr
	}

	if usingExternalRegistry {
		if testMode {
			Info("Test mode: pushing gateway proxy image to external registry")
		} else {
			Info("Pushing gateway proxy image to external registry")
		}
		if err := deps.PushGatewayProxyImage(gatewayProxyImage); err != nil {
			Warn(fmt.Sprintf("Could not push gateway proxy image to external registry: %v", err))
		}
		return gatewayProxyImage, nil
	}

	Info("Pushing gateway proxy image to internal registry")
	internalRegistryURL := deps.GetPlatformRegistryURL(logger)
	_, gatewayTag := splitImage(gatewayProxyImage)
	if gatewayTag == "" {
		gatewayTag = setupImageTag()
	}
	internalGatewayProxyImage := fmt.Sprintf("%s/%s:%s", internalRegistryURL, defaultGatewayProxyRepository, gatewayTag)

	if err := deps.EnsureNamespace("registry"); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrEnsureRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure registry namespace: %v", err),
			map[string]any{"namespace": "registry", "component": "setup"},
		)
		Error("Failed to ensure registry namespace")
		logStructuredError(logger, wrappedErr, "Failed to ensure registry namespace")
		return "", wrappedErr
	}

	if err := deps.PushGatewayProxyImageToInternal(logger, gatewayProxyImage, internalGatewayProxyImage, "registry"); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrPushGatewayProxyImageInternalFailed,
			err,
			fmt.Sprintf("failed to push gateway proxy image %q to internal registry %q: %v", gatewayProxyImage, internalGatewayProxyImage, err),
			map[string]any{
				"source_image": gatewayProxyImage,
				"target_image": internalGatewayProxyImage,
				"namespace":    "registry",
				"component":    "gateway-proxy",
			},
		)
		Error("Failed to push gateway proxy image to internal registry")
		logStructuredError(logger, wrappedErr, "Failed to push gateway proxy image to internal registry")
		return "", wrappedErr
	}

	Info(fmt.Sprintf("Using internal registry gateway proxy image: %s", internalGatewayProxyImage))
	return internalGatewayProxyImage, nil
}

func prepareAnalyticsImages(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (AnalyticsImageSet, error) {
	Step("Step 5a: Publish analytics images")

	images := AnalyticsImageSet{
		Ingest:    analyticsImageFor(extRegistry, analyticsComponents[0].Repository),
		API:       analyticsImageFor(extRegistry, analyticsComponents[1].Repository),
		Processor: analyticsImageFor(extRegistry, analyticsComponents[2].Repository),
		UI:        analyticsImageFor(extRegistry, analyticsComponents[3].Repository),
	}

	for _, component := range analyticsComponents {
		image := analyticsImageFor(extRegistry, component.Repository)
		if testMode {
			Info(fmt.Sprintf("Test mode: building analytics %s image: %s", component.Name, image))
		} else {
			Info(fmt.Sprintf("Building analytics %s image: %s", component.Name, image))
		}
		if err := deps.BuildAnalyticsImage(image, component.Dockerfile, component.BuildContext); err != nil {
			return AnalyticsImageSet{}, wrapWithSentinelAndContext(
				ErrBuildImageFailed,
				err,
				fmt.Sprintf("failed to build analytics %s image %q: %v", component.Name, image, err),
				map[string]any{"image": image, "component": component.Name},
			)
		}
		if usingExternalRegistry {
			if testMode {
				Info(fmt.Sprintf("Test mode: pushing analytics %s image to external registry", component.Name))
			} else {
				Info(fmt.Sprintf("Pushing analytics %s image to external registry", component.Name))
			}
			if err := deps.PushAnalyticsImage(image); err != nil {
				Warn(fmt.Sprintf("Could not push analytics %s image to external registry: %v", component.Name, err))
			}
			continue
		}

		if testMode {
			Info(fmt.Sprintf("Test mode: pushing analytics %s image to internal registry", component.Name))
		} else {
			Info(fmt.Sprintf("Pushing analytics %s image to internal registry", component.Name))
		}
		internalRegistryURL := deps.GetPlatformRegistryURL(logger)
		_, imageTag := splitImage(image)
		if imageTag == "" {
			imageTag = setupImageTag()
		}
		internalImage := fmt.Sprintf("%s/%s:%s", internalRegistryURL, component.Repository, imageTag)
		if err := deps.EnsureNamespace("registry"); err != nil {
			return AnalyticsImageSet{}, wrapWithSentinelAndContext(
				ErrEnsureRegistryNamespaceFailed,
				err,
				fmt.Sprintf("failed to ensure registry namespace: %v", err),
				map[string]any{"namespace": "registry", "component": component.Name},
			)
		}
		if err := deps.PushAnalyticsImageToInternal(logger, image, internalImage, "registry"); err != nil {
			return AnalyticsImageSet{}, wrapWithSentinelAndContext(
				ErrPushImageInClusterFailed,
				err,
				fmt.Sprintf("failed to push analytics %s image %q to internal registry %q: %v", component.Name, image, internalImage, err),
				map[string]any{"source_image": image, "target_image": internalImage, "component": component.Name},
			)
		}
		switch component.Repository {
		case "mcp-sentinel-ingest":
			images.Ingest = internalImage
		case "mcp-sentinel-api":
			images.API = internalImage
		case "mcp-sentinel-processor":
			images.Processor = internalImage
		case "mcp-sentinel-ui":
			images.UI = internalImage
		}
	}

	return images, nil
}

func deployAnalyticsStepCmd(logger *zap.Logger, images AnalyticsImageSet, storageMode string, deps SetupDeps) error {
	Info("Deploying mcp-sentinel manifests")
	if err := deps.DeployAnalyticsManifests(logger, images, storageMode); err != nil {
		Error("Analytics deployment failed")
		logStructuredError(logger, err, "Analytics deployment failed")
		return err
	}
	return nil
}

func deployOperatorStep(logger *zap.Logger, operatorImage, gatewayProxyImage string, extRegistry *ExternalRegistryConfig, registrySecretName string, usingExternalRegistry bool, operatorArgs []string, deps SetupDeps) error {
	Info("Deploying operator manifests")
	if err := deps.DeployOperatorManifests(logger, operatorImage, gatewayProxyImage, operatorArgs); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrOperatorDeploymentFailed,
			err,
			fmt.Sprintf("operator deployment failed for image %q: %v", operatorImage, err),
			map[string]any{
				"image":     operatorImage,
				"namespace": NamespaceMCPRuntime,
				"component": "operator",
			},
		)
		Error("Operator deployment failed")
		logStructuredError(logger, wrappedErr, "Operator deployment failed")
		return wrappedErr
	}

	if usingExternalRegistry {
		if err := deps.ConfigureProvisionedRegistryEnv(extRegistry, registrySecretName); err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				ErrConfigureExternalRegistryEnvFailed,
				err,
				fmt.Sprintf("failed to configure external registry env on operator (registry: %q, secret: %q): %v", extRegistry.URL, registrySecretName, err),
				map[string]any{
					"registry_url": extRegistry.URL,
					"secret_name":  registrySecretName,
					"namespace":    NamespaceMCPRuntime,
					"component":    "operator",
				},
			)
			Error("Failed to configure external registry environment")
			logStructuredError(logger, wrappedErr, "Failed to configure external registry environment")
			return wrappedErr
		}
	}

	if err := deps.RestartDeployment("mcp-runtime-operator-controller-manager", "mcp-runtime"); err != nil {
		if usingExternalRegistry {
			wrappedErr := wrapWithSentinel(ErrRestartOperatorDeploymentFailed, err, fmt.Sprintf("failed to restart operator deployment after registry env update: %v", err))
			Error("Failed to restart operator deployment")
			logStructuredError(logger, wrappedErr, "Failed to restart operator deployment")
			return wrappedErr
		}
		Warn(fmt.Sprintf("Could not restart operator deployment: %v", err))
	}
	return nil
}

func verifySetup(logger *zap.Logger, usingExternalRegistry bool, deps SetupDeps) error {
	Step("Step 6: Verify platform components")

	if usingExternalRegistry {
		Info("Skipping internal registry availability check (using external registry)")
	} else {
		Info("Waiting for registry deployment to be available")
		if err := deps.WaitForDeploymentAvailable(logger, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
			deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
			regCtx := map[string]any{
				"deployment": "registry",
				"namespace":  "registry",
				"selector":   "app=registry",
				"component":  "registry",
			}
			mergeDeploymentDebugDiagnosticsIfNeeded(kubectlClient, regCtx, "registry", "registry", "app=registry")
			wrappedErr := wrapWithSentinelAndContext(
				ErrRegistryNotReady,
				err,
				fmt.Sprintf("registry not ready: %v", err),
				regCtx,
			)
			Error("Registry not ready")
			logStructuredError(logger, wrappedErr, "Registry not ready")
			return wrappedErr
		}
	}

	Info("Waiting for operator deployment to be available")
	if err := deps.WaitForDeploymentAvailable(logger, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		opCtx := map[string]any{
			"deployment": "mcp-runtime-operator-controller-manager",
			"namespace":  "mcp-runtime",
			"selector":   "control-plane=controller-manager",
			"component":  "operator",
		}
		mergeDeploymentDebugDiagnosticsIfNeeded(kubectlClient, opCtx, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		wrappedErr := wrapWithSentinelAndContext(
			ErrOperatorNotReady,
			err,
			fmt.Sprintf("operator not ready: %v", err),
			opCtx,
		)
		Error("Operator not ready")
		logStructuredError(logger, wrappedErr, "Operator not ready")
		return wrappedErr
	}

	Info("Checking MCPServer CRD presence")
	if err := deps.CheckCRDInstalled("mcpservers.mcpruntime.org"); err != nil {
		crdName := "mcpservers.mcpruntime.org"
		crdCtx := map[string]any{"crd": crdName, "component": "crd-check"}
		mergeCRDCheckDebugDiagnosticsIfNeeded(kubectlClient, crdCtx, crdName)
		wrappedErr := wrapWithSentinelAndContext(ErrCRDCheckFailed, err, fmt.Sprintf("CRD check failed: %v", err), crdCtx)
		Error("CRD check failed")
		logStructuredError(logger, wrappedErr, "CRD check failed")
		return wrappedErr
	}

	Success("Verification complete")
	return nil
}

func getOperatorImage(ext *ExternalRegistryConfig) string {
	tag := setupImageTag()

	// Check for explicit override first
	if override := GetOperatorImageOverride(); override != "" {
		return override
	}

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/mcp-runtime-operator:" + tag
	}
	return fmt.Sprintf("%s/mcp-runtime-operator:%s", getPlatformRegistryURL(nil), tag)
}

func getGatewayProxyImage(ext *ExternalRegistryConfig) string {
	tag := setupImageTag()

	if override := GetGatewayProxyImageOverride(); override != "" {
		return override
	}

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/" + defaultGatewayProxyRepository + ":" + tag
	}
	return fmt.Sprintf("%s/%s:%s", getPlatformRegistryURL(nil), defaultGatewayProxyRepository, tag)
}

func analyticsImageFor(ext *ExternalRegistryConfig, repository string) string {
	tag := setupImageTag()

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/" + repository + ":" + tag
	}
	return fmt.Sprintf("%s/%s:%s", getPlatformRegistryURL(nil), repository, tag)
}

func configureProvisionedRegistryEnv(ext *ExternalRegistryConfig, secretName string) error {
	return configureProvisionedRegistryEnvWithKubectl(kubectlClient, ext, secretName)
}

func configureProvisionedRegistryEnvWithKubectl(kubectl KubectlRunner, ext *ExternalRegistryConfig, secretName string) error {
	if ext == nil || ext.URL == "" {
		return nil
	}
	hasCreds := ext.Username != "" || ext.Password != ""
	if hasCreds && secretName == "" {
		secretName = defaultRegistrySecretName
	}
	args := []string{
		"set", "env", "deployment/mcp-runtime-operator-controller-manager",
		"-n", "mcp-runtime",
		"PROVISIONED_REGISTRY_URL=" + ext.URL,
	}
	if hasCreds {
		if err := ensureProvisionedRegistrySecretWithKubectl(kubectl, secretName, ext.Username, ext.Password); err != nil {
			return err
		}
		// Create imagePullSecret in mcp-servers namespace for pod image pulls.
		if err := ensureImagePullSecretWithKubectl(kubectl, NamespaceMCPServers, secretName, ext.URL, ext.Username, ext.Password); err != nil {
			return err
		}
		args = append(args, "PROVISIONED_REGISTRY_SECRET_NAME="+secretName)
		// Populate env vars from the secret instead of literals to avoid leaking creds in args/history.
		args = append(args, "--from=secret/"+secretName)
	}
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput(args, os.Stdout, os.Stderr)
}

func ensureProvisionedRegistrySecretWithKubectl(kubectl KubectlRunner, name, username, password string) error {
	var envData strings.Builder
	if username != "" {
		envData.WriteString("PROVISIONED_REGISTRY_USERNAME=")
		envData.WriteString(username)
		envData.WriteString("\n")
	}
	if password != "" {
		envData.WriteString("PROVISIONED_REGISTRY_PASSWORD=")
		envData.WriteString(password)
		envData.WriteString("\n")
	}
	if envData.Len() == 0 {
		return nil
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	createCmd, err := kubectl.CommandArgs([]string{
		"create", "secret", "generic", name,
		"--from-env-file=-",
		"-n", NamespaceMCPRuntime,
		"--dry-run=client",
		"-o", "yaml",
	})
	if err != nil {
		return err
	}
	createCmd.SetStdin(strings.NewReader(envData.String()))
	var rendered bytes.Buffer
	createCmd.SetStdout(&rendered)
	createCmd.SetStderr(os.Stderr)
	if err := createCmd.Run(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrRenderSecretManifestFailed,
			err,
			fmt.Sprintf("render secret manifest: %v", err),
			map[string]any{"secret_name": name, "namespace": NamespaceMCPRuntime, "component": "setup"},
		)
		Error("Failed to render secret manifest")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	applyCmd, err := kubectl.CommandArgs([]string{"apply", "-f", "-"})
	if err != nil {
		return err
	}
	applyCmd.SetStdin(&rendered)
	applyCmd.SetStdout(os.Stdout)
	applyCmd.SetStderr(os.Stderr)
	if err := applyCmd.Run(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplySecretManifestFailed,
			err,
			fmt.Sprintf("apply secret manifest: %v", err),
			map[string]any{"secret_name": name, "namespace": NamespaceMCPRuntime, "component": "setup"},
		)
		Error("Failed to apply secret manifest")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	return nil
}

func ensureImagePullSecretWithKubectl(kubectl KubectlRunner, namespace, name, registry, username, password string) error {
	if username == "" && password == "" {
		return nil
	}

	dockerCfg := map[string]any{
		"auths": map[string]any{
			registry: map[string]string{
				"username": username,
				"password": password,
				"auth":     base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password))),
			},
		},
	}
	dockerCfgJSON, err := json.Marshal(dockerCfg)
	if err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrMarshalDockerConfigFailed,
			err,
			fmt.Sprintf("marshal docker config: %v", err),
			map[string]any{"registry": registry, "namespace": namespace, "component": "setup"},
		)
		Error("Failed to marshal docker config")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	// Build secret manifest
	secretManifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: %s
`, name, namespace, base64.StdEncoding.EncodeToString(dockerCfgJSON))

	// Apply secret manifest
	applyCmd, err := kubectl.CommandArgs([]string{"apply", "-f", "-"})
	if err != nil {
		return err
	}
	applyCmd.SetStdin(strings.NewReader(secretManifest))
	applyCmd.SetStdout(os.Stdout)
	applyCmd.SetStderr(os.Stderr)
	if err := applyCmd.Run(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyImagePullSecretFailed,
			err,
			fmt.Sprintf("apply imagePullSecret: %v", err),
			map[string]any{"secret_name": name, "namespace": namespace, "registry": registry, "component": "setup"},
		)
		Error("Failed to apply image pull secret")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	return nil
}

func buildOperatorImage(image string) error {
	target := "docker-build-operator-no-test"
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := execCommandWithValidators("make", []string{"-f", "Makefile.operator", target, "IMG=" + image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func buildGatewayProxyImage(image string) error {
	dockerfilePath, err := resolveRepoAssetPath(gatewayProxyDockerfilePath)
	if err != nil {
		return err
	}
	buildContext, err := resolveRepoAssetPath(gatewayProxyBuildContext)
	if err != nil {
		return err
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := execCommandWithValidators("docker", []string{
		"build",
		"-f", dockerfilePath,
		"-t", image,
		buildContext,
	})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func buildAnalyticsImage(image, dockerfilePath, buildContext string) error {
	resolvedDockerfilePath, err := resolveRepoAssetPath(dockerfilePath)
	if err != nil {
		return err
	}
	resolvedBuildContext, err := resolveRepoAssetPath(buildContext)
	if err != nil {
		return err
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := execCommandWithValidators("docker", []string{
		"build",
		"-f", resolvedDockerfilePath,
		"-t", image,
		resolvedBuildContext,
	})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func restartDeployment(name, namespace string) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return restartDeploymentWithKubectl(kubectlClient, name, namespace)
}

func restartDeploymentWithKubectl(kubectl KubectlRunner, name, namespace string) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput([]string{"rollout", "restart", "deployment/" + name, "-n", namespace}, os.Stdout, os.Stderr)
}

func pushOperatorImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := execCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushGatewayProxyImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := execCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushAnalyticsImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := execCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushOperatorImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "setup"},
		)
		Error("Failed to push image in-cluster")
		logStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func pushGatewayProxyImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "gateway-proxy"},
		)
		Error("Failed to push image in-cluster")
		logStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func pushAnalyticsImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "analytics"},
		)
		Error("Failed to push image in-cluster")
		logStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func checkCRDInstalled(name string) error {
	// #nosec G204 -- name is hardcoded CRD identifier from internal code.
	return checkCRDInstalledWithKubectl(kubectlClient, name)
}

func checkCRDInstalledWithKubectl(kubectl KubectlRunner, name string) error {
	// #nosec G204 -- name is hardcoded CRD identifier from internal code.
	return kubectl.RunWithOutput([]string{"get", "crd", name}, os.Stdout, os.Stderr)
}

// waitForDeploymentAvailable polls a deployment until it has at least one available replica or times out.
func waitForDeploymentAvailable(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
	return waitForDeploymentAvailableWithKubectl(kubectlClient, logger, name, namespace, selector, timeout)
}

// waitForDeploymentAvailableWithKubectl polls a deployment until it has at least one available replica or times out.
func waitForDeploymentAvailableWithKubectl(kubectl KubectlRunner, logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastLog := time.Time{}
	for {
		// #nosec G204 -- name/namespace from internal setup logic, not direct user input.
		cmd, err := kubectl.CommandArgs([]string{"get", "deployment", name, "-n", namespace, "-o", "jsonpath={.status.availableReplicas}"})
		if err == nil {
			out, execErr := cmd.Output()
			if execErr == nil {
				val := strings.TrimSpace(string(out))
				if val == "" {
					val = "0"
				}
				if n, convErr := strconv.Atoi(val); convErr == nil && n > 0 {
					return nil
				}
			}
		}
		if time.Since(lastLog) > 10*time.Second {
			Info(fmt.Sprintf("Still waiting for deployment/%s in %s (selector %s, timeout %s)", name, namespace, selector, timeout.Round(time.Second)))
			lastLog = time.Now()
		}
		if time.Now().After(deadline) {
			msg := fmt.Sprintf("timed out waiting for deployment %s in namespace %s", name, namespace)
			cause := errors.New("deployment readiness deadline exceeded")
			ctx := map[string]any{
				"deployment": name,
				"namespace":  namespace,
				"selector":   selector,
				"component":  "deployment-wait",
			}
			mergeDeploymentDebugDiagnosticsIfNeeded(kubectl, ctx, name, namespace, selector)
			wrappedErr := wrapWithSentinelAndContext(ErrDeploymentTimeout, cause, msg, ctx)
			Error("Deployment timeout")
			if logger != nil {
				logStructuredError(logger, wrappedErr, "Deployment timeout")
			}
			return wrappedErr
		}
		time.Sleep(5 * time.Second)
	}
}

// printDeploymentDiagnostics prints a quick status of pods for a deployment selector to help users triage readiness issues.
func printDeploymentDiagnostics(deploy, namespace, selector string) {
	printDeploymentDiagnosticsWithKubectl(kubectlClient, deploy, namespace, selector)
}

// printDeploymentDiagnosticsWithKubectl prints a quick status of pods for a deployment selector.
func printDeploymentDiagnosticsWithKubectl(kubectl KubectlRunner, deploy, namespace, selector string) {
	Warn(fmt.Sprintf("Deployment %s in %s is not ready. Showing pod statuses:", deploy, namespace))
	// #nosec G204 -- namespace/selector from internal diagnostics, not user input.
	_ = kubectl.RunWithOutput([]string{"get", "pods", "-n", namespace, "-l", selector, "-o", "wide"}, os.Stdout, os.Stderr)
}

// mergeDeploymentDebugDiagnosticsIfNeeded fetches describe/events/pods from the API when --debug is set
// and attaches a bounded blob to the errx context (cluster-backed failures, not local validation).
func mergeDeploymentDebugDiagnosticsIfNeeded(kubectl KubectlRunner, m map[string]any, deployName, namespace, selector string) {
	if !IsDebugMode() {
		return
	}
	if d := buildDeploymentWaitDebugDetail(kubectl, deployName, namespace, selector); d != "" {
		m["diagnostics"] = trimDiagnosticsString(d)
	}
}

// buildDeploymentWaitDebugDetail returns kubectl text for a stuck or timed-out deployment wait.
func buildDeploymentWaitDebugDetail(kubectl KubectlRunner, deployName, namespace, selector string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("---- describe deployment %s\n", deployName))
	// #nosec G204 -- deploy/namespace/selector are internal setup identifiers, not user shell input.
	if out, err := kubectlText(kubectl, []string{
		"describe", "deployment", deployName, "-n", namespace, "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get pods (selector)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "pods", "-n", namespace, "-l", selector, "-o", "wide", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get events (sorted)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "events", "-n", namespace, "--sort-by", ".lastTimestamp", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

// buildNamespacedResourceDebugDetail returns describe, pods, and events for a namespaced object (e.g. StatefulSet, Job).
func buildNamespacedResourceDebugDetail(kubectl KubectlRunner, kind, name, namespace string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("---- describe %s %s\n", kind, name))
	// #nosec G204 -- kind/name/namespace are internal resource identifiers, not user shell input.
	if out, err := kubectlText(kubectl, []string{
		"describe", kind, name, "-n", namespace, "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get pods (namespace)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "pods", "-n", namespace, "-o", "wide", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get events (sorted)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "events", "-n", namespace, "--sort-by", ".lastTimestamp", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

// buildCRDCheckDebugDetail returns CRD and api-resources text when a CRD presence check fails.
func buildCRDCheckDebugDetail(kubectl KubectlRunner, crdName string) string {
	var b strings.Builder
	b.WriteString("---- get crd\n")
	// #nosec G204 -- crdName is a hardcoded internal API identity.
	if out, err := kubectlText(kubectl, []string{
		"get", "crd", crdName, "-o", "wide", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("get crd: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- api-resources (group mcpruntime.org)\n")
	if out, err := kubectlText(kubectl, []string{
		"api-resources", "--api-group=mcpruntime.org", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

func mergeCRDCheckDebugDiagnosticsIfNeeded(kubectl KubectlRunner, m map[string]any, crdName string) {
	if !IsDebugMode() {
		return
	}
	if d := buildCRDCheckDebugDetail(kubectl, crdName); d != "" {
		m["diagnostics"] = trimDiagnosticsString(d)
	}
}

// deployOperatorManifests deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifests(logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error {
	return deployOperatorManifestsWithKubectl(kubectlClient, logger, operatorImage, gatewayProxyImage, operatorArgs)
}

// deployOperatorManifestsWithKubectl deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name and injecting operator args/env.
func deployOperatorManifestsWithKubectl(kubectl KubectlRunner, logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error {
	// Step 1: Apply CRD
	Info("Applying CRD manifests")
	// #nosec G204 -- fixed directory path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "--validate=false", "-f", "config/crd/bases"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinel(ErrApplyCRDFailed, err, fmt.Sprintf("failed to apply CRD: %v", err))
		Error("Failed to apply CRD")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply CRD")
		}
		return wrappedErr
	}

	// Step 2: Apply RBAC (ServiceAccount, Role, RoleBinding)
	Info("Applying RBAC manifests")
	if err := ensureNamespace(NamespaceMCPRuntime); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrEnsureOperatorNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure operator namespace: %v", err),
			map[string]any{"namespace": NamespaceMCPRuntime, "component": "setup"},
		)
		Error("Failed to ensure operator namespace")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to ensure operator namespace")
		}
		return wrappedErr
	}

	// #nosec G204 -- fixed kustomize path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "-k", "config/rbac/"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinel(ErrApplyRBACFailed, err, fmt.Sprintf("failed to apply RBAC: %v", err))
		Error("Failed to apply RBAC")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply RBAC")
		}
		return wrappedErr
	}

	// Step 3: Apply manager deployment with structured image replacement
	Info("Applying operator deployment")

	// Read manager.yaml and apply structured mutations
	managerYAML, err := os.ReadFile("config/manager/manager.yaml")
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to read manager.yaml: %v", err))
		Error("Failed to read manager.yaml")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to read manager.yaml")
		}
		return wrappedErr
	}

	// Use structured manifest mutation instead of regex
	mutator, err := manifest.NewMutator(managerYAML)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrParseManagerYAMLFailed, err, fmt.Sprintf("failed to parse manager.yaml: %v", err))
		Error("Failed to parse manager.yaml")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to parse manager.yaml")
		}
		return wrappedErr
	}

	// Set the operator image
	if err := mutator.SetDeploymentImage(OperatorDeploymentName, OperatorManagerContainerName, operatorImage); err != nil {
		wrappedErr := wrapWithSentinel(ErrSetOperatorImageFailed, err, fmt.Sprintf("failed to set operator image: %v", err))
		Error("Failed to set operator image")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to set operator image")
		}
		return wrappedErr
	}

	// Set image pull policy based on image
	pullPolicy := operatorImagePullPolicy(operatorImage)
	if pullPolicy != "" {
		if err := mutator.SetDeploymentImagePullPolicy(OperatorDeploymentName, OperatorManagerContainerName, pullPolicy); err != nil {
			wrappedErr := wrapWithSentinel(ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to set operator image pull policy: %v", err))
			Error("Failed to set operator image pull policy")
			if logger != nil {
				logStructuredError(logger, wrappedErr, "Failed to set operator image pull policy")
			}
			return wrappedErr
		}
	}

	// Inject operator args if provided
	if len(operatorArgs) > 0 {
		if err := mutator.MergeDeploymentArgs(OperatorDeploymentName, OperatorManagerContainerName, operatorArgs); err != nil {
			wrappedErr := wrapWithSentinel(ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to merge operator args: %v", err))
			Error("Failed to merge operator args")
			if logger != nil {
				logStructuredError(logger, wrappedErr, "Failed to merge operator args")
			}
			return wrappedErr
		}
	}

	// Inject environment variables if provided
	if envVars := operatorEnvOverrides(gatewayProxyImage); len(envVars) > 0 {
		envMap := make(map[string]string, len(envVars))
		for _, ev := range envVars {
			envMap[ev.Name] = ev.Value
		}
		if err := mutator.MergeDeploymentEnv(OperatorDeploymentName, OperatorManagerContainerName, envMap); err != nil {
			wrappedErr := wrapWithSentinel(ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to merge operator env vars: %v", err))
			Error("Failed to merge operator env vars")
			if logger != nil {
				logStructuredError(logger, wrappedErr, "Failed to merge operator env vars")
			}
			return wrappedErr
		}
	}

	// Render the mutated manifest
	mutatedYAML, err := mutator.ToYAML()
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrRenderManagerYAMLFailed, err, fmt.Sprintf("failed to render mutated manifest: %v", err))
		Error("Failed to render mutated manifest")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to render mutated manifest")
		}
		return wrappedErr
	}

	// Write to temp file under the working directory so kubectl path validation passes.
	tmpFile, err := os.CreateTemp(".", "manager-*.yaml")
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrCreateTempFileFailed, err, fmt.Sprintf("failed to create temp file: %v", err))
		Error("Failed to create temp file")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to create temp file")
		}
		return wrappedErr
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(mutatedYAML); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			wrappedErr := wrapWithSentinel(ErrCloseTempFileFailed, errors.Join(err, closeErr), fmt.Sprintf("failed to close temp file after write error: %v", closeErr))
			Error("Failed to close temp file")
			if logger != nil {
				logStructuredError(logger, wrappedErr, "Failed to close temp file")
			}
			return wrappedErr
		}
		wrappedErr := wrapWithSentinel(ErrWriteTempFileFailed, err, fmt.Sprintf("failed to write temp file: %v", err))
		Error("Failed to write temp file")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to write temp file")
		}
		return wrappedErr
	}
	if err := tmpFile.Close(); err != nil {
		wrappedErr := wrapWithSentinel(ErrCloseTempFileFailed, err, fmt.Sprintf("failed to close temp file: %v", err))
		Error("Failed to close temp file")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to close temp file")
		}
		return wrappedErr
	}

	// Delete existing deployment to avoid immutable selector conflicts on reapply.
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	_ = kubectl.Run([]string{"delete", "deployment/" + OperatorDeploymentName, "-n", NamespaceMCPRuntime, "--ignore-not-found"})

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := kubectl.RunWithOutput([]string{"apply", "-f", tmpFile.Name()}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyManagerDeploymentFailed,
			err,
			fmt.Sprintf("failed to apply manager deployment: %v", err),
			map[string]any{"operator_image": operatorImage, "namespace": NamespaceMCPRuntime, "component": "setup"},
		)
		Error("Failed to apply manager deployment")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply manager deployment")
		}
		return wrappedErr
	}

	Success("Operator manifests deployed successfully")
	return nil
}

// mcpSentinelDependencyRolloutFailed wraps early mcp-sentinel storage/messaging rollouts; diagnostics are attached only in --debug.
func mcpSentinelDependencyRolloutFailed(kubectl KubectlRunner, err error, kind, name, namespace, phase string) error {
	ctx := map[string]any{
		"component": "mcp-sentinel",
		"phase":     phase,
		"resource":  fmt.Sprintf("%s/%s", kind, name),
		"namespace": namespace,
	}
	if IsDebugMode() {
		if diag := buildNamespacedResourceDebugDetail(kubectl, kind, name, namespace); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return wrapWithSentinelAndContext(ErrOperatorDeploymentFailed, err,
		fmt.Sprintf("mcp-sentinel %s: %s/%s: %v", phase, kind, name, err), ctx)
}

// mcpSentinelDependencyJobFailed wraps the clickhouse init job; diagnostics are attached only in --debug.
func mcpSentinelDependencyJobFailed(kubectl KubectlRunner, err error, name, namespace, phase string) error {
	ctx := map[string]any{
		"component": "mcp-sentinel",
		"phase":     phase,
		"resource":  "job/" + name,
		"namespace": namespace,
	}
	if IsDebugMode() {
		if diag := buildNamespacedResourceDebugDetail(kubectl, "job", name, namespace); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return wrapWithSentinelAndContext(ErrOperatorDeploymentFailed, err,
		fmt.Sprintf("mcp-sentinel %s: job/%s: %v", phase, name, err), ctx)
}

func deployAnalyticsManifests(logger *zap.Logger, images AnalyticsImageSet, storageMode string) error {
	return deployAnalyticsManifestsWithKubectl(kubectlClient, logger, images, storageMode)
}

func deployAnalyticsManifestsWithKubectl(kubectl KubectlRunner, logger *zap.Logger, images AnalyticsImageSet, storageMode string) error {
	rolloutTimeout := analyticsRolloutTimeoutString()
	Info("Applying mcp-sentinel namespace and config")
	manifests := []string{
		"k8s/00-namespace.yaml",
		"k8s/01-config.yaml",
	}
	for _, manifest := range manifests {
		if err := applyRenderedManifest(kubectl, manifest, images, ""); err != nil {
			return err
		}
	}

	Info("Applying mcp-sentinel managed secrets")
	secretManifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		return err
	}
	if err := applyManifestContent(kubectl, secretManifest); err != nil {
		return err
	}

	imagePullSecretName, err := ensureAnalyticsImagePullSecret(kubectl)
	if err != nil {
		return err
	}

	clickhouseManifest := "k8s/03-clickhouse.yaml"
	kafkaManifest := "k8s/05-kafka.yaml"
	if storageMode == StorageModeHostpath {
		clickhouseManifest = "k8s/03-clickhouse-hostpath.yaml"
		kafkaManifest = "k8s/05-kafka-hostpath.yaml"
	}

	Info("Applying analytics storage and messaging components")
	for _, manifest := range []string{
		clickhouseManifest,
		kafkaManifest,
	} {
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName); err != nil {
			return err
		}
	}

	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "clickhouse", defaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "clickhouse", defaultAnalyticsNamespace, "storage (clickhouse)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "deployment", "zookeeper", defaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "deployment", "zookeeper", defaultAnalyticsNamespace, "messaging (zookeeper)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "kafka", defaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "kafka", defaultAnalyticsNamespace, "messaging (kafka)")
	}

	Info("Initializing ClickHouse schema")
	if err := applyRenderedManifest(kubectl, "k8s/04-clickhouse-init.yaml", images, imagePullSecretName); err != nil {
		return err
	}
	if err := waitForJobCompletionWithKubectl(kubectl, "clickhouse-init", defaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyJobFailed(kubectl, err, "clickhouse-init", defaultAnalyticsNamespace, "clickhouse init schema")
	}

	Info("Applying analytics services")
	for _, manifest := range []string{
		"k8s/06-ingest.yaml",
		"k8s/07-processor.yaml",
		"k8s/08-api.yaml",
		"k8s/08-api-rbac.yaml",
		"k8s/09-ui.yaml",
		"k8s/10-gateway.yaml",
		"k8s/11-prometheus.yaml",
		"k8s/15-otel-collector.yaml",
		"k8s/16-tempo.yaml",
		"k8s/17-loki.yaml",
		"k8s/18-promtail.yaml",
		"k8s/19-grafana-datasources.yaml",
		"k8s/12-grafana.yaml",
	} {
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName); err != nil {
			return err
		}
	}

	Info(fmt.Sprintf("Waiting for mcp-sentinel workload rollouts (per-resource timeout %s; override with MCP_DEPLOYMENT_TIMEOUT)", rolloutTimeout))
	targets := []struct{ kind, name string }{
		{kind: "deployment", name: "mcp-sentinel-ingest"},
		{kind: "deployment", name: "mcp-sentinel-processor"},
		{kind: "deployment", name: "mcp-sentinel-api"},
		{kind: "deployment", name: "mcp-sentinel-ui"},
		{kind: "deployment", name: "mcp-sentinel-gateway"},
		{kind: "deployment", name: "prometheus"},
		{kind: "deployment", name: "grafana"},
		{kind: "deployment", name: "otel-collector"},
		{kind: "statefulset", name: "tempo"},
		{kind: "statefulset", name: "loki"},
	}
	var rolloutFailures []string
	var failedForDebug []analyticsFailedRollout
	for _, target := range targets {
		rolloutLog, err := runRolloutWithOptionalDebugCapture(kubectl, target.kind, target.name, defaultAnalyticsNamespace, rolloutTimeout)
		if err != nil {
			rolloutFailures = append(rolloutFailures, fmt.Sprintf("%s/%s: %v", target.kind, target.name, err))
			failedForDebug = append(failedForDebug, analyticsFailedRollout{
				kind: target.kind, name: target.name, rolloutLog: rolloutLog,
			})
		}
	}
	if len(rolloutFailures) == 0 {
		Success("mcp-sentinel manifests deployed successfully")
		return nil
	}

	printAnalyticsRolloutDiagnostics(kubectl)
	summary := strings.Join(rolloutFailures, "; ")
	cause := errors.New(summary)
	msg := fmt.Sprintf("analytics components failed to roll out: %s", summary)
	ctx := map[string]any{"component": "mcp-sentinel", "rollout_failures": summary}
	if IsDebugMode() {
		if diag := buildAnalyticsRolloutDebugDetail(kubectl, failedForDebug); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return wrapWithSentinelAndContext(ErrOperatorDeploymentFailed, cause, msg, ctx)
}

func trimDiagnosticsString(s string) string {
	const maxBytes = 300 * 1024
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... [diagnostics truncated]\n"
}

// runRolloutWithOptionalDebugCapture runs kubectl rollout status, teeing output to a buffer
// in --debug mode so it can be attached to the structured error.
func runRolloutWithOptionalDebugCapture(kubectl KubectlRunner, kind, name, namespace, timeout string) (capture string, err error) {
	args := []string{
		"rollout", "status",
		fmt.Sprintf("%s/%s", kind, name),
		"-n", namespace, "--timeout=" + timeout,
	}
	if !IsDebugMode() {
		return "", kubectl.RunWithOutput(args, os.Stdout, os.Stderr)
	}
	var buf bytes.Buffer
	w := io.MultiWriter(os.Stdout, &buf)
	err = kubectl.RunWithOutput(args, w, w)
	return buf.String(), err
}

func kubectlText(kubectl KubectlRunner, args []string) (string, error) {
	cmd, err := kubectl.CommandArgs(args)
	if err != nil {
		return "", err
	}
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// analyticsFailedRollout records a failed rollout and optional tee capture from runRolloutWithOptionalDebugCapture.
type analyticsFailedRollout struct {
	kind, name, rolloutLog string
}

// buildAnalyticsRolloutDebugDetail collects kubectl output for mcp-sentinel (describe + get) when --debug is set.
func buildAnalyticsRolloutDebugDetail(kubectl KubectlRunner, failed []analyticsFailedRollout) string {
	var b strings.Builder
	for _, w := range failed {
		if strings.TrimSpace(w.rolloutLog) != "" {
			b.WriteString(fmt.Sprintf("---- kubectl rollout status %s/%s\n", w.kind, w.name))
			b.WriteString(w.rolloutLog)
		}
		b.WriteString(fmt.Sprintf("---- describe %s %s\n", w.kind, w.name))
		out, err := kubectlText(kubectl, []string{
			"describe", w.kind, w.name, "-n", defaultAnalyticsNamespace, "--request-timeout=30s",
		})
		if err != nil {
			b.WriteString(fmt.Sprintf("error: %v\n", err))
			continue
		}
		b.WriteString(out)
	}
	b.WriteString("---- get pods (wide)\n")
	if out, err := kubectlText(kubectl, []string{"get", "pods", "-n", defaultAnalyticsNamespace, "-o", "wide", "--request-timeout=30s"}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get events (sorted)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "events", "-n", defaultAnalyticsNamespace, "--sort-by", ".lastTimestamp", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

func applyRenderedManifest(kubectl KubectlRunner, manifestPath string, images AnalyticsImageSet, imagePullSecretName string) error {
	resolvedManifestPath, err := resolveRepoAssetPath(manifestPath)
	if err != nil {
		return wrapWithSentinel(ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to resolve manifest %s: %v", manifestPath, err))
	}

	content, err := readFileAtPath(resolvedManifestPath)
	if err != nil {
		return wrapWithSentinel(ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to read manifest %s: %v", resolvedManifestPath, err))
	}
	rendered, err := renderAnalyticsManifest(string(content), images, imagePullSecretName)
	if err != nil {
		return fmt.Errorf("render manifest %s: %w", manifestPath, err)
	}
	return applyManifestContent(kubectl, rendered)
}

func applyManifestContent(kubectl KubectlRunner, manifest string) error {
	return applyManifestContentWithNamespace(kubectl, manifest, "")
}

func applyManifestContentWithNamespace(kubectl KubectlRunner, manifest, namespace string) error {
	args := []string{"apply", "-f", "-"}
	if strings.TrimSpace(namespace) != "" {
		args = append(args, "-n", namespace)
	}
	applyCmd, err := kubectl.CommandArgs(args)
	if err != nil {
		return err
	}
	applyCmd.SetStdin(strings.NewReader(manifest))
	applyCmd.SetStdout(os.Stdout)
	applyCmd.SetStderr(os.Stderr)
	return applyCmd.Run()
}

func renderAnalyticsManifest(content string, images AnalyticsImageSet, imagePullSecretName string) (string, error) {
	replacements := map[string]string{}
	if strings.TrimSpace(images.Ingest) != "" {
		replacements["image: mcp-sentinel-ingest:latest"] = "image: " + images.Ingest
	}
	if strings.TrimSpace(images.API) != "" {
		replacements["image: mcp-sentinel-api:latest"] = "image: " + images.API
	}
	if strings.TrimSpace(images.Processor) != "" {
		replacements["image: mcp-sentinel-processor:latest"] = "image: " + images.Processor
	}
	if strings.TrimSpace(images.UI) != "" {
		replacements["image: mcp-sentinel-ui:latest"] = "image: " + images.UI
	}
	if strings.TrimSpace(images.Traefik) != "" {
		replacements["image: traefik:v3.0"] = "image: " + images.Traefik
	}
	if strings.TrimSpace(images.ClickHouse) != "" {
		replacements["image: clickhouse/clickhouse-server:23.8"] = "image: " + images.ClickHouse
	}
	if strings.TrimSpace(images.Zookeeper) != "" {
		replacements["image: confluentinc/cp-zookeeper:7.5.1"] = "image: " + images.Zookeeper
	}
	if strings.TrimSpace(images.Kafka) != "" {
		replacements["image: confluentinc/cp-kafka:7.5.1"] = "image: " + images.Kafka
	}
	if strings.TrimSpace(images.Prometheus) != "" {
		replacements["image: prom/prometheus:v2.49.1"] = "image: " + images.Prometheus
	}
	if strings.TrimSpace(images.OTelCollector) != "" {
		replacements["image: otel/opentelemetry-collector:0.92.0"] = "image: " + images.OTelCollector
	}
	if strings.TrimSpace(images.Tempo) != "" {
		replacements["image: grafana/tempo:2.3.1"] = "image: " + images.Tempo
	}
	if strings.TrimSpace(images.Loki) != "" {
		replacements["image: grafana/loki:2.9.4"] = "image: " + images.Loki
	}
	if strings.TrimSpace(images.Promtail) != "" {
		replacements["image: grafana/promtail:2.9.4"] = "image: " + images.Promtail
	}
	if strings.TrimSpace(images.Grafana) != "" {
		replacements["image: grafana/grafana:10.2.3"] = "image: " + images.Grafana
	}
	rendered := content
	for oldValue, newValue := range replacements {
		rendered = strings.ReplaceAll(rendered, oldValue, newValue)
	}
	if strings.TrimSpace(imagePullSecretName) == "" {
		return rendered, nil
	}

	rendered, err := injectImagePullSecretsIntoManifest(rendered, imagePullSecretName)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

func renderAnalyticsSecretManifest(kubectl KubectlRunner) (string, error) {
	apiKeys, err := existingSecretDataValueOrRandom(kubectl, defaultAnalyticsNamespace, "mcp-sentinel-secrets", "API_KEYS", 16)
	if err != nil {
		return "", wrapWithSentinel(ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	uiAPIKey, err := existingSecretDataValueOrRandom(kubectl, defaultAnalyticsNamespace, "mcp-sentinel-secrets", "UI_API_KEY", 16)
	if err != nil {
		return "", wrapWithSentinel(ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	grafanaPassword, err := existingSecretDataValueOrRandom(kubectl, defaultAnalyticsNamespace, "mcp-sentinel-secrets", "GRAFANA_ADMIN_PASSWORD", 16)
	if err != nil {
		return "", wrapWithSentinel(ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	secretManifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: mcp-sentinel-secrets
  namespace: %s
type: Opaque
stringData:
  API_KEYS: "%s"
  UI_API_KEY: "%s"
  GRAFANA_ADMIN_USER: "admin"
  GRAFANA_ADMIN_PASSWORD: "%s"
`, defaultAnalyticsNamespace, apiKeys, uiAPIKey, grafanaPassword)
	return secretManifest, nil
}

func ensureAnalyticsImagePullSecret(kubectl KubectlRunner) (string, error) {
	extRegistry, err := resolveExternalRegistryConfig(nil)
	if err != nil {
		return "", err
	}
	if extRegistry == nil || extRegistry.URL == "" || (extRegistry.Username == "" && extRegistry.Password == "") {
		return "", nil
	}
	if err := ensureImagePullSecretWithKubectl(kubectl, defaultAnalyticsNamespace, defaultRegistrySecretName, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
		return "", err
	}
	return defaultRegistrySecretName, nil
}

func existingSecretDataValue(kubectl KubectlRunner, namespace, name, key string) (string, error) {
	cmd, err := kubectl.CommandArgs([]string{"get", "secret", name, "-n", namespace, "-o", "jsonpath={.data." + key + "}"})
	if err != nil {
		return "", err
	}

	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "notfound") {
			return "", nil
		}
		return "", fmt.Errorf("read secret %s/%s key %s: %w", namespace, name, key, err)
	}
	if trimmed == "" {
		return "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return "", fmt.Errorf("decode secret %s/%s key %s: %w", namespace, name, key, err)
	}
	return string(decoded), nil
}

func existingSecretDataValueOrRandom(kubectl KubectlRunner, namespace, name, key string, size int) (string, error) {
	value, err := existingSecretDataValue(kubectl, namespace, name, key)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	return randomHex(size)
}

func injectImagePullSecretsIntoManifest(manifest, secretName string) (string, error) {
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	var renderedDocs []string

	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if len(doc) == 0 {
			continue
		}

		injectImagePullSecretIntoDocument(doc, secretName)
		data, err := yaml.Marshal(doc)
		if err != nil {
			return "", err
		}
		renderedDocs = append(renderedDocs, strings.TrimRight(string(data), "\n"))
	}

	if len(renderedDocs) == 0 {
		return manifest, nil
	}
	return strings.Join(renderedDocs, "\n---\n") + "\n", nil
}

func injectImagePullSecretIntoDocument(doc map[string]any, secretName string) {
	podSpec := manifestPodSpec(doc)
	if podSpec == nil {
		return
	}

	if existing, ok := podSpec["imagePullSecrets"].([]any); ok {
		for _, item := range existing {
			entry, ok := item.(map[string]any)
			if ok && strings.TrimSpace(fmt.Sprint(entry["name"])) == secretName {
				return
			}
		}
		podSpec["imagePullSecrets"] = append(existing, map[string]any{"name": secretName})
		return
	}

	podSpec["imagePullSecrets"] = []map[string]any{{"name": secretName}}
}

func manifestPodSpec(doc map[string]any) map[string]any {
	kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(doc["kind"])))
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}

	switch kind {
	case "deployment", "statefulset", "daemonset", "job":
		template := ensureMap(spec, "template")
		return ensureMap(template, "spec")
	case "cronjob":
		jobTemplate := ensureMap(spec, "jobTemplate")
		jobSpec := ensureMap(jobTemplate, "spec")
		template := ensureMap(jobSpec, "template")
		return ensureMap(template, "spec")
	default:
		return nil
	}
}

func ensureMap(root map[string]any, key string) map[string]any {
	if existing, ok := root[key].(map[string]any); ok && existing != nil {
		return existing
	}
	created := map[string]any{}
	root[key] = created
	return created
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func waitForRolloutStatusWithKubectl(kubectl KubectlRunner, kind, name, namespace, timeout string) error {
	return kubectl.RunWithOutput([]string{"rollout", "status", fmt.Sprintf("%s/%s", kind, name), "-n", namespace, "--timeout=" + timeout}, os.Stdout, os.Stderr)
}

// analyticsRolloutTimeoutString returns the kubectl --timeout value for mcp-sentinel rollouts.
// Uses MCP_DEPLOYMENT_TIMEOUT (see GetDeploymentTimeout); if unset or non-positive, uses the default 5m.
func analyticsRolloutTimeoutString() string {
	d := GetDeploymentTimeout()
	if d <= 0 {
		d = defaultDeploymentTimeout
	}
	return d.String()
}

// printAnalyticsRolloutDiagnostics prints pods and events to help triage stuck mcp-sentinel rollouts.
func printAnalyticsRolloutDiagnostics(kubectl KubectlRunner) {
	Warn("mcp-sentinel rollouts failed. Namespace snapshot (pods):")
	// #nosec G204 -- fixed namespace for diagnostics.
	_ = kubectl.RunWithOutput([]string{"get", "pods", "-n", defaultAnalyticsNamespace, "-o", "wide"}, os.Stdout, os.Stderr)
	Warn("Recent events in mcp-sentinel (newest last):")
	_ = kubectl.RunWithOutput([]string{"get", "events", "-n", defaultAnalyticsNamespace, "--sort-by", ".lastTimestamp"}, os.Stdout, os.Stderr)
}

func waitForJobCompletionWithKubectl(kubectl KubectlRunner, name, namespace, timeout string) error {
	return kubectl.RunWithOutput([]string{"wait", "--for=condition=complete", "job/" + name, "-n", namespace, "--timeout=" + timeout}, os.Stdout, os.Stderr)
}

func operatorImagePullPolicy(operatorImage string) string {
	if strings.TrimSpace(operatorImage) == testModeOperatorImage {
		return "IfNotPresent"
	}
	return "Always"
}

// operatorEnvVar represents an environment variable for the operator.
type operatorEnvVar struct {
	Name  string
	Value string
}

// operatorEnvOverrides returns the environment variables to set on the operator deployment.
func operatorEnvOverrides(gatewayProxyImage string) []operatorEnvVar {
	var envVars []operatorEnvVar
	image := strings.TrimSpace(gatewayProxyImage)
	if image == "" {
		image = strings.TrimSpace(GetGatewayProxyImageOverride())
	}
	if image != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_GATEWAY_PROXY_IMAGE", Value: image})
	}
	ingestURL := strings.TrimSpace(GetAnalyticsIngestURLOverride())
	if ingestURL == "" {
		ingestURL = defaultAnalyticsIngestURL
	}
	if ingestURL != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_SENTINEL_INGEST_URL", Value: ingestURL})
	}
	registryEndpoint := strings.TrimSpace(GetRegistryEndpoint())
	if registryEndpoint != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_REGISTRY_ENDPOINT", Value: registryEndpoint})
	}
	registryIngressHost := strings.TrimSpace(GetRegistryIngressHost())
	if registryIngressHost != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_REGISTRY_INGRESS_HOST", Value: registryIngressHost})
	}
	if mcpHost := strings.TrimSpace(GetMcpIngressHost()); mcpHost != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_DEFAULT_INGRESS_HOST", Value: mcpHost})
	}
	clusterName := strings.TrimSpace(GetClusterName())
	if clusterName != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_CLUSTER_NAME", Value: clusterName})
	}
	return envVars
}

func applySetupPlanToCLIConfig(plan SetupPlan) {
	if DefaultCLIConfig == nil {
		return
	}
	if !plan.TLSEnabled {
		DefaultCLIConfig.RegistryClusterIssuerName = ""
		return
	}
	if strings.TrimSpace(plan.ACMEmail) != "" {
		DefaultCLIConfig.RegistryClusterIssuerName = ClusterIssuerNameForACME(plan.ACMEStaging)
		return
	}
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		DefaultCLIConfig.RegistryClusterIssuerName = strings.TrimSpace(plan.TLSClusterIssuer)
		return
	}
	DefaultCLIConfig.RegistryClusterIssuerName = certClusterIssuerName
}

// setupTLSWithKubectlAndPlan provisions TLS: Let's Encrypt when plan.ACMEmail is set, an existing
// ClusterIssuer when plan.TLSClusterIssuer is set, otherwise the bundled private CA (mcp-runtime-ca).
func setupTLSWithKubectlAndPlan(kubectl KubectlRunner, logger *zap.Logger, plan SetupPlan) error {
	if strings.TrimSpace(plan.ACMEmail) != "" {
		return setupTLSLetsEncrypt(kubectl, logger, plan)
	}
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		return setupTLSWithExistingClusterIssuer(kubectl, logger, plan)
	}
	return setupTLSPrivateCA(kubectl, logger)
}

func setupTLSLetsEncrypt(kubectl KubectlRunner, logger *zap.Logger, plan SetupPlan) error {
	Info("Configuring TLS with Let's Encrypt (cert-manager HTTP-01)")
	if err := validateACMEHostnameForPublicCA(); err != nil {
		wrappedErr := wrapWithSentinel(ErrTLSSetupFailed, err, err.Error())
		Error("Invalid configuration for Let's Encrypt")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Invalid configuration for Let's Encrypt")
		}
		return wrappedErr
	}
	if plan.InstallCertManager {
		if err := ensureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := wrapWithSentinel(ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			Error("Cert-manager not installed")
			if logger != nil {
				logStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		Info("cert-manager CRDs found")
	}

	Info("Applying Let's Encrypt ClusterIssuer")
	if err := applyLetsEncryptClusterIssuer(kubectl, plan.ACMEmail, plan.ACMEStaging, logger); err != nil {
		return err
	}

	if err := ensureNamespace(NamespaceRegistry); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to create registry namespace")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}

	issuerName := ClusterIssuerNameForACME(plan.ACMEStaging)
	dnsNames := acmeTLSDNSNames()
	Info("Applying Certificate for registry (Let's Encrypt SANs)")
	if err := applyRegistryCertificateForACME(kubectl, dnsNames, issuerName); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": registryCertificateName, "namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to apply Certificate")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, NamespaceRegistry, certTimeout); err != nil {
		err := newWithSentinel(ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		Error("Certificate not ready")
		if logger != nil {
			logStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	Success("Certificate issued successfully")
	return nil
}

// setupTLSWithExistingClusterIssuer issues the registry (and optional mcp SAN) Certificate using a
// ClusterIssuer that already exists in the cluster (internal / enterprise CA).
func setupTLSWithExistingClusterIssuer(kubectl KubectlRunner, logger *zap.Logger, plan SetupPlan) error {
	issuerName := strings.TrimSpace(plan.TLSClusterIssuer)
	Info("Configuring TLS with existing ClusterIssuer: " + issuerName)
	if plan.InstallCertManager {
		if err := ensureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := wrapWithSentinel(ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			Error("Cert-manager not installed")
			if logger != nil {
				logStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		Info("cert-manager CRDs found")
	}

	if err := checkNamedClusterIssuerWithKubectl(kubectl, issuerName); err != nil {
		Error("Cluster issuer not found")
		if logger != nil {
			logStructuredError(logger, err, "Cluster issuer not found")
		}
		return err
	}

	if err := ensureNamespace(NamespaceRegistry); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to create registry namespace")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}

	dnsNames := acmeTLSDNSNames()
	if len(dnsNames) == 0 {
		err := fmt.Errorf("no DNS names resolved for the Certificate; set MCP_PLATFORM_DOMAIN, MCP_REGISTRY_HOST, or MCP_REGISTRY_INGRESS_HOST (and optional MCP_MCP_INGRESS_HOST)")
		wrappedErr := wrapWithSentinel(ErrTLSSetupFailed, err, err.Error())
		Error("Invalid TLS host configuration")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Invalid TLS host configuration")
		}
		return wrappedErr
	}

	Info("Applying Certificate for registry (custom ClusterIssuer)")
	if err := applyRegistryCertificateForACME(kubectl, dnsNames, issuerName); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": registryCertificateName, "namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to apply Certificate")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, NamespaceRegistry, certTimeout); err != nil {
		err := newWithSentinel(ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager and your ClusterIssuer configuration: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		Error("Certificate not ready")
		if logger != nil {
			logStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	Success("Certificate issued successfully")
	return nil
}

// setupTLSPrivateCA uses a pre-created TLS secret mcp-runtime-ca in cert-manager (see config/cert-manager/cluster-issuer.yaml).
func setupTLSPrivateCA(kubectl KubectlRunner, logger *zap.Logger) error {
	Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
		err := wrapWithSentinel(ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true\n  or run setup with --with-tls --acme-email <addr> to install cert-manager automatically")
		Error("Cert-manager not installed")
		if logger != nil {
			logStructuredError(logger, err, "Cert-manager not installed")
		}
		return err
	}
	Info("cert-manager CRDs found")

	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(kubectl); err != nil {
		err := wrapWithSentinel(ErrCASecretNotFound, err, "CA secret 'mcp-runtime-ca' not found in cert-manager namespace. For Let's Encrypt use --acme-email, or create a private CA:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
		Error("CA secret not found")
		if logger != nil {
			logStructuredError(logger, err, "CA secret not found")
		}
		return err
	}
	Info("CA secret found")

	Info("Applying ClusterIssuer")
	if err := applyClusterIssuerWithKubectl(kubectl); err != nil {
		wrappedErr := wrapWithSentinel(ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply ClusterIssuer: %v", err))
		Error("Failed to apply ClusterIssuer")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply ClusterIssuer")
		}
		return wrappedErr
	}

	if err := ensureNamespace(NamespaceRegistry); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to create registry namespace")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}

	Info("Applying Certificate for registry")
	if err := applyRegistryCertificateWithKubectl(kubectl); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": registryCertificateName, "namespace": NamespaceRegistry, "component": "setup"},
		)
		Error("Failed to apply Certificate")
		if logger != nil {
			logStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := GetCertTimeout()
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, NamespaceRegistry, certTimeout); err != nil {
		err := newWithSentinel(ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		Error("Certificate not ready")
		if logger != nil {
			logStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	Success("Certificate issued successfully")
	return nil
}
