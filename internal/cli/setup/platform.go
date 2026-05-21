package setup

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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/certmanager"
	"mcp-runtime/internal/cli/cluster"
	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/registry"
	"mcp-runtime/internal/cli/registry/config"
	"mcp-runtime/internal/cli/registry/ref"
	"mcp-runtime/internal/cli/setup/assetpath"
	"mcp-runtime/internal/cli/setup/ingressmanifest"
	setupplan "mcp-runtime/internal/cli/setup/plan"
	"mcp-runtime/pkg/manifest"
)

const defaultRegistrySecretName = "mcp-runtime-registry-creds" // #nosec G101 -- default secret name, not a credential.
const registryAdminAuthMiddleware = "registry-admin-auth@file"
const testModeOperatorImage = "docker.io/library/mcp-runtime-operator:latest"
const defaultGatewayProxyRepository = "mcp-sentinel-mcp-gateway"
const defaultAnalyticsIngestURL = "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events"
const gatewayOTELExporterOTLPEndpointEnv = "MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT"
const defaultGatewayOTELExporterOTLPEndpoint = "http://otel-collector.mcp-sentinel.svc.cluster.local:4318"
const gatewayProxyDockerfilePath = "services/mcp-gateway/Dockerfile"
const gatewayProxyBuildContext = "."

// pathBasedSentinelIngressNames lists the dev path-based ingresses for the
// mcp-sentinel stack. Public-host installs remove these after applying the
// dedicated platform ingress so platform UI/API routes are not exposed on
// unrelated public hosts such as the MCP gateway host.
var pathBasedSentinelIngressNames = []string{
	"mcp-sentinel-gateway",
	"mcp-sentinel-gateway-observability",
	"mcp-sentinel-gateway-adapter-session",
	"mcp-sentinel-gateway-api",
	"mcp-sentinel-gateway-ingest",
}

const (
	defaultDevUserEmail     = "test@mcpruntime.org"
	defaultDevUserPassword  = "test@123"
	defaultDevAdminEmail    = "admin@mcpruntime.org"
	defaultDevAdminPassword = "admin@123"
)

var setupImageTagResolver = registry.DefaultGitTag

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
		BuildContext: ".",
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
		BuildContext: ".",
	},
	{
		Name:         "ui",
		Repository:   "mcp-sentinel-ui",
		Dockerfile:   "services/ui/Dockerfile",
		BuildContext: ".",
	},
}

type ClusterManagerAPI interface {
	InitCluster(kubeconfig, context string) error
	ConfigureCluster(opts cluster.IngressOptions) error
}

type RegistryManagerAPI interface {
	ShowRegistryInfo() error
	PushInCluster(source, target, helperNS string) error
}

type SetupDeps struct {
	ResolveExternalRegistryConfig   func(*config.ExternalRegistryConfig) (*config.ExternalRegistryConfig, error)
	ClusterManager                  ClusterManagerAPI
	RegistryManager                 RegistryManagerAPI
	LoginRegistry                   func(logger *zap.Logger, registryURL, username, password string) error
	DeployRegistry                  func(logger *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error
	WaitForDeploymentAvailable      func(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error
	PrintDeploymentDiagnostics      func(deploy, namespace, selector string)
	SetupTLS                        func(logger *zap.Logger, plan setupplan.Plan) error
	BuildOperatorImage              func(image string) error
	PushOperatorImage               func(image string) error
	BuildGatewayProxyImage          func(image string) error
	PushGatewayProxyImage           func(image string) error
	BuildAnalyticsImage             func(image, dockerfilePath, buildContext string) error
	PushAnalyticsImage              func(image string) error
	EnsureNamespace                 func(namespace string) error
	EnsureCatalogNamespace          func(namespace string, labels map[string]string) error
	ResolvePlatformRegistryURL      func(logger *zap.Logger) string
	PushOperatorImageToInternal     func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	PushGatewayProxyImageToInternal func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	PushAnalyticsImageToInternal    func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	DeployOperatorManifests         func(logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error
	DeployAnalyticsManifests        func(logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string) error
	DisableRegistryIngressAuth      func() error
	EnableRegistryIngressAuth       func() error
	ConfigureProvisionedRegistryEnv func(ext *config.ExternalRegistryConfig, secretName string) error
	RestartDeployment               func(name, namespace string) error
	CheckCRDInstalled               func(name string) error
	GetDeploymentTimeout            func() time.Duration
	GetRegistryPort                 func() int
	OperatorImageFor                func(ext *config.ExternalRegistryConfig) string
	GatewayProxyImageFor            func(ext *config.ExternalRegistryConfig) string
}

func (d SetupDeps) withDefaults(logger *zap.Logger) SetupDeps {
	if d.ResolveExternalRegistryConfig == nil {
		d.ResolveExternalRegistryConfig = registry.ResolveExternalRegistryConfig
	}
	if d.ClusterManager == nil {
		panic("cli: SetupDeps.ClusterManager must be set; pass it via SetupPlatform")
	}
	if d.RegistryManager == nil {
		d.RegistryManager = registry.DefaultRegistryManager(logger)
	}
	if d.LoginRegistry == nil {
		d.LoginRegistry = func(l *zap.Logger, registryURL, username, password string) error {
			return registry.DefaultRegistryManager(l).LoginRegistry(registryURL, username, password)
		}
	}
	if d.DeployRegistry == nil {
		d.DeployRegistry = registry.DeployRegistry
	}
	if d.WaitForDeploymentAvailable == nil {
		d.WaitForDeploymentAvailable = waitForDeploymentAvailable
	}
	if d.PrintDeploymentDiagnostics == nil {
		d.PrintDeploymentDiagnostics = printDeploymentDiagnostics
	}
	if d.SetupTLS == nil {
		d.SetupTLS = func(l *zap.Logger, p setupplan.Plan) error {
			return setupTLSWithKubectlAndPlan(core.DefaultKubectlClient(), l, p)
		}
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
		d.EnsureNamespace = func(namespace string) error {
			return kube.EnsureNamespace(core.DefaultKubectlClient().CommandArgs, namespace)
		}
	}
	if d.EnsureCatalogNamespace == nil {
		d.EnsureCatalogNamespace = func(namespace string, labels map[string]string) error {
			return kube.EnsureNamespaceWithLabels(core.DefaultKubectlClient().CommandArgs, namespace, labels)
		}
	}
	if d.ResolvePlatformRegistryURL == nil {
		d.ResolvePlatformRegistryURL = registry.ResolveInternalPlatformRegistryURL
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
	if d.DisableRegistryIngressAuth == nil {
		d.DisableRegistryIngressAuth = disableRegistryIngressAuth
	}
	if d.EnableRegistryIngressAuth == nil {
		d.EnableRegistryIngressAuth = enableRegistryIngressAuth
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
		d.GetDeploymentTimeout = core.GetDeploymentTimeout
	}
	if d.GetRegistryPort == nil {
		d.GetRegistryPort = core.GetRegistryPort
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
func ValidateTLSSetupCLIFlags(
	tlsEnabled bool,
	acmeEmailResolved, tlsCIResolved string,
	acmeStagingResolved, skipCertManagerInstall bool,
) error {
	if acmeEmailResolved != "" && tlsCIResolved != "" {
		return core.NewWithSentinel(core.ErrFieldRequired, "use either --acme-email (or MCP_ACME_EMAIL) for public Let's Encrypt, or --tls-cluster-issuer (or MCP_TLS_CLUSTER_ISSUER) for an existing internal ClusterIssuer, not both")
	}
	if !tlsEnabled && (tlsCIResolved != "" || acmeEmailResolved != "" || acmeStagingResolved || skipCertManagerInstall) {
		return core.NewWithSentinel(core.ErrFieldRequired, "--with-tls is required when using --acme-email, --tls-cluster-issuer, --acme-staging, --skip-cert-manager-install, or related environment variables (MCP_ACME_EMAIL, MCP_ACME_STAGING, MCP_TLS_CLUSTER_ISSUER)")
	}
	return nil
}

// buildOperatorArgs constructs operator command-line arguments from flags.
// Only includes flags that were explicitly set.
func BuildOperatorArgs(metricsAddr, probeAddr string, leaderElect, leaderElectChanged bool) []string {
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

func ValidateStorageMode(mode string) error {
	switch mode {
	case setupplan.StorageModeDynamic, setupplan.StorageModeHostpath:
		return nil
	default:
		return core.WrapWithSentinel(core.ErrFieldRequired, fmt.Errorf("invalid storage mode %q", mode), "invalid --storage-mode; expected dynamic or hostpath")
	}
}

func ValidateRegistryMode(mode string) error {
	if _, ok := setupplan.NormalizeRegistryMode(mode); ok {
		return nil
	}
	return core.WrapWithSentinel(
		core.ErrFieldRequired,
		fmt.Errorf("invalid registry mode %q", mode),
		"invalid --registry-mode; expected auto, bundled-http, bundled-https, or external",
	)
}

func ValidateRegistryTLSMode(mode string, tlsEnabled bool, acmeEmail string) error {
	normalized, ok := setupplan.NormalizeRegistryMode(mode)
	if !ok {
		return nil
	}
	if normalized == setupplan.RegistryModeBundledHTTPS && !tlsEnabled {
		return core.NewWithSentinel(
			core.ErrFieldRequired,
			"--registry-mode bundled-https requires --with-tls so setup can provision registry-tls for the registry pod",
		)
	}
	return nil
}

func ValidatePlatformMode(mode string) error {
	if _, ok := setupplan.NormalizePlatformMode(mode); ok {
		return nil
	}
	return core.WrapWithSentinel(core.ErrFieldRequired, fmt.Errorf("invalid platform mode %q", mode), "invalid --platform-mode; expected tenant, org, or public")
}

func ValidatePublicPlatformAuthEnv(platformMode string, tlsEnabled, testMode bool) error {
	return ValidatePublicPlatformAuthConfig(platformMode, tlsEnabled, testMode, nil)
}

func ValidatePublicPlatformAuthConfig(platformMode string, tlsEnabled, testMode bool, existingData map[string]string) error {
	if !publicPlatformAuthConfigRequired(platformMode, tlsEnabled, testMode) {
		return nil
	}
	if publicBrowserLoginConfigConfigured(existingData) {
		return nil
	}
	return core.NewWithSentinel(
		core.ErrFieldRequired,
		"--platform-mode public with --with-tls requires browser login configuration: set GOOGLE_CLIENT_ID or MCP_GOOGLE_CLIENT_ID for Google sign-in, set OIDC_ISSUER, OIDC_AUDIENCE, and OIDC_JWKS_URL for another provider, or rerun against a cluster whose mcp-sentinel-config already contains those values",
	)
}

func publicPlatformAuthConfigRequired(platformMode string, tlsEnabled, testMode bool) bool {
	mode, ok := setupplan.NormalizePlatformMode(platformMode)
	return ok && mode == setupplan.PlatformModePublic && tlsEnabled && !testMode
}

func publicBrowserLoginConfigConfigured(existingData map[string]string) bool {
	if publicAuthConfigValue(existingData, "GOOGLE_CLIENT_ID") != "" {
		return true
	}
	oidcIssuer := publicAuthConfigValue(existingData, "OIDC_ISSUER")
	oidcAudience := publicAuthConfigValue(existingData, "OIDC_AUDIENCE")
	oidcJWKSURL := publicAuthConfigValue(existingData, "OIDC_JWKS_URL")
	return oidcIssuer != "" && oidcAudience != "" && oidcJWKSURL != ""
}

func publicAuthConfigValue(existingData map[string]string, key string) string {
	if envValue := setupAnalyticsConfigEnvValue(key); envValue != "" {
		return envValue
	}
	return strings.TrimSpace(existingData[key])
}

func SetupPlatform(logger *zap.Logger, plan setupplan.Plan, clusterMgr ClusterManagerAPI) error {
	return setupPlatformWithDeps(logger, plan, SetupDeps{ClusterManager: clusterMgr}.withDefaults(logger))
}

func buildOperatorArgs(metricsAddr, probeAddr string, leaderElect, leaderElectChanged bool) []string {
	return BuildOperatorArgs(metricsAddr, probeAddr, leaderElect, leaderElectChanged)
}

func setupPlatformWithDeps(logger *zap.Logger, plan setupplan.Plan, deps SetupDeps) error {
	deps = deps.withDefaults(logger)
	core.Section("MCP Runtime Setup")

	// Propagate test mode to build helpers so they can choose faster/safer build paths.
	if plan.TestMode {
		_ = os.Setenv("MCP_RUNTIME_TEST_MODE", "1")
	} else {
		_ = os.Unsetenv("MCP_RUNTIME_TEST_MODE")
	}
	_ = os.Setenv("MCP_PLATFORM_MODE", plan.PlatformMode)

	extRegistry, usingExternalRegistry, registrySecretName, err := resolveRegistrySetup(logger, plan, deps)
	if err != nil {
		core.LogStructuredError(logger, err, "Invalid registry setup configuration")
		return err
	}
	existingPublicAuthConfig, err := existingPublicAuthConfigForSetup(plan)
	if err != nil {
		core.LogStructuredError(logger, err, "Invalid public platform auth configuration")
		return err
	}
	if err := validateNonTestSetupWithAuthConfig(plan, extRegistry, usingExternalRegistry, existingPublicAuthConfig); err != nil {
		core.LogStructuredError(logger, err, "Invalid non-test setup configuration")
		return err
	}
	applySetupPlanToCLIConfig(plan)
	for _, warning := range setupWarnings(plan, extRegistry, usingExternalRegistry) {
		core.Warn(warning)
	}
	ctx := &SetupContext{
		Plan:                  plan,
		ExternalRegistry:      extRegistry,
		UsingExternalRegistry: usingExternalRegistry,
		RegistrySecretName:    registrySecretName,
	}
	// Re-enable registry ingress auth on every exit path. The setup pipeline
	// temporarily strips the `registry-admin-auth@file` middleware so the
	// internal in-cluster image push helper can talk to the registry while
	// the auth-resolver is still being wired. If we put the re-enable as a
	// pipeline step it would be skipped whenever an earlier step fails,
	// leaving the public registry without auth.
	defer func() {
		if !ctx.RegistryAuthStaged {
			return
		}
		if err := deps.EnableRegistryIngressAuth(); err != nil {
			core.Error("Failed to re-enable registry ingress auth after setup")
			core.LogStructuredError(logger, err, "Re-enable registry ingress auth")
			return
		}
		ctx.RegistryAuthStaged = false
	}()
	if err := runSetupSteps(logger, deps, ctx, buildSetupSteps(ctx)); err != nil {
		return err
	}

	core.Success("Platform setup complete")
	fmt.Println(core.Green("\nPlatform is ready. Use 'mcp-runtime status' to check everything."))
	printPlatformEntrypoints(plan.TLSEnabled)
	return nil
}

func setupImageTag() string {
	if os.Getenv("MCP_RUNTIME_TEST_MODE") == "1" {
		return "latest"
	}
	return setupImageTagResolver()
}

func setupClusterSteps(logger *zap.Logger, kubeconfig, context string, ingressOpts cluster.IngressOptions, deps SetupDeps) error {
	// Step 1: Initialize cluster
	core.Step("Step 1: Initialize cluster")
	core.Info("Installing CRD")
	if err := deps.ClusterManager.InitCluster(kubeconfig, context); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterInitFailed, err, fmt.Sprintf("failed to initialize cluster: %v", err))
		core.Error("Cluster initialization failed")
		core.LogStructuredError(logger, wrappedErr, "Cluster initialization failed")
		return wrappedErr
	}
	core.Info("Cluster initialized")

	// Step 2: Configure cluster
	core.Step("Step 2: Configure cluster")
	core.Info("Checking ingress controller")
	if err := deps.ClusterManager.ConfigureCluster(ingressOpts); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterConfigFailed, err, fmt.Sprintf("cluster configuration failed: %v", err))
		core.Error("Cluster configuration failed")
		core.LogStructuredError(logger, wrappedErr, "Cluster configuration failed")
		return wrappedErr
	}
	core.Info("Cluster configuration complete")
	return nil
}

func setupTLSStep(logger *zap.Logger, plan setupplan.Plan, deps SetupDeps) error {
	// Step 3: Configure TLS (if enabled)
	core.Step("Step 3: Configure TLS")
	if !plan.TLSEnabled {
		core.Info("Skipped (TLS disabled, use --with-tls to enable)")
		return nil
	}
	if err := deps.SetupTLS(logger, plan); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, fmt.Sprintf("TLS setup failed: %v", err))
		core.Error("TLS setup failed")
		core.LogStructuredError(logger, wrappedErr, "TLS setup failed")
		return wrappedErr
	}
	core.Success("TLS configured successfully")
	return nil
}

// catalogNamespaceLabels returns the labels the platform API expects to find on
// a shared catalog namespace. Keeping these aligned with EnsureCatalogNamespace
// in services/api/internal/runtimeapi/deployments.go lets the runtime-side
// ensure call degrade to an idempotent patch instead of a create, which is
// what allows non-admin users to publish into the catalog without giving the
// API ServiceAccount cluster-wide namespace-create RBAC.
func catalogNamespaceLabels(platformMode string) map[string]string {
	return map[string]string{
		"platform.mcpruntime.org/managed":    "true",
		"mcpruntime.org/scope":               platformMode,
		"pod-security.kubernetes.io/enforce": "restricted",
		"pod-security.kubernetes.io/audit":   "restricted",
		"pod-security.kubernetes.io/warn":    "restricted",
		core.LabelManagedBy:                  core.LabelManagedByValue,
	}
}

func setupCatalogNamespaceStep(logger *zap.Logger, plan setupplan.Plan, deps SetupDeps) error {
	namespace := setupplan.CatalogNamespaceForPlatformMode(plan.PlatformMode)
	if namespace == "" {
		// tenant mode has no shared catalog namespace.
		return nil
	}
	core.Step(fmt.Sprintf("Provisioning %s catalog namespace %q", plan.PlatformMode, namespace))
	if err := deps.EnsureCatalogNamespace(namespace, catalogNamespaceLabels(plan.PlatformMode)); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrSetupStepFailed,
			err,
			fmt.Sprintf("ensure catalog namespace %q failed: %v", namespace, err),
			map[string]any{"namespace": namespace, "platform_mode": plan.PlatformMode, "component": "setup"},
		)
		core.Error("Catalog namespace provisioning failed")
		core.LogStructuredError(logger, wrappedErr, "Catalog namespace provisioning failed")
		return wrappedErr
	}
	core.Success(fmt.Sprintf("Catalog namespace %q ready", namespace))
	return nil
}

func setupRegistryStep(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry bool, registryType, registryStorageSize, registryManifest, registryMode string, tlsEnabled bool, deps SetupDeps) error {
	// Step 4: Deploy internal container registry
	core.Step("Step 4: Configure registry")
	if usingExternalRegistry {
		core.Info(fmt.Sprintf("Using external registry: %s", extRegistry.URL))
		if extRegistry.Username != "" || extRegistry.Password != "" {
			core.Info("Logging into external registry")
			if err := deps.LoginRegistry(logger, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
				wrappedErr := core.WrapWithSentinel(core.ErrRegistryLoginFailed, err, fmt.Sprintf("failed to login to registry %q: %v", extRegistry.URL, err))
				core.Error("Registry login failed")
				core.LogStructuredError(logger, wrappedErr, "Registry login failed")
				return wrappedErr
			}
		}
		return nil
	}

	core.Info(fmt.Sprintf("Type: %s", registryType))
	if registryMode == setupplan.RegistryModeBundledHTTPS {
		core.Info("TLS: enabled for registry pod and ingress (bundled HTTPS mode)")
	} else if tlsEnabled {
		core.Info("TLS: enabled for registry ingress; registry pod remains HTTP for internal pulls")
	} else {
		core.Info("TLS: disabled (dev HTTP mode)")
	}
	if err := deps.DeployRegistry(logger, "registry", deps.GetRegistryPort(), registryType, registryStorageSize, registryManifest); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrDeployRegistryFailed,
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
		core.Error("Registry deployment failed")
		core.LogStructuredError(logger, wrappedErr, "Registry deployment failed")
		return wrappedErr
	}

	core.Info("Waiting for registry to be ready...")
	if err := deps.WaitForDeploymentAvailable(logger, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
		regCtx := map[string]any{
			"deployment": "registry",
			"namespace":  "registry",
			"selector":   "app=registry",
			"component":  "registry",
		}
		mergeDeploymentDebugDiagnosticsIfNeeded(core.DefaultKubectlClient(), regCtx, "registry", "registry", "app=registry")
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrRegistryNotReady,
			err,
			fmt.Sprintf("registry deployment not ready in namespace %q: %v", "registry", err),
			regCtx,
		)
		core.Error("Registry failed to become ready")
		core.LogStructuredError(logger, wrappedErr, "Registry failed to become ready")
		return wrappedErr
	}

	if err := deps.RegistryManager.ShowRegistryInfo(); err != nil {
		core.Warn(fmt.Sprintf("Failed to show registry info: %v", err))
	}
	return nil
}

func prepareDeploymentImages(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode, parallelBuilds bool, deps SetupDeps) (string, string, error) {
	core.Step("Step 5: Publish runtime images")

	if parallelBuilds {
		core.Info("Parallel image publishing enabled for setup runtime images")

		parallelDeps, err := prepareParallelImagePublishDeps(logger, usingExternalRegistry, deps, "setup")
		if err != nil {
			core.Error("Failed to prepare internal registry image publishing")
			core.LogStructuredError(logger, err, "Failed to prepare internal registry image publishing")
			return "", "", err
		}

		type imageResult struct {
			kind  string
			image string
			err   error
		}
		results := make(chan imageResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			image, err := prepareOperatorImage(logger, extRegistry, usingExternalRegistry, testMode, parallelDeps)
			results <- imageResult{kind: "operator", image: image, err: err}
		}()
		go func() {
			defer wg.Done()
			image, err := prepareGatewayProxyImage(logger, extRegistry, usingExternalRegistry, testMode, parallelDeps)
			results <- imageResult{kind: "gateway", image: image, err: err}
		}()

		wg.Wait()
		close(results)

		var operatorImage, gatewayProxyImage string
		for result := range results {
			if result.err != nil {
				return "", "", result.err
			}
			switch result.kind {
			case "operator":
				operatorImage = result.image
			case "gateway":
				gatewayProxyImage = result.image
			}
		}
		return operatorImage, gatewayProxyImage, nil
	}

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

func prepareOperatorImage(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, error) {
	operatorImage := deps.OperatorImageFor(extRegistry)
	core.Info(fmt.Sprintf("Operator image: %s", operatorImage))

	core.Info("Building operator image")
	if err := deps.BuildOperatorImage(operatorImage); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrOperatorImageBuildFailed,
			err,
			fmt.Sprintf("operator image build failed for image %q: %v", operatorImage, err),
			map[string]any{
				"image":     operatorImage,
				"component": "operator",
			},
		)
		core.Error("Operator image build failed")
		core.LogStructuredError(logger, wrappedErr, "Operator image build failed")
		return "", wrappedErr
	}

	if usingExternalRegistry {
		if testMode {
			core.Info("Test mode: pushing operator image to external registry")
		} else {
			core.Info("Pushing operator image to external registry")
		}
		if err := deps.PushOperatorImage(operatorImage); err != nil {
			core.Warn(fmt.Sprintf("Could not push image to external registry: %v", err))
		}
		return operatorImage, nil
	}

	core.Info("Pushing operator image to internal registry")
	internalRegistryURL := deps.ResolvePlatformRegistryURL(logger)
	_, operatorTag := ref.SplitImage(operatorImage)
	if operatorTag == "" {
		operatorTag = setupImageTag()
	}
	internalOperatorImage := fmt.Sprintf("%s/mcp-runtime-operator:%s", internalRegistryURL, operatorTag)

	if err := ensureRegistryNamespaceForImagePush(deps, "setup"); err != nil {
		core.Error("Failed to ensure registry namespace")
		core.LogStructuredError(logger, err, "Failed to ensure registry namespace")
		return "", err
	}

	if err := deps.PushOperatorImageToInternal(logger, operatorImage, internalOperatorImage, "registry"); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushOperatorImageInternalFailed,
			err,
			fmt.Sprintf("failed to push operator image %q to internal registry %q: %v", operatorImage, internalOperatorImage, err),
			map[string]any{
				"source_image": operatorImage,
				"target_image": internalOperatorImage,
				"namespace":    "registry",
				"component":    "operator",
			},
		)
		core.Error("Failed to push operator image to internal registry")
		core.LogStructuredError(logger, wrappedErr, "Failed to push operator image to internal registry")
		return "", wrappedErr
	}
	core.Info(fmt.Sprintf("Using internal registry image: %s", internalOperatorImage))
	return internalOperatorImage, nil
}

func prepareGatewayProxyImage(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, error) {
	gatewayProxyImage := deps.GatewayProxyImageFor(extRegistry)
	core.Info(fmt.Sprintf("Gateway proxy image: %s", gatewayProxyImage))

	core.Info("Building gateway proxy image")
	if err := deps.BuildGatewayProxyImage(gatewayProxyImage); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrGatewayProxyImageBuildFailed,
			err,
			fmt.Sprintf("gateway proxy image build failed for image %q: %v", gatewayProxyImage, err),
			map[string]any{
				"image":     gatewayProxyImage,
				"component": "gateway-proxy",
			},
		)
		core.Error("Gateway proxy image build failed")
		core.LogStructuredError(logger, wrappedErr, "Gateway proxy image build failed")
		return "", wrappedErr
	}

	if usingExternalRegistry {
		if testMode {
			core.Info("Test mode: pushing gateway proxy image to external registry")
		} else {
			core.Info("Pushing gateway proxy image to external registry")
		}
		if err := deps.PushGatewayProxyImage(gatewayProxyImage); err != nil {
			core.Warn(fmt.Sprintf("Could not push gateway proxy image to external registry: %v", err))
		}
		return gatewayProxyImage, nil
	}

	core.Info("Pushing gateway proxy image to internal registry")
	internalRegistryURL := deps.ResolvePlatformRegistryURL(logger)
	_, gatewayTag := ref.SplitImage(gatewayProxyImage)
	if gatewayTag == "" {
		gatewayTag = setupImageTag()
	}
	internalGatewayProxyImage := fmt.Sprintf("%s/%s:%s", internalRegistryURL, defaultGatewayProxyRepository, gatewayTag)

	if err := ensureRegistryNamespaceForImagePush(deps, "setup"); err != nil {
		core.Error("Failed to ensure registry namespace")
		core.LogStructuredError(logger, err, "Failed to ensure registry namespace")
		return "", err
	}

	if err := deps.PushGatewayProxyImageToInternal(logger, gatewayProxyImage, internalGatewayProxyImage, "registry"); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushGatewayProxyImageInternalFailed,
			err,
			fmt.Sprintf("failed to push gateway proxy image %q to internal registry %q: %v", gatewayProxyImage, internalGatewayProxyImage, err),
			map[string]any{
				"source_image": gatewayProxyImage,
				"target_image": internalGatewayProxyImage,
				"namespace":    "registry",
				"component":    "gateway-proxy",
			},
		)
		core.Error("Failed to push gateway proxy image to internal registry")
		core.LogStructuredError(logger, wrappedErr, "Failed to push gateway proxy image to internal registry")
		return "", wrappedErr
	}

	core.Info(fmt.Sprintf("Using internal registry gateway proxy image: %s", internalGatewayProxyImage))
	return internalGatewayProxyImage, nil
}

func prepareAnalyticsImages(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode, parallelBuilds bool, deps SetupDeps) (AnalyticsImageSet, error) {
	core.Step("Step 5a: Publish analytics images")

	images := AnalyticsImageSet{
		Ingest:    analyticsImageFor(extRegistry, analyticsComponents[0].Repository),
		API:       analyticsImageFor(extRegistry, analyticsComponents[1].Repository),
		Processor: analyticsImageFor(extRegistry, analyticsComponents[2].Repository),
		UI:        analyticsImageFor(extRegistry, analyticsComponents[3].Repository),
	}

	if parallelBuilds {
		core.Info("Parallel image publishing enabled for setup analytics images")
		return prepareAnalyticsImagesParallel(logger, extRegistry, usingExternalRegistry, testMode, deps, images)
	}

	for _, component := range analyticsComponents {
		image, err := buildAndPublishAnalyticsComponent(logger, extRegistry, usingExternalRegistry, testMode, deps, component)
		if err != nil {
			return AnalyticsImageSet{}, err
		}
		assignAnalyticsImage(&images, component.Repository, image)
	}

	return images, nil
}

func prepareAnalyticsImagesParallel(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps, images AnalyticsImageSet) (AnalyticsImageSet, error) {
	parallelDeps, err := prepareParallelImagePublishDeps(logger, usingExternalRegistry, deps, "analytics")
	if err != nil {
		return AnalyticsImageSet{}, err
	}

	type analyticsResult struct {
		repository string
		image      string
		err        error
	}

	results := make(chan analyticsResult, len(analyticsComponents))
	var wg sync.WaitGroup
	wg.Add(len(analyticsComponents))

	for _, component := range analyticsComponents {
		component := component
		go func() {
			defer wg.Done()
			finalImage, err := buildAndPublishAnalyticsComponent(logger, extRegistry, usingExternalRegistry, testMode, parallelDeps, component)
			results <- analyticsResult{repository: component.Repository, image: finalImage, err: err}
		}()
	}

	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			return AnalyticsImageSet{}, result.err
		}
		assignAnalyticsImage(&images, result.repository, result.image)
	}

	return images, nil
}

func buildAndPublishAnalyticsComponent(logger *zap.Logger, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps, component analyticsComponent) (string, error) {
	image := analyticsImageFor(extRegistry, component.Repository)
	if testMode {
		core.Info(fmt.Sprintf("Test mode: building analytics %s image: %s", component.Name, image))
	} else {
		core.Info(fmt.Sprintf("Building analytics %s image: %s", component.Name, image))
	}
	if err := deps.BuildAnalyticsImage(image, component.Dockerfile, component.BuildContext); err != nil {
		return "", core.WrapWithSentinelAndContext(
			core.ErrBuildImageFailed,
			err,
			fmt.Sprintf("failed to build analytics %s image %q: %v", component.Name, image, err),
			map[string]any{"image": image, "component": component.Name},
		)
	}
	if usingExternalRegistry {
		if testMode {
			core.Info(fmt.Sprintf("Test mode: pushing analytics %s image to external registry", component.Name))
		} else {
			core.Info(fmt.Sprintf("Pushing analytics %s image to external registry", component.Name))
		}
		if err := deps.PushAnalyticsImage(image); err != nil {
			core.Warn(fmt.Sprintf("Could not push analytics %s image to external registry: %v", component.Name, err))
		}
		return image, nil
	}

	if testMode {
		core.Info(fmt.Sprintf("Test mode: pushing analytics %s image to internal registry", component.Name))
	} else {
		core.Info(fmt.Sprintf("Pushing analytics %s image to internal registry", component.Name))
	}
	if err := ensureRegistryNamespaceForImagePush(deps, component.Name); err != nil {
		return "", err
	}
	internalRegistryURL := deps.ResolvePlatformRegistryURL(logger)
	_, imageTag := ref.SplitImage(image)
	if imageTag == "" {
		imageTag = setupImageTag()
	}
	internalImage := fmt.Sprintf("%s/%s:%s", internalRegistryURL, component.Repository, imageTag)
	if err := deps.PushAnalyticsImageToInternal(logger, image, internalImage, "registry"); err != nil {
		return "", core.WrapWithSentinelAndContext(
			core.ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push analytics %s image %q to internal registry %q: %v", component.Name, image, internalImage, err),
			map[string]any{"source_image": image, "target_image": internalImage, "component": component.Name},
		)
	}
	return internalImage, nil
}

func prepareParallelImagePublishDeps(logger *zap.Logger, usingExternalRegistry bool, deps SetupDeps, component string) (SetupDeps, error) {
	if usingExternalRegistry {
		return deps, nil
	}
	if err := ensureRegistryNamespaceForImagePush(deps, component); err != nil {
		return SetupDeps{}, err
	}
	internalRegistryURL := deps.ResolvePlatformRegistryURL(logger)
	parallelDeps := deps
	parallelDeps.EnsureNamespace = func(string) error { return nil }
	parallelDeps.ResolvePlatformRegistryURL = func(*zap.Logger) string { return internalRegistryURL }
	return parallelDeps, nil
}

func ensureRegistryNamespaceForImagePush(deps SetupDeps, component string) error {
	if err := deps.EnsureNamespace("registry"); err != nil {
		return core.WrapWithSentinelAndContext(
			core.ErrEnsureRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure registry namespace: %v", err),
			map[string]any{"namespace": "registry", "component": component},
		)
	}
	return nil
}

func assignAnalyticsImage(images *AnalyticsImageSet, repository, image string) {
	switch repository {
	case "mcp-sentinel-ingest":
		images.Ingest = image
	case "mcp-sentinel-api":
		images.API = image
	case "mcp-sentinel-processor":
		images.Processor = image
	case "mcp-sentinel-ui":
		images.UI = image
	}
}

func deployAnalyticsStepCmd(logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string, deps SetupDeps) error {
	core.Info("Deploying mcp-sentinel manifests")
	if err := deps.DeployAnalyticsManifests(logger, images, storageMode, platformMode); err != nil {
		core.Error("Analytics deployment failed")
		core.LogStructuredError(logger, err, "Analytics deployment failed")
		return err
	}
	return nil
}

func deployOperatorStep(logger *zap.Logger, operatorImage, gatewayProxyImage string, extRegistry *config.ExternalRegistryConfig, registrySecretName string, usingExternalRegistry bool, operatorArgs []string, deps SetupDeps) error {
	core.Info("Deploying operator manifests")
	if err := deps.DeployOperatorManifests(logger, operatorImage, gatewayProxyImage, operatorArgs); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrOperatorDeploymentFailed,
			err,
			fmt.Sprintf("operator deployment failed for image %q: %v", operatorImage, err),
			map[string]any{
				"image":     operatorImage,
				"namespace": core.NamespaceMCPRuntime,
				"component": "operator",
			},
		)
		core.Error("Operator deployment failed")
		core.LogStructuredError(logger, wrappedErr, "Operator deployment failed")
		return wrappedErr
	}

	if usingExternalRegistry {
		if err := deps.ConfigureProvisionedRegistryEnv(extRegistry, registrySecretName); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrConfigureExternalRegistryEnvFailed,
				err,
				fmt.Sprintf("failed to configure external registry env on operator (registry: %q, secret: %q): %v", extRegistry.URL, registrySecretName, err),
				map[string]any{
					"registry_url": extRegistry.URL,
					"secret_name":  registrySecretName,
					"namespace":    core.NamespaceMCPRuntime,
					"component":    "operator",
				},
			)
			core.Error("Failed to configure external registry environment")
			core.LogStructuredError(logger, wrappedErr, "Failed to configure external registry environment")
			return wrappedErr
		}
	}

	if err := deps.RestartDeployment("mcp-runtime-operator-controller-manager", "mcp-runtime"); err != nil {
		if usingExternalRegistry {
			wrappedErr := core.WrapWithSentinel(core.ErrRestartOperatorDeploymentFailed, err, fmt.Sprintf("failed to restart operator deployment after registry env update: %v", err))
			core.Error("Failed to restart operator deployment")
			core.LogStructuredError(logger, wrappedErr, "Failed to restart operator deployment")
			return wrappedErr
		}
		core.Warn(fmt.Sprintf("Could not restart operator deployment: %v", err))
	}
	return nil
}

func verifySetup(logger *zap.Logger, usingExternalRegistry bool, deps SetupDeps) error {
	core.Step("Step 6: Verify platform components")

	if usingExternalRegistry {
		core.Info("Skipping internal registry availability check (using external registry)")
	} else {
		core.Info("Waiting for registry deployment to be available")
		if err := deps.WaitForDeploymentAvailable(logger, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
			deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
			regCtx := map[string]any{
				"deployment": "registry",
				"namespace":  "registry",
				"selector":   "app=registry",
				"component":  "registry",
			}
			mergeDeploymentDebugDiagnosticsIfNeeded(core.DefaultKubectlClient(), regCtx, "registry", "registry", "app=registry")
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrRegistryNotReady,
				err,
				fmt.Sprintf("registry not ready: %v", err),
				regCtx,
			)
			core.Error("Registry not ready")
			core.LogStructuredError(logger, wrappedErr, "Registry not ready")
			return wrappedErr
		}
	}

	core.Info("Waiting for operator deployment to be available")
	if err := deps.WaitForDeploymentAvailable(logger, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		opCtx := map[string]any{
			"deployment": "mcp-runtime-operator-controller-manager",
			"namespace":  "mcp-runtime",
			"selector":   "control-plane=controller-manager",
			"component":  "operator",
		}
		mergeDeploymentDebugDiagnosticsIfNeeded(core.DefaultKubectlClient(), opCtx, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrOperatorNotReady,
			err,
			fmt.Sprintf("operator not ready: %v", err),
			opCtx,
		)
		core.Error("Operator not ready")
		core.LogStructuredError(logger, wrappedErr, "Operator not ready")
		return wrappedErr
	}

	core.Info("Checking MCPServer CRD presence")
	if err := deps.CheckCRDInstalled("mcpservers.mcpruntime.org"); err != nil {
		crdName := "mcpservers.mcpruntime.org"
		crdCtx := map[string]any{"crd": crdName, "component": "crd-check"}
		mergeCRDCheckDebugDiagnosticsIfNeeded(core.DefaultKubectlClient(), crdCtx, crdName)
		wrappedErr := core.WrapWithSentinelAndContext(core.ErrCRDCheckFailed, err, fmt.Sprintf("CRD check failed: %v", err), crdCtx)
		core.Error("CRD check failed")
		core.LogStructuredError(logger, wrappedErr, "CRD check failed")
		return wrappedErr
	}

	core.Success("Verification complete")
	return nil
}

func getOperatorImage(ext *config.ExternalRegistryConfig) string {
	tag := setupImageTag()

	// Check for explicit override first
	if override := core.GetOperatorImageOverride(); override != "" {
		return override
	}

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/mcp-runtime-operator:" + tag
	}
	return fmt.Sprintf("%s/mcp-runtime-operator:%s", registry.ResolveInternalPlatformRegistryURL(nil), tag)
}

func getGatewayProxyImage(ext *config.ExternalRegistryConfig) string {
	tag := setupImageTag()

	if override := core.GetGatewayProxyImageOverride(); override != "" {
		return override
	}

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/" + defaultGatewayProxyRepository + ":" + tag
	}
	return fmt.Sprintf("%s/%s:%s", registry.ResolveInternalPlatformRegistryURL(nil), defaultGatewayProxyRepository, tag)
}

func analyticsImageFor(ext *config.ExternalRegistryConfig, repository string) string {
	tag := setupImageTag()

	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/" + repository + ":" + tag
	}
	return fmt.Sprintf("%s/%s:%s", registry.ResolveInternalPlatformRegistryURL(nil), repository, tag)
}

func shouldStageRegistryIngressAuth(usingExternalRegistry bool) bool {
	if usingExternalRegistry {
		return false
	}
	host := strings.TrimSpace(core.GetRegistryIngressHost())
	if host == "" {
		return false
	}
	return !isDevRegistryURL(host)
}

func disableRegistryIngressAuth() error {
	return disableRegistryIngressAuthWithKubectl(core.DefaultKubectlClient())
}

func enableRegistryIngressAuth() error {
	return enableRegistryIngressAuthWithKubectl(core.DefaultKubectlClient())
}

func disableRegistryIngressAuthWithKubectl(kubectl core.KubectlRunner) error {
	// Capture kubectl output so the success path stays quiet (the annotation
	// may already be absent, which kubectl reports loudly) but surface the
	// captured stderr when an unexpected failure happens.
	var stdout, stderr bytes.Buffer
	err := kubectl.RunWithOutput([]string{
		"annotate", "ingress", "registry",
		"-n", "registry",
		"traefik.ingress.kubernetes.io/router.middlewares-",
	}, &stdout, &stderr)
	if err == nil {
		return nil
	}
	combined := strings.ToLower(err.Error() + " " + stderr.String())
	// "not found" covers a missing ingress; "at least one annotation update is required"
	// covers a missing annotation key — both mean there is nothing to disable.
	if strings.Contains(combined, "not found") || strings.Contains(combined, "at least one annotation update is required") {
		return nil
	}
	if stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return err
}

func enableRegistryIngressAuthWithKubectl(kubectl core.KubectlRunner) error {
	var stdout, stderr bytes.Buffer
	err := kubectl.RunWithOutput([]string{
		"annotate", "ingress", "registry",
		"-n", "registry",
		"traefik.ingress.kubernetes.io/router.middlewares=" + registryAdminAuthMiddleware,
		"--overwrite",
	}, &stdout, &stderr)
	if err != nil && stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return err
}

func configureProvisionedRegistryEnv(ext *config.ExternalRegistryConfig, secretName string) error {
	return configureProvisionedRegistryEnvWithKubectl(core.DefaultKubectlClient(), ext, secretName)
}

func configureProvisionedRegistryEnvWithKubectl(kubectl core.KubectlRunner, ext *config.ExternalRegistryConfig, secretName string) error {
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
		platformMode := os.Getenv("MCP_PLATFORM_MODE")
		catalogNamespace := setupplan.CatalogNamespaceForPlatformMode(platformMode)
		if catalogNamespace != "" {
			if err := kube.EnsureNamespaceWithLabels(kubectl.CommandArgs, catalogNamespace, catalogNamespaceLabels(platformMode)); err != nil {
				return err
			}
			// Create imagePullSecret in the active catalog namespace for pod image pulls.
			if err := ensureImagePullSecretWithKubectl(kubectl, catalogNamespace, secretName, ext.URL, ext.Username, ext.Password); err != nil {
				return err
			}
		}
		args = append(args, "PROVISIONED_REGISTRY_SECRET_NAME="+secretName)
		// Populate env vars from the secret instead of literals to avoid leaking creds in args/history.
		args = append(args, "--from=secret/"+secretName)
	}
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput(args, os.Stdout, os.Stderr)
}

func ensureProvisionedRegistrySecretWithKubectl(kubectl core.KubectlRunner, name, username, password string) error {
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
		"-n", core.NamespaceMCPRuntime,
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
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrRenderSecretManifestFailed,
			err,
			fmt.Sprintf("render secret manifest: %v", err),
			map[string]any{"secret_name": name, "namespace": core.NamespaceMCPRuntime, "component": "setup"},
		)
		core.Error("Failed to render secret manifest")
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
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplySecretManifestFailed,
			err,
			fmt.Sprintf("apply secret manifest: %v", err),
			map[string]any{"secret_name": name, "namespace": core.NamespaceMCPRuntime, "component": "setup"},
		)
		core.Error("Failed to apply secret manifest")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	return nil
}

func ensureImagePullSecretWithKubectl(kubectl core.KubectlRunner, namespace, name, registry, username, password string) error {
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
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrMarshalDockerConfigFailed,
			err,
			fmt.Sprintf("marshal docker config: %v", err),
			map[string]any{"registry": registry, "namespace": namespace, "component": "setup"},
		)
		core.Error("Failed to marshal docker config")
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
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyImagePullSecretFailed,
			err,
			fmt.Sprintf("apply imagePullSecret: %v", err),
			map[string]any{"secret_name": name, "namespace": namespace, "registry": registry, "component": "setup"},
		)
		core.Error("Failed to apply image pull secret")
		// Note: logger not available in this helper, but error will be logged by caller
		return wrappedErr
	}

	return nil
}

func buildOperatorImage(image string) error {
	target := "docker-build-operator-no-test"
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := core.ExecCommandWithValidators("make", []string{"-f", "Makefile.operator", target, "IMG=" + image})
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
	dockerfilePath, err := assetpath.ResolveRepoAssetPath(gatewayProxyDockerfilePath)
	if err != nil {
		return err
	}
	buildContext, err := assetpath.ResolveRepoAssetPath(gatewayProxyBuildContext)
	if err != nil {
		return err
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := core.ExecCommandWithValidators("docker", []string{
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
	resolvedDockerfilePath, err := assetpath.ResolveRepoAssetPath(dockerfilePath)
	if err != nil {
		return err
	}
	resolvedBuildContext, err := assetpath.ResolveRepoAssetPath(buildContext)
	if err != nil {
		return err
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := core.ExecCommandWithValidators("docker", []string{
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
	return restartDeploymentWithKubectl(core.DefaultKubectlClient(), name, namespace)
}

func restartDeploymentWithKubectl(kubectl core.KubectlRunner, name, namespace string) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput([]string{"rollout", "restart", "deployment/" + name, "-n", namespace}, os.Stdout, os.Stderr)
}

func pushOperatorImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := core.ExecCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushGatewayProxyImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := core.ExecCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushAnalyticsImage(image string) error {
	// #nosec G204 -- image from internal build process or validated config.
	cmd, err := core.ExecCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushOperatorImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := registry.DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "setup"},
		)
		core.Error("Failed to push image in-cluster")
		core.LogStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func pushGatewayProxyImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := registry.DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "gateway-proxy"},
		)
		core.Error("Failed to push image in-cluster")
		core.LogStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func pushAnalyticsImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := registry.DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushImageInClusterFailed,
			err,
			fmt.Sprintf("failed to push image in-cluster: %v", err),
			map[string]any{"source_image": sourceImage, "target_image": targetImage, "namespace": helperNamespace, "component": "analytics"},
		)
		core.Error("Failed to push image in-cluster")
		core.LogStructuredError(logger, wrappedErr, "Failed to push image in-cluster")
		return wrappedErr
	}
	return nil
}

func checkCRDInstalled(name string) error {
	// #nosec G204 -- name is hardcoded CRD identifier from internal code.
	return checkCRDInstalledWithKubectl(core.DefaultKubectlClient(), name)
}

func checkCRDInstalledWithKubectl(kubectl core.KubectlRunner, name string) error {
	// #nosec G204 -- name is hardcoded CRD identifier from internal code.
	return kubectl.RunWithOutput([]string{"get", "crd", name}, os.Stdout, os.Stderr)
}

// waitForDeploymentAvailable polls a deployment until it has at least one available replica or times out.
func waitForDeploymentAvailable(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
	return waitForDeploymentAvailableWithKubectl(core.DefaultKubectlClient(), logger, name, namespace, selector, timeout)
}

// waitForDeploymentAvailableWithKubectl polls a deployment until it has at least one available replica or times out.
func waitForDeploymentAvailableWithKubectl(kubectl core.KubectlRunner, logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
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
			core.Info(fmt.Sprintf("Still waiting for deployment/%s in %s (selector %s, timeout %s)", name, namespace, selector, timeout.Round(time.Second)))
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
			wrappedErr := core.WrapWithSentinelAndContext(core.ErrDeploymentTimeout, cause, msg, ctx)
			core.Error("Deployment timeout")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Deployment timeout")
			}
			return wrappedErr
		}
		time.Sleep(5 * time.Second)
	}
}

// printDeploymentDiagnostics prints a quick status of pods for a deployment selector to help users triage readiness issues.
func printDeploymentDiagnostics(deploy, namespace, selector string) {
	printDeploymentDiagnosticsWithKubectl(core.DefaultKubectlClient(), deploy, namespace, selector)
}

// printDeploymentDiagnosticsWithKubectl prints a quick status of pods for a deployment selector.
func printDeploymentDiagnosticsWithKubectl(kubectl core.KubectlRunner, deploy, namespace, selector string) {
	core.Warn(fmt.Sprintf("Deployment %s in %s is not ready. Showing pod statuses:", deploy, namespace))
	// #nosec G204 -- namespace/selector from internal diagnostics, not user input.
	_ = kubectl.RunWithOutput([]string{"get", "pods", "-n", namespace, "-l", selector, "-o", "wide"}, os.Stdout, os.Stderr)
}

// mergeDeploymentDebugDiagnosticsIfNeeded fetches describe/events/pods from the API when --debug is set
// and attaches a bounded blob to the errx context (cluster-backed failures, not local validation).
func mergeDeploymentDebugDiagnosticsIfNeeded(kubectl core.KubectlRunner, m map[string]any, deployName, namespace, selector string) {
	if !core.IsDebugMode() {
		return
	}
	if d := buildDeploymentWaitDebugDetail(kubectl, deployName, namespace, selector); d != "" {
		m["diagnostics"] = trimDiagnosticsString(d)
	}
}

// buildDeploymentWaitDebugDetail returns kubectl text for a stuck or timed-out deployment wait.
func buildDeploymentWaitDebugDetail(kubectl core.KubectlRunner, deployName, namespace, selector string) string {
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
func buildNamespacedResourceDebugDetail(kubectl core.KubectlRunner, kind, name, namespace string) string {
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
func buildCRDCheckDebugDetail(kubectl core.KubectlRunner, crdName string) string {
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

func mergeCRDCheckDebugDiagnosticsIfNeeded(kubectl core.KubectlRunner, m map[string]any, crdName string) {
	if !core.IsDebugMode() {
		return
	}
	if d := buildCRDCheckDebugDetail(kubectl, crdName); d != "" {
		m["diagnostics"] = trimDiagnosticsString(d)
	}
}

// deployOperatorManifests deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifests(logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error {
	return deployOperatorManifestsWithKubectl(core.DefaultKubectlClient(), logger, operatorImage, gatewayProxyImage, operatorArgs)
}

// deployOperatorManifestsWithKubectl deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name and injecting operator args/env.
func deployOperatorManifestsWithKubectl(kubectl core.KubectlRunner, logger *zap.Logger, operatorImage, gatewayProxyImage string, operatorArgs []string) error {
	if err := ensureRepoManagedTraefikMiddlewareResources(kubectl, logger); err != nil {
		return err
	}

	// Step 1: Apply CRD
	core.Info("Applying CRD manifests")
	// #nosec G204 -- fixed directory path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "--validate=false", "-f", "config/crd/bases"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrApplyCRDFailed, err, fmt.Sprintf("failed to apply CRD: %v", err))
		core.Error("Failed to apply CRD")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply CRD")
		}
		return wrappedErr
	}

	// Step 2: Apply RBAC (ServiceAccount, Role, RoleBinding)
	core.Info("Applying RBAC manifests")
	if err := kube.EnsureNamespace(core.DefaultKubectlClient().CommandArgs, core.NamespaceMCPRuntime); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrEnsureOperatorNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure operator namespace: %v", err),
			map[string]any{"namespace": core.NamespaceMCPRuntime, "component": "setup"},
		)
		core.Error("Failed to ensure operator namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to ensure operator namespace")
		}
		return wrappedErr
	}

	// #nosec G204 -- fixed kustomize path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "-k", "config/rbac/"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrApplyRBACFailed, err, fmt.Sprintf("failed to apply RBAC: %v", err))
		core.Error("Failed to apply RBAC")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply RBAC")
		}
		return wrappedErr
	}
	core.Info("Reapplied operator ClusterRole mcp-runtime-operator-role from config/rbac/role.yaml; run `mcp-runtime cluster doctor` if MCPServer creates ever appear unreconciled")

	// Step 3: Apply manager deployment with structured image replacement
	core.Info("Applying operator deployment")

	// Read manager.yaml and apply structured mutations
	managerYAML, err := os.ReadFile("config/manager/manager.yaml")
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to read manager.yaml: %v", err))
		core.Error("Failed to read manager.yaml")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to read manager.yaml")
		}
		return wrappedErr
	}

	// Use structured manifest mutation instead of regex
	mutator, err := manifest.NewMutator(managerYAML)
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrParseManagerYAMLFailed, err, fmt.Sprintf("failed to parse manager.yaml: %v", err))
		core.Error("Failed to parse manager.yaml")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to parse manager.yaml")
		}
		return wrappedErr
	}

	// Set the operator image
	if err := mutator.SetDeploymentImage(core.OperatorDeploymentName, core.OperatorManagerContainerName, operatorImage); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrSetOperatorImageFailed, err, fmt.Sprintf("failed to set operator image: %v", err))
		core.Error("Failed to set operator image")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to set operator image")
		}
		return wrappedErr
	}

	// Set image pull policy based on image
	pullPolicy := operatorImagePullPolicy(operatorImage)
	if pullPolicy != "" {
		if err := mutator.SetDeploymentImagePullPolicy(core.OperatorDeploymentName, core.OperatorManagerContainerName, pullPolicy); err != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to set operator image pull policy: %v", err))
			core.Error("Failed to set operator image pull policy")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to set operator image pull policy")
			}
			return wrappedErr
		}
	}

	// Inject operator args if provided
	if len(operatorArgs) > 0 {
		if err := mutator.MergeDeploymentArgs(core.OperatorDeploymentName, core.OperatorManagerContainerName, operatorArgs); err != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to merge operator args: %v", err))
			core.Error("Failed to merge operator args")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to merge operator args")
			}
			return wrappedErr
		}
	}

	// Inject environment variables if provided.
	existingGatewayOTLPEndpoint := existingOperatorEnvValue(kubectl, gatewayOTELExporterOTLPEndpointEnv)
	if envVars := operatorEnvOverrides(gatewayProxyImage, existingGatewayOTLPEndpoint); len(envVars) > 0 {
		envMap := make(map[string]string, len(envVars))
		for _, ev := range envVars {
			envMap[ev.Name] = ev.Value
		}
		if err := mutator.MergeDeploymentEnv(core.OperatorDeploymentName, core.OperatorManagerContainerName, envMap); err != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrMutateManagerYAMLFailed, err, fmt.Sprintf("failed to merge operator env vars: %v", err))
			core.Error("Failed to merge operator env vars")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to merge operator env vars")
			}
			return wrappedErr
		}
	}

	// Render the mutated manifest
	mutatedYAML, err := mutator.ToYAML()
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrRenderManagerYAMLFailed, err, fmt.Sprintf("failed to render mutated manifest: %v", err))
		core.Error("Failed to render mutated manifest")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to render mutated manifest")
		}
		return wrappedErr
	}

	// Write to temp file under the working directory so kubectl path validation passes.
	tmpFile, err := os.CreateTemp(".", "manager-*.yaml")
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCreateTempFileFailed, err, fmt.Sprintf("failed to create temp file: %v", err))
		core.Error("Failed to create temp file")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create temp file")
		}
		return wrappedErr
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(mutatedYAML); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrCloseTempFileFailed, errors.Join(err, closeErr), fmt.Sprintf("failed to close temp file after write error: %v", closeErr))
			core.Error("Failed to close temp file")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to close temp file")
			}
			return wrappedErr
		}
		wrappedErr := core.WrapWithSentinel(core.ErrWriteTempFileFailed, err, fmt.Sprintf("failed to write temp file: %v", err))
		core.Error("Failed to write temp file")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to write temp file")
		}
		return wrappedErr
	}
	if err := tmpFile.Close(); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCloseTempFileFailed, err, fmt.Sprintf("failed to close temp file: %v", err))
		core.Error("Failed to close temp file")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to close temp file")
		}
		return wrappedErr
	}

	// Delete existing deployment to avoid immutable selector conflicts on reapply.
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	_ = kubectl.Run([]string{"delete", "deployment/" + core.OperatorDeploymentName, "-n", core.NamespaceMCPRuntime, "--ignore-not-found"})

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := kubectl.RunWithOutput([]string{"apply", "-f", tmpFile.Name()}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyManagerDeploymentFailed,
			err,
			fmt.Sprintf("failed to apply manager deployment: %v", err),
			map[string]any{"operator_image": operatorImage, "namespace": core.NamespaceMCPRuntime, "component": "setup"},
		)
		core.Error("Failed to apply manager deployment")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply manager deployment")
		}
		return wrappedErr
	}

	core.Success("Operator manifests deployed successfully")
	return nil
}

// mcpSentinelDependencyRolloutFailed wraps early mcp-sentinel storage/messaging rollouts; diagnostics are attached only in --debug.
func mcpSentinelDependencyRolloutFailed(kubectl core.KubectlRunner, err error, kind, name, namespace, phase string) error {
	ctx := map[string]any{
		"component": "mcp-sentinel",
		"phase":     phase,
		"resource":  fmt.Sprintf("%s/%s", kind, name),
		"namespace": namespace,
	}
	if core.IsDebugMode() {
		if diag := buildNamespacedResourceDebugDetail(kubectl, kind, name, namespace); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return core.WrapWithSentinelAndContext(core.ErrOperatorDeploymentFailed, err,
		fmt.Sprintf("mcp-sentinel %s: %s/%s: %v", phase, kind, name, err), ctx)
}

// mcpSentinelDependencyJobFailed wraps the clickhouse init job; diagnostics are attached only in --debug.
func mcpSentinelDependencyJobFailed(kubectl core.KubectlRunner, err error, name, namespace, phase string) error {
	ctx := map[string]any{
		"component": "mcp-sentinel",
		"phase":     phase,
		"resource":  "job/" + name,
		"namespace": namespace,
	}
	if core.IsDebugMode() {
		if diag := buildNamespacedResourceDebugDetail(kubectl, "job", name, namespace); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return core.WrapWithSentinelAndContext(core.ErrOperatorDeploymentFailed, err,
		fmt.Sprintf("mcp-sentinel %s: job/%s: %v", phase, name, err), ctx)
}

func deployAnalyticsManifests(logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string) error {
	return deployAnalyticsManifestsWithKubectl(core.DefaultKubectlClient(), logger, images, storageMode, platformMode)
}

func deployAnalyticsManifestsWithKubectl(kubectl core.KubectlRunner, logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string) error {
	rolloutTimeout := analyticsRolloutTimeoutString()

	if err := ensureRepoManagedTraefikMiddlewareResources(kubectl, logger); err != nil {
		return err
	}

	core.Info("Applying mcp-sentinel namespace and config")
	manifests := []string{
		"k8s/00-namespace.yaml",
		"k8s/01-config.yaml",
	}
	for _, manifest := range manifests {
		if err := applyRenderedManifest(kubectl, manifest, images, "", platformMode); err != nil {
			return err
		}
	}
	if err := applyCatalogNamespaceForMode(kubectl, platformMode); err != nil {
		return err
	}

	core.Info("Applying mcp-sentinel managed secrets")
	secretManifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		return err
	}
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, secretManifest); err != nil {
		return err
	}

	imagePullSecretName, err := ensureAnalyticsImagePullSecret(kubectl)
	if err != nil {
		return err
	}

	clickhouseManifest := "k8s/03-clickhouse.yaml"
	kafkaManifest := "k8s/05-kafka.yaml"
	postgresManifest := "k8s/20-postgres.yaml"
	if storageMode == setupplan.StorageModeHostpath {
		clickhouseManifest = "k8s/03-clickhouse-hostpath.yaml"
		kafkaManifest = "k8s/05-kafka-hostpath.yaml"
		postgresManifest = "k8s/20-postgres-hostpath.yaml"
	}

	core.Info("Applying analytics storage and messaging components")
	for _, manifest := range []string{
		clickhouseManifest,
		kafkaManifest,
	} {
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName, platformMode); err != nil {
			return err
		}
	}

	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "clickhouse", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "clickhouse", core.DefaultAnalyticsNamespace, "storage (clickhouse)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "deployment", "zookeeper", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "deployment", "zookeeper", core.DefaultAnalyticsNamespace, "messaging (zookeeper)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "kafka", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "kafka", core.DefaultAnalyticsNamespace, "messaging (kafka)")
	}

	core.Info("Initializing ClickHouse schema")
	if err := deleteJobIfExistsWithKubectl(kubectl, "clickhouse-init", core.DefaultAnalyticsNamespace); err != nil {
		return fmt.Errorf("delete existing clickhouse init job: %w", err)
	}
	if err := applyRenderedManifest(kubectl, "k8s/04-clickhouse-init.yaml", images, imagePullSecretName, platformMode); err != nil {
		return err
	}
	if err := waitForJobCompletionWithKubectl(kubectl, "clickhouse-init", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyJobFailed(kubectl, err, "clickhouse-init", core.DefaultAnalyticsNamespace, "clickhouse init schema")
	}

	core.Info("Applying analytics services")
	for _, manifest := range []string{
		postgresManifest,
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
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName, platformMode); err != nil {
			return err
		}
	}

	if err := applyPlatformIngressIfConfigured(kubectl); err != nil {
		return err
	}

	core.Info(fmt.Sprintf("Waiting for mcp-sentinel workload rollouts (per-resource timeout %s; override with MCP_DEPLOYMENT_TIMEOUT)", rolloutTimeout))
	targets := []struct{ kind, name string }{
		{kind: "statefulset", name: "mcp-sentinel-postgres"},
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
		rolloutLog, err := runRolloutWithOptionalDebugCapture(kubectl, target.kind, target.name, core.DefaultAnalyticsNamespace, rolloutTimeout)
		if err != nil {
			rolloutFailures = append(rolloutFailures, fmt.Sprintf("%s/%s: %v", target.kind, target.name, err))
			failedForDebug = append(failedForDebug, analyticsFailedRollout{
				kind: target.kind, name: target.name, rolloutLog: rolloutLog,
			})
		}
	}
	if len(rolloutFailures) == 0 {
		core.Success("mcp-sentinel manifests deployed successfully")
		return nil
	}

	printAnalyticsRolloutDiagnostics(kubectl)
	summary := strings.Join(rolloutFailures, "; ")
	cause := errors.New(summary)
	msg := fmt.Sprintf("analytics components failed to roll out: %s", summary)
	ctx := map[string]any{"component": "mcp-sentinel", "rollout_failures": summary}
	if core.IsDebugMode() {
		if diag := buildAnalyticsRolloutDebugDetail(kubectl, failedForDebug); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return core.WrapWithSentinelAndContext(core.ErrOperatorDeploymentFailed, cause, msg, ctx)
}

func ensureRepoManagedTraefikMiddlewareResources(kubectl core.KubectlRunner, logger *zap.Logger) error {
	namespaces, err := activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl)
	if err != nil {
		return err
	}
	for _, namespace := range namespaces {
		if logger != nil {
			logger.Info("Reconciling Traefik file-provider resources", zap.String("namespace", namespace))
		}
		if err := applyTraefikSupportManifest(kubectl, "config/ingress/overlays/http/dynamic-config.yaml", namespace); err != nil {
			return err
		}
		if err := applyTraefikSupportManifest(kubectl, "config/ingress/overlays/http/plugin-source.yaml", namespace); err != nil {
			return err
		}
		if err := patchTraefikDeploymentForFileMiddlewareSupport(kubectl, namespace); err != nil {
			return err
		}
	}
	return nil
}

func activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl core.KubectlRunner) ([]string, error) {
	cmd, err := kubectl.CommandArgs([]string{
		"get", "deployment", "-A", "--no-headers",
		"-o", "custom-columns=NS:.metadata.namespace,NAME:.metadata.name",
	})
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list traefik deployments: %w", err)
	}
	seen := map[string]struct{}{}
	var namespaces []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "traefik" {
			continue
		}
		ns := strings.TrimSpace(fields[0])
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		namespaces = append(namespaces, ns)
	}
	slices.Sort(namespaces)
	return namespaces, nil
}

func applyTraefikSupportManifest(kubectl core.KubectlRunner, relPath, namespace string) error {
	resolvedPath, err := assetpath.ResolveRepoAssetPath(relPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadIngressManifestFailed, err, fmt.Sprintf("failed to resolve Traefik manifest %s: %v", relPath, err))
	}
	manifestBytes, err := kube.ReadFileAtPath(resolvedPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadIngressManifestFailed, err, fmt.Sprintf("failed to read Traefik manifest %s: %v", relPath, err))
	}
	manifestContent := strings.ReplaceAll(string(manifestBytes), "namespace: traefik", "namespace: "+namespace)
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, manifestContent); err != nil {
		return core.WrapWithSentinel(
			core.ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to reconcile Traefik manifest %s in namespace %s: %v", relPath, namespace, err),
		)
	}
	return nil
}

type traefikDeploymentSpec struct {
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name         string   `json:"name"`
					Args         []string `json:"args"`
					VolumeMounts []struct {
						Name      string `json:"name"`
						MountPath string `json:"mountPath"`
					} `json:"volumeMounts"`
				} `json:"containers"`
				Volumes []struct {
					Name string `json:"name"`
				} `json:"volumes"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

type jsonPatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func patchTraefikDeploymentForFileMiddlewareSupport(kubectl core.KubectlRunner, namespace string) error {
	spec, err := readTraefikDeploymentSpec(kubectl, namespace)
	if err != nil {
		return err
	}
	if len(spec.Spec.Template.Spec.Containers) == 0 {
		return core.NewWithSentinel(core.ErrInstallIngressControllerFailed, fmt.Sprintf("traefik deployment in namespace %s has no containers", namespace))
	}
	containerIndex := -1
	for i, candidate := range spec.Spec.Template.Spec.Containers {
		if candidate.Name == "traefik" {
			containerIndex = i
			break
		}
	}
	if containerIndex == -1 {
		return core.NewWithSentinel(core.ErrInstallIngressControllerFailed, fmt.Sprintf("traefik deployment in namespace %s has no container named traefik", namespace))
	}
	container := spec.Spec.Template.Spec.Containers[containerIndex]

	var ops []jsonPatchOperation
	if !containsString(container.Args, "--providers.file.filename=/etc/traefik/dynamic/dynamic.yml") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--providers.file.filename=/etc/traefik/dynamic/dynamic.yml",
		})
	}
	if !containsString(container.Args, "--providers.file.watch=true") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--providers.file.watch=true",
		})
	}
	if !containsString(container.Args, "--experimental.localplugins.pii-redactor.modulename=github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--experimental.localplugins.pii-redactor.modulename=github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor",
		})
	}
	addDynamicMount := !hasVolumeMountPath(container.VolumeMounts, "/etc/traefik/dynamic")
	if addDynamicMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-dynamic", "mountPath": "/etc/traefik/dynamic", "readOnly": true},
		})
	}
	addPluginSourceMount := !hasVolumeMountPath(container.VolumeMounts, "/plugins-local/src/github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor")
	if addPluginSourceMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-plugin-source", "mountPath": "/plugins-local/src/github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor", "readOnly": true},
		})
	}
	addPluginStorageMount := !hasVolumeMountPath(container.VolumeMounts, "/plugins-storage")
	if addPluginStorageMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-plugins", "mountPath": "/plugins-storage"},
		})
	}
	if addDynamicMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-dynamic") {
		ops = append(ops, jsonPatchOperation{
			Op:   "add",
			Path: "/spec/template/spec/volumes/-",
			Value: map[string]any{
				"name": "traefik-dynamic",
				"configMap": map[string]any{
					"name":  "traefik-dynamic",
					"items": []map[string]any{{"key": "dynamic.yml", "path": "dynamic.yml"}},
				},
			},
		})
	}
	if addPluginSourceMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-plugin-source") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  "/spec/template/spec/volumes/-",
			Value: map[string]any{"name": "traefik-plugin-source", "configMap": map[string]any{"name": "traefik-plugin-pii-redactor"}},
		})
	}
	if addPluginStorageMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-plugins") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  "/spec/template/spec/volumes/-",
			Value: map[string]any{"name": "traefik-plugins", "emptyDir": map[string]any{}},
		})
	}
	if len(ops) == 0 {
		return nil
	}
	patchBytes, err := json.Marshal(ops)
	if err != nil {
		return fmt.Errorf("marshal traefik deployment patch: %w", err)
	}
	if err := kubectl.RunWithOutput([]string{
		"patch", "deployment", "traefik", "-n", namespace, "--type=json", "-p", string(patchBytes),
	}, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinel(
			core.ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to patch traefik deployment in namespace %s for file-provider middleware support: %v", namespace, err),
		)
	}
	return nil
}

func readTraefikDeploymentSpec(kubectl core.KubectlRunner, namespace string) (traefikDeploymentSpec, error) {
	var spec traefikDeploymentSpec
	cmd, err := kubectl.CommandArgs([]string{"get", "deployment", "traefik", "-n", namespace, "-o", "json"})
	if err != nil {
		return spec, err
	}
	out, err := cmd.Output()
	if err != nil {
		return spec, fmt.Errorf("read traefik deployment %s/traefik: %w", namespace, err)
	}
	if err := json.Unmarshal(out, &spec); err != nil {
		return spec, fmt.Errorf("decode traefik deployment %s/traefik: %w", namespace, err)
	}
	return spec, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hasVolumeMountPath(mounts []struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}, mountPath string) bool {
	for _, mount := range mounts {
		if mount.MountPath == mountPath {
			return true
		}
	}
	return false
}

func hasVolume(volumes []struct {
	Name string `json:"name"`
}, name string) bool {
	for _, volume := range volumes {
		if volume.Name == name {
			return true
		}
	}
	return false
}

func applyCatalogNamespaceForMode(kubectl core.KubectlRunner, platformMode string) error {
	namespace := setupplan.CatalogNamespaceForPlatformMode(platformMode)
	if strings.TrimSpace(namespace) == "" {
		return nil
	}
	core.Info(fmt.Sprintf("Applying platform catalog namespace %s", namespace))
	return kube.EnsureNamespaceWithLabels(kubectl.CommandArgs, namespace, catalogNamespaceLabels(platformMode))
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
func runRolloutWithOptionalDebugCapture(kubectl core.KubectlRunner, kind, name, namespace, timeout string) (capture string, err error) {
	args := []string{
		"rollout", "status",
		fmt.Sprintf("%s/%s", kind, name),
		"-n", namespace, "--timeout=" + timeout,
	}
	if !core.IsDebugMode() {
		return "", kubectl.RunWithOutput(args, os.Stdout, os.Stderr)
	}
	var buf bytes.Buffer
	w := io.MultiWriter(os.Stdout, &buf)
	err = kubectl.RunWithOutput(args, w, w)
	return buf.String(), err
}

func kubectlText(kubectl core.KubectlRunner, args []string) (string, error) {
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
func buildAnalyticsRolloutDebugDetail(kubectl core.KubectlRunner, failed []analyticsFailedRollout) string {
	var b strings.Builder
	for _, w := range failed {
		if strings.TrimSpace(w.rolloutLog) != "" {
			b.WriteString(fmt.Sprintf("---- kubectl rollout status %s/%s\n", w.kind, w.name))
			b.WriteString(w.rolloutLog)
		}
		b.WriteString(fmt.Sprintf("---- describe %s %s\n", w.kind, w.name))
		out, err := kubectlText(kubectl, []string{
			"describe", w.kind, w.name, "-n", core.DefaultAnalyticsNamespace, "--request-timeout=30s",
		})
		if err != nil {
			b.WriteString(fmt.Sprintf("error: %v\n", err))
			continue
		}
		b.WriteString(out)
	}
	b.WriteString("---- get pods (wide)\n")
	if out, err := kubectlText(kubectl, []string{"get", "pods", "-n", core.DefaultAnalyticsNamespace, "-o", "wide", "--request-timeout=30s"}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get events (sorted)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "events", "-n", core.DefaultAnalyticsNamespace, "--sort-by", ".lastTimestamp", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

func applyRenderedManifest(kubectl core.KubectlRunner, manifestPath string, images AnalyticsImageSet, imagePullSecretName, platformMode string) error {
	resolvedManifestPath, err := assetpath.ResolveRepoAssetPath(manifestPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to resolve manifest %s: %v", manifestPath, err))
	}

	content, err := kube.ReadFileAtPath(resolvedManifestPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to read manifest %s: %v", resolvedManifestPath, err))
	}
	rendered := ""
	if manifestPath == "k8s/01-config.yaml" {
		rendered, err = renderAnalyticsConfigManifest(kubectl, string(content), platformMode, images)
	} else {
		rendered, err = renderAnalyticsManifest(string(content), images, imagePullSecretName, platformMode)
	}
	if err != nil {
		return fmt.Errorf("render manifest %s: %w", manifestPath, err)
	}
	return kube.ApplyManifestContent(kubectl.CommandArgs, rendered)
}

func applyPlatformIngressIfConfigured(kubectl core.KubectlRunner) error {
	host := strings.TrimSpace(core.GetPlatformIngressHost())
	if host == "" {
		return nil
	}
	manifest := ingressmanifest.RenderPlatformUIIngress(host, core.GetRegistryClusterIssuerName(), core.DefaultAnalyticsNamespace)
	core.Info(fmt.Sprintf("Applying platform UI ingress for %s", host))
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, manifest); err != nil {
		return fmt.Errorf("apply platform UI ingress: %w", err)
	}
	if err := removePathBasedSentinelIngresses(kubectl); err != nil {
		return err
	}
	return nil
}

func removePathBasedSentinelIngresses(kubectl core.KubectlRunner) error {
	args := append([]string{"delete", "ingress"}, pathBasedSentinelIngressNames...)
	args = append(args, "-n", core.DefaultAnalyticsNamespace, "--ignore-not-found=true")
	if err := kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("remove path-based sentinel ingresses for public platform host: %w", err)
	}
	return nil
}

func renderAnalyticsManifest(content string, images AnalyticsImageSet, imagePullSecretName, platformMode string) (string, error) {
	replacements := map[string]string{}
	if mode, ok := setupplan.NormalizePlatformMode(platformMode); ok && mode != "" {
		replacements[`PLATFORM_MODE: "tenant"`] = fmt.Sprintf(`PLATFORM_MODE: "%s"`, mode)
	}
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

func renderAnalyticsConfigManifest(kubectl core.KubectlRunner, content, platformMode string, images AnalyticsImageSet) (string, error) {
	type configMapManifest struct {
		APIVersion string            `yaml:"apiVersion"`
		Kind       string            `yaml:"kind"`
		Metadata   map[string]any    `yaml:"metadata"`
		Data       map[string]string `yaml:"data"`
	}

	var manifest configMapManifest
	if err := yaml.Unmarshal([]byte(content), &manifest); err != nil {
		return "", fmt.Errorf("decode analytics config manifest: %w", err)
	}
	if manifest.Data == nil {
		manifest.Data = map[string]string{}
	}

	existingData, err := existingConfigMapData(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-config")
	if err != nil {
		return "", err
	}
	for _, key := range []string{
		"GOOGLE_CLIENT_ID",
		"MCP_SENTINEL_INGEST_URL",
		"OIDC_ISSUER",
		"OIDC_AUDIENCE",
		"OIDC_JWKS_URL",
		"MCP_PLATFORM_DOMAIN",
		"MCP_MCP_INGRESS_HOST",
		"MCP_REGISTRY_ENDPOINT",
		"MCP_REGISTRY_INGRESS_HOST",
		"PLATFORM_REGISTRY_URL",
		"PLATFORM_TRAEFIK_NAMESPACE",
	} {
		if envValue := setupAnalyticsConfigEnvValue(key); envValue != "" {
			manifest.Data[key] = envValue
			continue
		}
		if strings.TrimSpace(manifest.Data[key]) == "" && strings.TrimSpace(existingData[key]) != "" {
			manifest.Data[key] = existingData[key]
		}
	}
	applyGoogleOIDCDefaults(manifest.Data)
	if registryEndpoint := strings.TrimSpace(core.GetRegistryEndpoint()); registryEndpoint != "" {
		manifest.Data["MCP_REGISTRY_ENDPOINT"] = registryEndpoint
	}
	if registryIngressHost := strings.TrimSpace(core.GetRegistryIngressHost()); registryIngressHost != "" {
		manifest.Data["MCP_REGISTRY_INGRESS_HOST"] = registryIngressHost
	}
	if registryHost := platformRegistryHostForConfig(images); registryHost != "" {
		manifest.Data["PLATFORM_REGISTRY_URL"] = registryHost
	}
	if strings.TrimSpace(manifest.Data["PLATFORM_TRAEFIK_NAMESPACE"]) == "" {
		if namespace := activeTraefikNamespaceForPlatform(kubectl); namespace != "" {
			manifest.Data["PLATFORM_TRAEFIK_NAMESPACE"] = namespace
		}
	}
	if existingMode, ok := setupplan.NormalizePlatformMode(existingData["PLATFORM_MODE"]); ok {
		requestedMode, requestedOK := setupplan.NormalizePlatformMode(platformMode)
		switch {
		case !requestedOK:
			manifest.Data["PLATFORM_MODE"] = existingMode
		case requestedMode == setupplan.PlatformModeTenant && existingMode != setupplan.PlatformModeTenant:
			manifest.Data["PLATFORM_MODE"] = existingMode
		default:
			manifest.Data["PLATFORM_MODE"] = requestedMode
		}
	} else if mode, ok := setupplan.NormalizePlatformMode(platformMode); ok {
		manifest.Data["PLATFORM_MODE"] = mode
	}

	rendered, err := yaml.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode analytics config manifest: %w", err)
	}
	return string(rendered), nil
}

func setupAnalyticsConfigEnvValue(key string) string {
	candidates := []string{key}
	if key == "GOOGLE_CLIENT_ID" {
		candidates = append(candidates, "MCP_GOOGLE_CLIENT_ID")
	} else if strings.HasPrefix(key, "OIDC_") {
		candidates = append(candidates, "MCP_"+key)
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(os.Getenv(candidate)); value != "" {
			return value
		}
	}
	return ""
}

func platformRegistryHostForConfig(images AnalyticsImageSet) string {
	if explicit := setupAnalyticsConfigEnvValue("PLATFORM_REGISTRY_URL"); explicit != "" {
		return explicit
	}
	if host := strings.TrimSpace(core.GetRegistryIngressHost()); host != "" &&
		(host != core.DefaultRegistryIngressHost || registryIngressHostExplicitlyConfigured()) {
		return host
	}
	return registryHostFromImage(images.API)
}

func registryIngressHostExplicitlyConfigured() bool {
	for _, key := range []string{"MCP_REGISTRY_INGRESS_HOST", "MCP_PLATFORM_DOMAIN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return true
		}
	}
	return false
}

func applyGoogleOIDCDefaults(data map[string]string) {
	clientID := strings.TrimSpace(data["GOOGLE_CLIENT_ID"])
	if clientID == "" {
		return
	}
	if strings.TrimSpace(data["OIDC_ISSUER"]) == "" {
		data["OIDC_ISSUER"] = "https://accounts.google.com"
	}
	if strings.TrimSpace(data["OIDC_AUDIENCE"]) == "" {
		data["OIDC_AUDIENCE"] = clientID
	}
	if strings.TrimSpace(data["OIDC_JWKS_URL"]) == "" {
		data["OIDC_JWKS_URL"] = "https://www.googleapis.com/oauth2/v3/certs"
	}
}

func activeTraefikNamespaceForPlatform(kubectl core.KubectlRunner) string {
	namespaces, err := activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl)
	if err != nil || len(namespaces) == 0 {
		return ""
	}
	for _, preferred := range []string{"traefik", "kube-system"} {
		if slices.Contains(namespaces, preferred) {
			return preferred
		}
	}
	return namespaces[0]
}

func registryHostFromImage(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	repo, _ := ref.SplitImage(image)
	first, _, found := strings.Cut(repo, "/")
	if !found {
		return ""
	}
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return first
	}
	return ""
}

func existingConfigMapData(kubectl core.KubectlRunner, namespace, name string) (map[string]string, error) {
	cmd, err := kubectl.CommandArgs([]string{"get", "configmap", name, "-n", namespace, "-o", "json"})
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.ToLower(strings.TrimSpace(string(out)))
		if strings.Contains(detail, "not found") || strings.Contains(detail, "notfound") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read configmap %s/%s: %w", namespace, name, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return map[string]string{}, nil
	}
	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decode configmap %s/%s: %w", namespace, name, err)
	}
	if payload.Data == nil {
		return map[string]string{}, nil
	}
	return payload.Data, nil
}

func renderAnalyticsSecretManifest(kubectl core.KubectlRunner) (string, error) {
	apiKeys, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "API_KEYS", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	ingestAPIKeys, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "INGEST_API_KEYS", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	uiAPIKey, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "UI_API_KEY", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	apiKeys = ensureCSVIncludes(apiKeys, uiAPIKey)
	adminAPIKeys, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "ADMIN_API_KEYS")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	adminAPIKeys = ensureCSVIncludes(adminAPIKeys, uiAPIKey)
	grafanaPassword, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "GRAFANA_ADMIN_PASSWORD", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresUser, err := existingSecretDataValueOrDefault(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_USER", "mcp_runtime")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresPassword, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_PASSWORD", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresDB, err := existingSecretDataValueOrDefault(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_DB", "mcp_runtime")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresDSN, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_DSN")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if postgresDSN == "" {
		postgresDSN = fmt.Sprintf(
			"postgres://%s@mcp-sentinel-postgres.%s.svc.cluster.local:5432/%s?sslmode=disable",
			url.UserPassword(postgresUser, postgresPassword).String(),
			core.DefaultAnalyticsNamespace,
			postgresDB,
		)
	}
	platformJWTSecret, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_JWT_SECRET", 32)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformAdminEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_ADMIN_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if envValue := setupSecretEnvValue("MCP_PLATFORM_ADMIN_EMAIL", "PLATFORM_ADMIN_EMAIL"); envValue != "" {
		platformAdminEmail = envValue
	}
	platformAdminPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_ADMIN_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if envValue := setupSecretEnvValue("MCP_PLATFORM_ADMIN_PASSWORD", "PLATFORM_ADMIN_PASSWORD"); envValue != "" {
		platformAdminPassword = envValue
	}
	adminUsers, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "ADMIN_USERS")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	adminUsers = ensureCSVIncludesValues(adminUsers, setupSecretEnvValue("MCP_ADMIN_USERS", "ADMIN_USERS"), platformAdminEmail)
	platformDevLoginEnabled := ""
	platformDevUserEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_USER_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevUserPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_USER_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevAdminEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_ADMIN_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevAdminPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_ADMIN_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if os.Getenv("MCP_RUNTIME_TEST_MODE") == "1" {
		platformDevLoginEnabled = "true"
		if platformDevUserEmail == "" {
			platformDevUserEmail = defaultDevUserEmail
		}
		if platformDevUserPassword == "" {
			platformDevUserPassword = defaultDevUserPassword
		}
		if platformDevAdminEmail == "" {
			platformDevAdminEmail = defaultDevAdminEmail
		}
		if platformDevAdminPassword == "" {
			platformDevAdminPassword = defaultDevAdminPassword
		}
	} else {
		platformDevLoginEnabled = "false"
		platformDevUserEmail = ""
		platformDevUserPassword = ""
		platformDevAdminEmail = ""
		platformDevAdminPassword = ""
	}
	stringData := map[string]string{
		"API_KEYS":                apiKeys,
		"INGEST_API_KEYS":         ingestAPIKeys,
		"ADMIN_API_KEYS":          adminAPIKeys,
		"UI_API_KEY":              uiAPIKey,
		"ADMIN_USERS":             adminUsers,
		"PLATFORM_ADMIN_EMAIL":    platformAdminEmail,
		"PLATFORM_ADMIN_PASSWORD": platformAdminPassword,
		"POSTGRES_USER":           postgresUser,
		"POSTGRES_PASSWORD":       postgresPassword,
		"POSTGRES_DB":             postgresDB,
		"POSTGRES_DSN":            postgresDSN,
		"PLATFORM_JWT_SECRET":     platformJWTSecret,
		"GRAFANA_ADMIN_USER":      "admin",
		"GRAFANA_ADMIN_PASSWORD":  grafanaPassword,
	}
	if platformDevLoginEnabled != "" ||
		platformDevUserEmail != "" ||
		platformDevUserPassword != "" ||
		platformDevAdminEmail != "" ||
		platformDevAdminPassword != "" {
		stringData["PLATFORM_DEV_LOGIN_ENABLED"] = platformDevLoginEnabled
		stringData["PLATFORM_DEV_USER_EMAIL"] = platformDevUserEmail
		stringData["PLATFORM_DEV_USER_PASSWORD"] = platformDevUserPassword
		stringData["PLATFORM_DEV_ADMIN_EMAIL"] = platformDevAdminEmail
		stringData["PLATFORM_DEV_ADMIN_PASSWORD"] = platformDevAdminPassword
	}
	secretManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      "mcp-sentinel-secrets",
			"namespace": core.DefaultAnalyticsNamespace,
		},
		"type":       "Opaque",
		"stringData": stringData,
	}
	rendered, err := yaml.Marshal(secretManifest)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to render analytics secrets: %v", err))
	}
	return string(rendered), nil
}

func ensureCSVIncludes(csv, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(csv)
	}
	parts := make([]string, 0)
	found := false
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == value {
			found = true
		}
		parts = append(parts, part)
	}
	if !found {
		parts = append(parts, value)
	}
	return strings.Join(parts, ",")
}

func ensureCSVIncludesValues(csv string, values ...string) string {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			csv = ensureCSVIncludes(csv, part)
		}
	}
	return csv
}

func setupSecretEnvValue(candidates ...string) string {
	for _, candidate := range candidates {
		if value := strings.TrimSpace(os.Getenv(candidate)); value != "" {
			return value
		}
	}
	return ""
}

func ensureAnalyticsImagePullSecret(kubectl core.KubectlRunner) (string, error) {
	extRegistry, err := registry.ResolveExternalRegistryConfig(nil)
	if err != nil {
		return "", err
	}
	if extRegistry == nil || extRegistry.URL == "" || (extRegistry.Username == "" && extRegistry.Password == "") {
		return "", nil
	}
	if err := ensureImagePullSecretWithKubectl(kubectl, core.DefaultAnalyticsNamespace, defaultRegistrySecretName, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
		return "", err
	}
	return defaultRegistrySecretName, nil
}

func existingSecretDataValue(kubectl core.KubectlRunner, namespace, name, key string) (string, error) {
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

func existingSecretDataValueOrRandom(kubectl core.KubectlRunner, namespace, name, key string, size int) (string, error) {
	value, err := existingSecretDataValue(kubectl, namespace, name, key)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	return randomHex(size)
}

func existingSecretDataValueOrDefault(kubectl core.KubectlRunner, namespace, name, key, fallback string) (string, error) {
	value, err := existingSecretDataValue(kubectl, namespace, name, key)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	return fallback, nil
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

func waitForRolloutStatusWithKubectl(kubectl core.KubectlRunner, kind, name, namespace, timeout string) error {
	return kubectl.RunWithOutput([]string{"rollout", "status", fmt.Sprintf("%s/%s", kind, name), "-n", namespace, "--timeout=" + timeout}, os.Stdout, os.Stderr)
}

// analyticsRolloutTimeoutString returns the kubectl --timeout value for mcp-sentinel rollouts.
// Uses MCP_DEPLOYMENT_TIMEOUT (see core.GetDeploymentTimeout); if unset or non-positive, uses the default 5m.
func analyticsRolloutTimeoutString() string {
	d := core.GetDeploymentTimeout()
	if d <= 0 {
		d = 5 * time.Minute
	}
	return d.String()
}

// printAnalyticsRolloutDiagnostics prints pods and events to help triage stuck mcp-sentinel rollouts.
func printAnalyticsRolloutDiagnostics(kubectl core.KubectlRunner) {
	core.Warn("mcp-sentinel rollouts failed. Namespace snapshot (pods):")
	// #nosec G204 -- fixed namespace for diagnostics.
	_ = kubectl.RunWithOutput([]string{"get", "pods", "-n", core.DefaultAnalyticsNamespace, "-o", "wide"}, os.Stdout, os.Stderr)
	core.Warn("Recent events in mcp-sentinel (newest last):")
	_ = kubectl.RunWithOutput([]string{"get", "events", "-n", core.DefaultAnalyticsNamespace, "--sort-by", ".lastTimestamp"}, os.Stdout, os.Stderr)
}

func waitForJobCompletionWithKubectl(kubectl core.KubectlRunner, name, namespace, timeout string) error {
	return kubectl.RunWithOutput([]string{"wait", "--for=condition=complete", "job/" + name, "-n", namespace, "--timeout=" + timeout}, os.Stdout, os.Stderr)
}

func deleteJobIfExistsWithKubectl(kubectl core.KubectlRunner, name, namespace string) error {
	return kubectl.RunWithOutput([]string{"delete", "job/" + name, "-n", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"}, os.Stdout, os.Stderr)
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
func operatorEnvOverrides(gatewayProxyImage, existingGatewayOTLPEndpoint string) []operatorEnvVar {
	var envVars []operatorEnvVar
	image := strings.TrimSpace(gatewayProxyImage)
	if image == "" {
		image = strings.TrimSpace(core.GetGatewayProxyImageOverride())
	}
	if image != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_GATEWAY_PROXY_IMAGE", Value: image})
	}
	gatewayOTLPEndpoint := strings.TrimSpace(core.GetGatewayOTLPEndpointOverride())
	if gatewayOTLPEndpoint == "" {
		gatewayOTLPEndpoint = strings.TrimSpace(existingGatewayOTLPEndpoint)
	}
	if gatewayOTLPEndpoint == "" {
		gatewayOTLPEndpoint = defaultGatewayOTELExporterOTLPEndpoint
	}
	envVars = append(envVars, operatorEnvVar{Name: gatewayOTELExporterOTLPEndpointEnv, Value: gatewayOTLPEndpoint})
	ingestURL := strings.TrimSpace(core.GetAnalyticsIngestURLOverride())
	if ingestURL == "" {
		ingestURL = defaultAnalyticsIngestURL
	}
	if ingestURL != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_SENTINEL_INGEST_URL", Value: ingestURL})
	}
	if mode := strings.TrimSpace(core.DefaultCLIConfig.IngressReadinessMode); mode != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_INGRESS_READINESS_MODE", Value: mode})
	}
	registryEndpoint := strings.TrimSpace(core.GetRegistryEndpoint())
	if registryEndpoint != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_REGISTRY_ENDPOINT", Value: registryEndpoint})
	}
	registryIngressHost := strings.TrimSpace(core.GetRegistryIngressHost())
	if registryIngressHost != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_REGISTRY_INGRESS_HOST", Value: registryIngressHost})
	}
	if mcpHost := strings.TrimSpace(core.GetMcpIngressHost()); mcpHost != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_DEFAULT_INGRESS_HOST", Value: mcpHost})
		if strings.TrimSpace(core.GetRegistryClusterIssuerName()) != "" {
			envVars = append(envVars,
				operatorEnvVar{Name: "MCP_DEFAULT_INGRESS_ENTRYPOINTS", Value: "websecure"},
				operatorEnvVar{Name: "MCP_DEFAULT_INGRESS_TLS", Value: "true"},
			)
		}
	}
	clusterName := strings.TrimSpace(core.GetClusterName())
	if clusterName != "" {
		envVars = append(envVars, operatorEnvVar{Name: "MCP_CLUSTER_NAME", Value: clusterName})
	}
	return envVars
}

func existingOperatorEnvValue(kubectl core.KubectlRunner, name string) string {
	jsonPath := fmt.Sprintf(
		`jsonpath={.spec.template.spec.containers[?(@.name=="%s")].env[?(@.name=="%s")].value}`,
		core.OperatorManagerContainerName,
		name,
	)
	cmd, err := kubectl.CommandArgs([]string{"get", "deployment/" + core.OperatorDeploymentName, "-n", core.NamespaceMCPRuntime, "-o", jsonPath})
	if err != nil {
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func applySetupPlanToCLIConfig(plan setupplan.Plan) {
	if core.DefaultCLIConfig == nil {
		return
	}
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS && !registryEndpointEnvExplicitlyConfigured() {
		core.DefaultCLIConfig.RegistryEndpoint = fmt.Sprintf("%s.%s.svc.cluster.local:%d", core.RegistryServiceName, core.NamespaceRegistry, core.GetRegistryPort())
	}
	if !plan.TLSEnabled {
		core.DefaultCLIConfig.RegistryClusterIssuerName = ""
		return
	}
	if strings.TrimSpace(plan.ACMEmail) != "" {
		core.DefaultCLIConfig.RegistryClusterIssuerName = certmanager.ClusterIssuerNameForACME(plan.ACMEStaging)
		return
	}
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		core.DefaultCLIConfig.RegistryClusterIssuerName = strings.TrimSpace(plan.TLSClusterIssuer)
		return
	}
	core.DefaultCLIConfig.RegistryClusterIssuerName = certmanager.CertClusterIssuerName
}

func registryEndpointEnvExplicitlyConfigured() bool {
	for _, key := range []string{"MCP_REGISTRY_ENDPOINT", "MCP_REGISTRY_HOST"} {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

// setupTLSWithKubectlAndPlan provisions TLS: Let's Encrypt when plan.ACMEmail is set, an existing
// ClusterIssuer when plan.TLSClusterIssuer is set, otherwise the bundled private CA (mcp-runtime-ca).
func setupTLSWithKubectlAndPlan(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	if strings.TrimSpace(plan.ACMEmail) != "" {
		return setupTLSLetsEncrypt(kubectl, logger, plan)
	}
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		return setupTLSWithExistingClusterIssuer(kubectl, logger, plan)
	}
	return setupTLSPrivateCA(kubectl, logger, plan)
}

func registryCertificateSANs(plan setupplan.Plan) ([]string, []string) {
	dnsNames := append([]string{}, certmanager.ACMETLSDNSNames()...)
	return dedupeStrings(dnsNames), nil
}

func registryInternalCertificateSANs(plan setupplan.Plan) ([]string, []string) {
	var dnsNames []string
	var ipAddresses []string
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		dnsNames = append(dnsNames,
			core.DefaultRegistryIngressHost,
			"registry.registry.svc",
			"registry.registry.svc.cluster.local",
		)
		addRegistrySAN(strings.TrimSpace(core.GetRegistryEndpoint()), &dnsNames, &ipAddresses)
	}
	return dedupeStrings(dnsNames), dedupeStrings(ipAddresses)
}

func bundledRegistryInternalIssuerName(plan setupplan.Plan) string {
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		return strings.TrimSpace(plan.TLSClusterIssuer)
	}
	return certmanager.CertClusterIssuerName
}

func addRegistrySAN(raw string, dnsNames, ipAddresses *[]string) {
	host := registryEndpointHost(raw)
	if host == "" {
		return
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		*ipAddresses = append(*ipAddresses, ip.String())
		return
	}
	*dnsNames = append(*dnsNames, host)
}

func registryEndpointHost(raw string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			trimmed = parsed.Host
		}
	}
	if slash := strings.Index(trimmed, "/"); slash >= 0 {
		trimmed = trimmed[:slash]
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(host, "[]")
	}
	if idx := strings.LastIndex(trimmed, ":"); idx >= 0 && strings.Count(trimmed, ":") == 1 {
		return strings.Trim(trimmed[:idx], "[]")
	}
	return strings.Trim(trimmed, "[]")
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func setupTLSLetsEncrypt(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	core.Info("Configuring TLS with Let's Encrypt (cert-manager HTTP-01)")
	if err := certmanager.ValidateACMEHostnameForPublicCA(); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Invalid configuration for Let's Encrypt")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Invalid configuration for Let's Encrypt")
		}
		return wrappedErr
	}
	if err := certmanager.ValidateIngressManifestForACME(plan.Ingress.Manifest); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Ingress configuration blocks Let's Encrypt")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Ingress configuration blocks Let's Encrypt")
		}
		return wrappedErr
	}
	if plan.InstallCertManager {
		if err := certmanager.EnsureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		core.Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			core.Error("Cert-manager not installed")
			if logger != nil {
				core.LogStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		core.Info("cert-manager CRDs found")
	}
	if err := certmanager.WaitForTraefikDeploymentForACME(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Traefik is not ready for HTTP-01")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Traefik is not ready for HTTP-01")
		}
		return wrappedErr
	}
	core.Info("Checking TCP connectivity to your ACME hostnames on port 80 (best effort from this machine)")
	certmanager.PreflightACMEHostnamesPort80(certmanager.ACMETLSDNSNames())

	core.Info("Applying Let's Encrypt ClusterIssuer")
	if err := certmanager.ApplyLetsEncryptClusterIssuer(kubectl, plan.ACMEmail, plan.ACMEStaging, logger); err != nil {
		return err
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	issuerName := certmanager.ClusterIssuerNameForACME(plan.ACMEStaging)
	dnsNames := certmanager.ACMETLSDNSNames()
	core.Info("Applying Certificate for registry (Let's Encrypt SANs)")
	if err := certmanager.ApplyRegistryCertificateForACME(kubectl, dnsNames, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

// setupTLSWithExistingClusterIssuer issues the registry (and optional mcp SAN) Certificate using a
// ClusterIssuer that already exists in the cluster (internal / enterprise CA).
func setupTLSWithExistingClusterIssuer(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	issuerName := strings.TrimSpace(plan.TLSClusterIssuer)
	core.Info("Configuring TLS with existing ClusterIssuer: " + issuerName)
	if plan.InstallCertManager {
		if err := certmanager.EnsureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		core.Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			core.Error("Cert-manager not installed")
			if logger != nil {
				core.LogStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		core.Info("cert-manager CRDs found")
	}

	if err := certmanager.CheckNamedClusterIssuerWithKubectl(kubectl, issuerName); err != nil {
		core.Error("Cluster issuer not found")
		if logger != nil {
			core.LogStructuredError(logger, err, "Cluster issuer not found")
		}
		return err
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	dnsNames, ipAddresses := registryCertificateSANs(plan)
	if len(dnsNames) == 0 && len(ipAddresses) == 0 {
		err := fmt.Errorf("no DNS names or IP addresses resolved for the Certificate; set MCP_PLATFORM_DOMAIN, MCP_REGISTRY_HOST, or MCP_REGISTRY_INGRESS_HOST (and optional MCP_MCP_INGRESS_HOST)")
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Invalid TLS host configuration")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Invalid TLS host configuration")
		}
		return wrappedErr
	}

	core.Info("Applying Certificate for registry (custom ClusterIssuer)")
	if err := certmanager.ApplyRegistryCertificate(kubectl, dnsNames, ipAddresses, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager and your ClusterIssuer configuration: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

// setupTLSPrivateCA uses mcp-runtime-ca in cert-manager; bundled HTTPS setup
// generates it when missing, while other private-CA paths require it up front.
func setupTLSPrivateCA(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	core.Info("Checking cert-manager installation")
	if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true\n  or run setup with --with-tls --acme-email <addr> to install cert-manager automatically")
		core.Error("Cert-manager not installed")
		if logger != nil {
			core.LogStructuredError(logger, err, "Cert-manager not installed")
		}
		return err
	}
	core.Info("cert-manager CRDs found")

	core.Info("Checking CA secret")
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		created, err := certmanager.EnsureCASecretWithKubectl(kubectl)
		if err != nil {
			err := core.WrapWithSentinel(core.ErrCASecretNotFound, err, "CA secret 'mcp-runtime-ca' could not be generated in cert-manager namespace. Create a private CA manually:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
			core.Error("CA secret unavailable")
			if logger != nil {
				core.LogStructuredError(logger, err, "CA secret unavailable")
			}
			return err
		}
		if created {
			core.Info("Generated cert-manager/mcp-runtime-ca for bundled HTTPS registry TLS; configure every Kubernetes node to trust its tls.crt before pulling from the bundled HTTPS registry")
		}
	} else if err := certmanager.CheckCASecretWithKubectl(kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCASecretNotFound, err, "CA secret 'mcp-runtime-ca' not found in cert-manager namespace. For Let's Encrypt use --acme-email, or create a private CA:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
		core.Error("CA secret not found")
		if logger != nil {
			core.LogStructuredError(logger, err, "CA secret not found")
		}
		return err
	}
	core.Info("CA secret found")

	core.Info("Applying ClusterIssuer")
	if err := certmanager.ApplyClusterIssuerWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply ClusterIssuer: %v", err))
		core.Error("Failed to apply ClusterIssuer")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply ClusterIssuer")
		}
		return wrappedErr
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	core.Info("Applying Certificate for registry")
	var certErr error
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		dnsNames, ipAddresses := registryCertificateSANs(plan)
		certErr = certmanager.ApplyRegistryCertificate(kubectl, dnsNames, ipAddresses, certmanager.CertClusterIssuerName)
	} else {
		certErr = certmanager.ApplyRegistryCertificateWithKubectl(kubectl)
	}
	if certErr != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			certErr,
			fmt.Sprintf("failed to apply Certificate: %v", certErr),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

func setupBundledRegistryInternalTLSStep(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	if plan.RegistryMode != setupplan.RegistryModeBundledHTTPS {
		return nil
	}
	issuerName := bundledRegistryInternalIssuerName(plan)
	if issuerName == certmanager.CertClusterIssuerName {
		core.Info("Ensuring internal registry CA secret")
		created, err := certmanager.EnsureCASecretWithKubectl(kubectl)
		if err != nil {
			err := core.WrapWithSentinel(
				core.ErrCASecretNotFound,
				err,
				"bundled HTTPS registry pulls need an internal CA for registry-internal-tls. Setup could not create cert-manager/mcp-runtime-ca; pass --tls-cluster-issuer for an existing internal issuer or create the CA secret manually",
			)
			core.Error("Internal registry CA secret unavailable")
			if logger != nil {
				core.LogStructuredError(logger, err, "Internal registry CA secret unavailable")
			}
			return err
		}
		if created {
			core.Info("Generated cert-manager/mcp-runtime-ca for internal registry TLS; configure every Kubernetes node to trust its tls.crt before pulling from the bundled HTTPS registry")
		}
		core.Info("Applying internal registry ClusterIssuer")
		if err := certmanager.ApplyClusterIssuerWithKubectl(kubectl); err != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply internal registry ClusterIssuer: %v", err))
			core.Error("Failed to apply internal registry ClusterIssuer")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to apply internal registry ClusterIssuer")
			}
			return wrappedErr
		}
	}

	dnsNames, ipAddresses := registryInternalCertificateSANs(plan)
	core.Info("Applying Certificate for internal registry pod TLS")
	if err := certmanager.ApplyRegistryInternalCertificate(kubectl, dnsNames, ipAddresses, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply internal registry Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryInternalCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply internal registry Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply internal registry Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 2*time.Minute {
		certTimeout = 2 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for internal registry certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryInternalCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("internal registry certificate not ready after %s. Check cert-manager and your internal issuer configuration", certTimeout))
		core.Error("Internal registry certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Internal registry certificate not ready")
		}
		return err
	}
	core.Success("Internal registry certificate issued successfully")
	return nil
}

func ensureRegistryCertificateOwnership(kubectl core.KubectlRunner, logger *zap.Logger) error {
	core.Info("Checking registry TLS Certificate ownership")
	if err := certmanager.RemoveRegistryIngressShimAnnotationWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrTLSSetupFailed,
			err,
			err.Error(),
			map[string]any{"ingress": core.RegistryServiceName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to remove registry ingress-shim annotation")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to remove registry ingress-shim annotation")
		}
		return wrappedErr
	}
	if err := certmanager.CheckRegistryCertificateOwnershipWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrTLSSetupFailed,
			err,
			err.Error(),
			map[string]any{"resource_name": certmanager.RegistryTLSSecretName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Registry TLS Certificate conflict")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Registry TLS Certificate conflict")
		}
		return wrappedErr
	}
	return nil
}
