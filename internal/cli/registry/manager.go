package registry

// This file implements the "registry" command for managing the container registry.
// It handles registry provisioning, status checks, image pushing, and registry information display.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/registry/config"
	"mcp-runtime/internal/cli/registry/ref"
)

const defaultRegistryImage = "registry:2.8.3"
const registryImageOverrideEnv = "MCP_RUNTIME_REGISTRY_IMAGE_OVERRIDE"

// RegistryManager handles registry operations with injected dependencies.
type RegistryManager struct {
	kubectl *core.KubectlClient
	exec    core.Executor
	logger  *zap.Logger
}

// NewRegistryManager creates a RegistryManager with the given dependencies.
func NewRegistryManager(kubectl *core.KubectlClient, exec core.Executor, logger *zap.Logger) *RegistryManager {
	return &RegistryManager{
		kubectl: kubectl,
		exec:    exec,
		logger:  logger,
	}
}

// DefaultRegistryManager returns a RegistryManager using default clients.
func DefaultRegistryManager(logger *zap.Logger) *RegistryManager {
	return NewRegistryManager(core.DefaultKubectlClient(), core.DefaultExecutor(), logger)
}

// RunRegistryProvision contains the registry provision command flow for folder packages.
func RunRegistryProvision(mgr *RegistryManager, url, username, password, operatorImage string, dryRun bool) error {
	flagCfg := &config.ExternalRegistryConfig{
		URL:      url,
		Username: username,
		Password: password,
	}
	cfg, err := resolveExternalRegistryConfig(flagCfg)
	if err != nil {
		return err
	}
	if cfg == nil || cfg.URL == "" {
		err := core.NewWithSentinel(core.ErrRegistryURLRequired, "registry url is required (flag, env PROVISIONED_REGISTRY_URL, or config file)")
		core.Error("Registry URL required")
		core.LogStructuredError(mgr.logger, err, "Registry URL required")
		return err
	}
	if dryRun {
		core.Info(fmt.Sprintf("[dry-run] would save registry config: url=%s username=%q", cfg.URL, cfg.Username))
		if cfg.Username != "" && cfg.Password != "" {
			core.Info(fmt.Sprintf("[dry-run] would docker login to %s", cfg.URL))
		}
		if operatorImage != "" {
			core.Info(fmt.Sprintf("[dry-run] would build and push operator image: %s", operatorImage))
		}
		core.Success("Dry-run complete; no changes made")
		return nil
	}
	if err := config.Save(cfg); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrSaveRegistryConfigFailed, err, fmt.Sprintf("failed to save registry config: %v", err))
		core.Error("Failed to save registry config")
		core.LogStructuredError(mgr.logger, wrappedErr, "Failed to save registry config")
		return wrappedErr
	}
	if cfg.Username != "" && cfg.Password != "" {
		mgr.logger.Info("Performing docker login to external registry", zap.String("url", cfg.URL))
		if err := mgr.LoginRegistry(cfg.URL, cfg.Username, cfg.Password); err != nil {
			return err
		}
	}
	if operatorImage != "" {
		mgr.logger.Info("Building and pushing operator image to external registry", zap.String("image", operatorImage))
		if err := buildOperatorImage(operatorImage); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrBuildOperatorImageFailed,
				err,
				fmt.Sprintf("failed to build operator image: %v", err),
				map[string]any{"image": operatorImage, "component": "registry"},
			)
			core.Error("Failed to build operator image")
			core.LogStructuredError(mgr.logger, wrappedErr, "Failed to build operator image")
			return wrappedErr
		}
		if err := pushOperatorImage(operatorImage); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrPushOperatorImageFailed,
				err,
				fmt.Sprintf("failed to push operator image: %v", err),
				map[string]any{"image": operatorImage, "component": "registry"},
			)
			core.Error("Failed to push operator image")
			core.LogStructuredError(mgr.logger, wrappedErr, "Failed to push operator image")
			return wrappedErr
		}
	}
	mgr.logger.Info("External registry configured", zap.String("url", cfg.URL))
	fmt.Printf("External registry configured: %s\n", cfg.URL)
	return nil
}

// RunRegistryPush contains the registry push command flow for folder packages.
func RunRegistryPush(mgr *RegistryManager, image, registryURL, name, mode, helperNamespace string) error {
	if image == "" {
		err := core.NewWithSentinel(core.ErrImageRequired, "image is required (use --image)")
		core.Error("Image required")
		core.LogStructuredError(mgr.logger, err, "Image required")
		return err
	}
	targetRegistry := registryURL
	if targetRegistry == "" {
		if ext, err := resolveExternalRegistryConfig(nil); err == nil && ext != nil && ext.URL != "" {
			targetRegistry = strings.TrimSuffix(ext.URL, "/")
		}
	}
	if targetRegistry == "" {
		targetRegistry = resolvePlatformRegistryURL(mgr.logger)
	}

	repo, tag := ref.SplitImage(image)
	if name != "" {
		repo = name
	} else {
		repo = ref.DropRegistryPrefix(repo)
	}
	target := targetRegistry + "/" + repo
	if tag != "" {
		target = target + ":" + tag
	}

	mgr.logger.Info("Pushing image", zap.String("source", image), zap.String("target", target))

	switch mode {
	case "direct":
		return mgr.PushDirect(image, target)
	case "in-cluster":
		return mgr.PushInCluster(image, target, helperNamespace)
	default:
		err := core.NewWithSentinel(core.ErrUnknownRegistryMode, fmt.Sprintf("unknown mode %q (use direct|in-cluster)", mode))
		core.Error("Unknown registry mode")
		core.LogStructuredError(mgr.logger, err, "Unknown registry mode")
		return err
	}
}

// resolveExternalRegistryConfig returns the external registry config using precedence:
// CLI flags > environment variables (PROVISIONED_REGISTRY_*) > config file.
// Returns (nil, nil) if no source provides a URL.
func resolveExternalRegistryConfig(flagCfg *config.ExternalRegistryConfig) (*config.ExternalRegistryConfig, error) {
	cfg, err := config.Resolve(flagCfg, registryConfigEnv())
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, config.ErrURLRequired) {
		wrapped := core.NewWithSentinel(core.ErrRegistryURLRequired, "registry url is required")
		core.Error("Registry URL required")
		return nil, wrapped
	}
	if errors.Is(err, config.ErrURLMissingInConfig) {
		return nil, core.NewWithSentinel(core.ErrRegistryURLMissingInConfig, "registry url missing in config")
	}
	return nil, err
}

func ResolveExternalRegistryConfig(flagCfg *config.ExternalRegistryConfig) (*config.ExternalRegistryConfig, error) {
	return resolveExternalRegistryConfig(flagCfg)
}

func registryConfigEnv() config.Env {
	return config.Env{
		URL:      core.DefaultCLIConfig.ProvisionedRegistryURL,
		Username: core.DefaultCLIConfig.ProvisionedRegistryUsername,
		Password: core.DefaultCLIConfig.ProvisionedRegistryPassword,
	}
}

func deployRegistry(logger *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error {
	logger.Info("Deploying container registry", zap.String("namespace", namespace), zap.String("type", registryType))

	if registryType == "" {
		registryType = "docker"
	}

	switch registryType {
	case "docker":
		// continue
	default:
		err := core.NewWithSentinel(core.ErrUnsupportedRegistryType, fmt.Sprintf("unsupported registry type %q (supported: docker; harbor coming soon)", registryType))
		core.Error("Unsupported registry type")
		core.LogStructuredError(logger, err, "Unsupported registry type")
		return err
	}

	if manifestPath == "" {
		manifestPath = "config/registry"
	}

	// Ensure Namespace
	if err := ensureNamespace(namespace); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrEnsureNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure namespace: %v", err),
			map[string]any{"namespace": namespace, "component": "registry"},
		)
		core.Error("Failed to ensure namespace")
		core.LogStructuredError(logger, wrappedErr, "Failed to ensure namespace")
		return wrappedErr
	}
	// Apply registry manifests via kustomize with namespace override
	logger.Info("Applying registry manifests")
	overrideImage := strings.TrimSpace(os.Getenv(registryImageOverrideEnv))
	manifest, err := renderKustomizeManifest(core.DefaultKubectlClient(), manifestPath)
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrDeployRegistryFailed,
			err,
			fmt.Sprintf("failed to render registry manifest %q: %v", manifestPath, err),
			map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
		)
		core.Error("Failed to render registry manifest")
		core.LogStructuredError(logger, wrappedErr, "Failed to render registry manifest")
		return wrappedErr
	}
	manifest = rewriteRegistryHost(manifest, core.GetRegistryIngressHost())
	issuer := core.GetRegistryClusterIssuerName()
	manifest = rewriteRegistryClusterIssuerAnnotation(manifest, issuer)
	if s := strings.TrimSpace(issuer); s != "" && !strings.Contains(manifest, "cert-manager.io/cluster-issuer: "+s) {
		logger.Warn("registry manifest does not show expected cert-manager.io/cluster-issuer; ingress TLS issuer may be wrong (check overlay for cert-manager.io/cluster-issuer: mcp-runtime-ca)",
			zap.String("expected_issuer", s))
	}
	if overrideImage != "" {
		logger.Info("Applying registry image override", zap.String("image", overrideImage))
		updated := strings.Replace(manifest, "image: "+defaultRegistryImage, "image: "+overrideImage, 1)
		if updated == manifest {
			err := fmt.Errorf("registry image reference %q not found in manifest", defaultRegistryImage)
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrDeployRegistryFailed,
				err,
				err.Error(),
				map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
			)
			core.Error("Failed to rewrite registry image")
			core.LogStructuredError(logger, wrappedErr, "Failed to rewrite registry image")
			return wrappedErr
		}
		if err := kube.ApplyManifestContentWithNamespace(core.DefaultKubectlClient().CommandArgs, updated, namespace); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrDeployRegistryFailed,
				err,
				fmt.Sprintf("failed to deploy registry with image override %q: %v", overrideImage, err),
				map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
			)
			core.Error("Failed to deploy registry")
			core.LogStructuredError(logger, wrappedErr, "Failed to deploy registry")
			return wrappedErr
		}
	} else {
		if err := kube.ApplyManifestContentWithNamespace(core.DefaultKubectlClient().CommandArgs, manifest, namespace); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrDeployRegistryFailed,
				err,
				fmt.Sprintf("failed to deploy registry: %v", err),
				map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
			)
			core.Error("Failed to deploy registry")
			core.LogStructuredError(logger, wrappedErr, "Failed to deploy registry")
			return wrappedErr
		}
	}

	if err := ensureRegistryStorageSize(logger, namespace, registryStorageSize); err != nil {
		return err
	}

	// Wait for registry to be ready
	logger.Info("Waiting for registry to be ready")
	deployTimeout := 5 * time.Minute
	if err := waitForDeploymentAvailable(logger, "registry", namespace, "app=registry", deployTimeout); err != nil {
		logger.Warn("Registry deployment may still be in progress", zap.Error(err))
	}

	logger.Info("Registry deployed successfully")
	return nil
}

func DeployRegistry(logger *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error {
	return deployRegistry(logger, namespace, port, registryType, registryStorageSize, manifestPath)
}

func ensureNamespace(namespace string) error {
	return kube.EnsureNamespace(core.DefaultKubectlClient().CommandArgs, namespace)
}

func buildOperatorImage(image string) error {
	cmd, err := core.ExecCommandWithValidators("make", []string{"-f", "Makefile.operator", "docker-build-operator-no-test", "IMG=" + image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func pushOperatorImage(image string) error {
	cmd, err := core.ExecCommandWithValidators("docker", []string{"push", image})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func waitForDeploymentAvailable(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
	if logger != nil {
		logger.Info("Waiting for deployment rollout", zap.String("deployment", name), zap.String("namespace", namespace), zap.String("selector", selector))
	}
	return core.DefaultKubectlClient().RunWithOutput([]string{
		"rollout", "status",
		"deployment/" + name,
		"-n", namespace,
		"--timeout=" + timeout.String(),
	}, os.Stdout, os.Stderr)
}

func rewriteRegistryHost(manifest, host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "registry.local" {
		return manifest
	}
	return strings.ReplaceAll(manifest, "registry.local", host)
}

// rewriteRegistryClusterIssuerAnnotation sets cert-manager.io/cluster-issuer on the registry Ingress when
// the TLS overlay is used (value is mcp-runtime-ca in git; replaced during setup for Let's Encrypt or private CA).
func rewriteRegistryClusterIssuerAnnotation(manifest, issuerName string) string {
	issuerName = strings.TrimSpace(issuerName)
	if issuerName == "" {
		return manifest
	}
	const oldLine = "cert-manager.io/cluster-issuer: mcp-runtime-ca"
	newLine := "cert-manager.io/cluster-issuer: " + issuerName
	if !strings.Contains(manifest, oldLine) {
		return manifest
	}
	return strings.ReplaceAll(manifest, oldLine, newLine)
}

func renderKustomizeManifest(kubectl core.KubectlRunner, manifestPath string) (string, error) {
	renderCmd, err := kubectl.CommandArgs([]string{"kustomize", manifestPath})
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	renderCmd.SetStdout(&stdout)
	renderCmd.SetStderr(&stderr)
	if err := renderCmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl kustomize %s failed: %v (%s)", manifestPath, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func ensureRegistryStorageSize(logger *zap.Logger, namespace, registryStorageSize string) error {
	storageSize := strings.TrimSpace(registryStorageSize)
	if storageSize == "" {
		return nil
	}

	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	getCmd, err := core.DefaultKubectlClient().CommandArgs([]string{"get", "pvc", core.RegistryPVCName, "-n", namespace, "-o", "jsonpath={.spec.resources.requests.storage}"})
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	getCmd.SetStdout(&stdout)
	getCmd.SetStderr(&stderr)
	if err := getCmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrReadRegistryStorageFailed,
			err,
			fmt.Sprintf("failed to read current registry storage size: %v (%s)", err, strings.TrimSpace(stderr.String())),
			map[string]any{"namespace": namespace, "pvc": core.RegistryPVCName, "component": "registry"},
		)
		core.Error("Failed to read registry storage size")
		core.LogStructuredError(logger, wrappedErr, "Failed to read registry storage size")
		return wrappedErr
	}

	currentSize := strings.TrimSpace(stdout.String())
	if currentSize == storageSize {
		logger.Info("Registry storage size already matches requested value", zap.String("size", storageSize))
		return nil
	}

	logger.Info("Updating registry storage size", zap.String("from", currentSize), zap.String("to", storageSize))
	patchPayload := fmt.Sprintf(`{"spec":{"resources":{"requests":{"storage":"%s"}}}}`, storageSize)
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := core.DefaultKubectlClient().RunWithOutput([]string{"patch", "pvc", core.RegistryPVCName, "-n", namespace, "-p", patchPayload}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrUpdateRegistryStorageFailed,
			err,
			fmt.Sprintf("failed to update registry storage size to %s: %v", storageSize, err),
			map[string]any{"namespace": namespace, "pvc": core.RegistryPVCName, "storage_size": storageSize, "component": "registry"},
		)
		core.Error("Failed to update registry storage size")
		core.LogStructuredError(logger, wrappedErr, "Failed to update registry storage size")
		return wrappedErr
	}

	return nil
}

// CheckRegistryStatus checks and displays registry status.
func (m *RegistryManager) CheckRegistryStatus(namespace string) error {
	m.logger.Info("Checking registry status")

	core.Header("Registry Status")
	core.DefaultPrinter.Println()

	// Get deployment status
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	readyOut, err := m.kubectl.Output([]string{"get", "deployment", core.RegistryDeploymentName, "-n", namespace, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		core.Error("Registry deployment not found")
		return err
	}

	// Get service IP
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	ipOut, _ := m.kubectl.Output([]string{"get", "service", core.RegistryServiceName, "-n", namespace, "-o", "jsonpath={.spec.clusterIP}:{.spec.ports[0].port}"})

	// Get pod status
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	podOut, _ := m.kubectl.Output([]string{"get", "pods", "-n", namespace, "-l", core.SelectorRegistry, "-o", "jsonpath={.items[0].status.phase}"})

	// Build status table
	replicas := strings.TrimSpace(string(readyOut))
	status := core.Green("Healthy")
	if replicas == "" || strings.HasPrefix(replicas, "/") || strings.HasPrefix(replicas, "0/") {
		status = core.Yellow("Starting")
	}

	tableData := [][]string{
		{"Property", "Value"},
		{"Status", status},
		{"Replicas", replicas},
		{"Endpoint", strings.TrimSpace(string(ipOut))},
		{"Pod Phase", strings.TrimSpace(string(podOut))},
	}

	core.TableBoxed(tableData)

	return nil
}

// LoginRegistry logs into a container registry.
func (m *RegistryManager) LoginRegistry(registryURL, username, password string) error {
	m.logger.Info("Logging into registry", zap.String("url", registryURL))

	// #nosec G204 -- credentials from validated config; password via stdin (not command line).
	cmd, err := m.exec.Command("docker", []string{"login", "-u", username, "--password-stdin", registryURL})
	if err != nil {
		return err
	}
	cmd.SetStdin(strings.NewReader(password))
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	if err := cmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrRegistryLoginFailed,
			err,
			fmt.Sprintf("failed to login to registry: %v", err),
			map[string]any{"registry_url": registryURL, "component": "registry"},
		)
		core.Error("Failed to login to registry")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to login to registry")
		return wrappedErr
	}

	m.logger.Info("Successfully logged into registry")
	return nil
}

// ShowRegistryInfo displays registry connection information.
func (m *RegistryManager) ShowRegistryInfo() error {
	ns := core.NamespaceRegistry
	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	ingressHost, err := m.kubectl.Output([]string{"get", "ingress", core.RegistryServiceName, "-n", ns, "-o", "jsonpath={.spec.rules[0].host}"})
	if err != nil {
		m.logger.Debug("Failed to get registry ingress host", zap.Error(err))
	}

	// Get registry service
	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	clusterIP, err := m.kubectl.Output([]string{"get", "service", core.RegistryServiceName, "-n", ns, "-o", "jsonpath={.spec.clusterIP}"})
	if err != nil {
		m.logger.Debug("Failed to get registry cluster IP", zap.Error(err))
	}

	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	port, err := m.kubectl.Output([]string{"get", "service", core.RegistryServiceName, "-n", ns, "-o", "jsonpath={.spec.ports[0].port}"})
	if err != nil {
		m.logger.Debug("Failed to get registry port", zap.Error(err))
	}

	if len(clusterIP) > 0 && len(port) > 0 {
		core.Header("Registry Information")
		core.DefaultPrinter.Println()

		ip := strings.TrimSpace(string(clusterIP))
		p := strings.TrimSpace(string(port))
		host := strings.TrimSpace(string(ingressHost))

		tableData := [][]string{
			{"Property", "Value"},
			{"Ingress Host", host},
			{"Internal URL", fmt.Sprintf("%s:%s", ip, p)},
			{"Service DNS", fmt.Sprintf("registry.registry.svc.cluster.local:%s", p)},
		}
		core.TableBoxed(tableData)

		core.DefaultPrinter.Println()
		core.Section("Local Access")
		if host != "" {
			core.Info("Option 1: Use the ingress host:")
			core.DefaultPrinter.Printf("  %s\n", host)
			core.DefaultPrinter.Println()
			core.Info("If running without TLS, add the ingress host to your runtime's insecure registry list.")
			core.DefaultPrinter.Println()
		}
		core.Info("Option 2: Add the internal service IP to /etc/docker/daemon.json:")
		core.DefaultPrinter.Printf("  \"insecure-registries\": [\"%s:%s\"]\n", ip, p)
		core.DefaultPrinter.Println()
		core.Info("Option 3: Use port-forward:")
		core.DefaultPrinter.Printf("  kubectl port-forward -n registry svc/registry %s:%s\n", p, p)
		core.DefaultPrinter.Printf("  Then use: localhost:%s\n", p)
	} else {
		core.Warn("Registry not found. Deploy it with: mcp-runtime setup")
	}

	return nil
}

// PushDirect pushes an image directly using docker.
func (m *RegistryManager) PushDirect(source, target string) error {
	// #nosec G204 -- source/target are image references from internal push logic.
	tagCmd, err := m.exec.Command("docker", []string{"tag", source, target})
	if err != nil {
		return err
	}
	tagCmd.SetStdout(os.Stdout)
	tagCmd.SetStderr(os.Stderr)
	if err := tagCmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrTagImageFailed,
			err,
			fmt.Sprintf("failed to tag image: %v", err),
			map[string]any{"source": source, "target": target, "component": "registry"},
		)
		core.Error("Failed to tag image")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to tag image")
		return wrappedErr
	}

	// #nosec G204 -- target is image reference from internal push logic.
	pushCmd, err := m.exec.Command("docker", []string{"push", target})
	if err != nil {
		return err
	}
	pushCmd.SetStdout(os.Stdout)
	pushCmd.SetStderr(os.Stderr)
	if err := pushCmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushImageFailed,
			err,
			fmt.Sprintf("failed to push image: %v", err),
			map[string]any{"target": target, "component": "registry"},
		)
		core.Error("Failed to push image")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to push image")
		return wrappedErr
	}

	core.Success(fmt.Sprintf("Pushed %s", target))
	return nil
}

// PushInCluster pushes an image using an in-cluster helper pod.
func (m *RegistryManager) PushInCluster(source, target, helperNS string) error {
	helperName := fmt.Sprintf("registry-pusher-%d", time.Now().UnixNano())

	// #nosec G204 -- helperNS from CLI flag, kubectl validates namespace names.
	if err := m.kubectl.Run([]string{"get", "namespace", helperNS}); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrHelperNamespaceNotFound,
			err,
			fmt.Sprintf("helper namespace %q not found (create it or pass --namespace): %v", helperNS, err),
			map[string]any{"namespace": helperNS, "component": "registry"},
		)
		core.Error("Helper namespace not found")
		core.LogStructuredError(m.logger, wrappedErr, "Helper namespace not found")
		return wrappedErr
	}

	// Ensure source is saved to tar; use CWD to satisfy kubectl path validation.
	tmpFile, err := os.CreateTemp(".", "mcp-img-*.tar")
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCreateTempFileFailed, err, fmt.Sprintf("failed to create temp file: %v", err))
		core.Error("Failed to create temp file")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create temp file")
		return wrappedErr
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCloseTempFileFailed, err, fmt.Sprintf("failed to close temp file: %v", err))
		core.Error("Failed to close temp file")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to close temp file")
		return wrappedErr
	}
	defer os.Remove(tmpPath)

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	saveCmd, err := m.exec.Command("docker", []string{"save", "-o", tmpPath, source})
	if err != nil {
		return err
	}
	saveCmd.SetStdout(os.Stdout)
	saveCmd.SetStderr(os.Stderr)
	if err := saveCmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrSaveImageFailed,
			err,
			fmt.Sprintf("failed to save image: %v", err),
			map[string]any{"source": source, "component": "registry"},
		)
		core.Error("Failed to save image")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to save image")
		return wrappedErr
	}

	// Start helper pod with skopeo
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"run", helperName, "-n", helperNS, "--image=" + core.GetSkopeoImage(), "--restart=Never", "--command", "--", "sh", "-c", "while true; do sleep 3600; done"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrStartHelperPodFailed,
			err,
			fmt.Sprintf("failed to start helper pod: %v", err),
			map[string]any{"pod": helperName, "namespace": helperNS, "component": "registry"},
		)
		core.Error("Failed to start helper pod")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to start helper pod")
		return wrappedErr
	}
	defer func() {
		// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
		_ = m.kubectl.Run([]string{"delete", "pod", helperName, "-n", helperNS, "--ignore-not-found"})
	}()

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	timeout := core.GetHelperPodTimeout()
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if err := m.kubectl.RunWithOutput([]string{"wait", "--for=condition=Ready", "pod/" + helperName, "-n", helperNS, "--timeout=" + timeout.String()}, os.Stdout, os.Stderr); err != nil {
		// Best-effort diagnostics for common real-cluster failures (DiskPressure, taints, quotas, etc).
		_ = m.kubectl.RunWithOutput([]string{"describe", "pod", helperName, "-n", helperNS, "--request-timeout=10s"}, os.Stdout, os.Stderr)
		_ = m.kubectl.RunWithOutput([]string{"get", "events", "-n", helperNS, "--request-timeout=10s", "--field-selector", "involvedObject.name=" + helperName, "--sort-by=.lastTimestamp"}, os.Stdout, os.Stderr)
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrHelperPodNotReady,
			err,
			fmt.Sprintf("helper pod not ready: %v", err),
			map[string]any{"pod": helperName, "namespace": helperNS, "timeout": timeout.String(), "component": "registry"},
		)
		core.Error("Helper pod not ready")
		core.LogStructuredError(m.logger, wrappedErr, "Helper pod not ready")
		return wrappedErr
	}

	// Copy tar into pod
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"cp", tmpPath, fmt.Sprintf("%s/%s:%s", helperNS, helperName, "/tmp/image.tar")}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCopyImageToHelperFailed,
			err,
			fmt.Sprintf("failed to copy image tar to helper pod: %v", err),
			map[string]any{"pod": helperName, "namespace": helperNS, "component": "registry"},
		)
		core.Error("Failed to copy image to helper pod")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to copy image to helper pod")
		return wrappedErr
	}

	// The helper pod uses cluster DNS, which does not resolve the external ingress host
	// (e.g. registry.local). Rewrite the destination host to the in-cluster registry
	// service DNS so skopeo can reach the registry from inside the cluster. The Docker
	// registry stores images by repository path, so the resulting image is still
	// addressable via any hostname that routes to the same registry service.
	pushTarget := rewriteTargetHostForInClusterPush(target, m.kubectl)

	// Push using skopeo from inside cluster (registry is http, so disable tls verify)
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"exec", "-n", helperNS, helperName, "--",
		"skopeo", "copy", "--dest-tls-verify=false", "docker-archive:/tmp/image.tar", "docker://" + pushTarget}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrPushImageFromHelperFailed,
			err,
			fmt.Sprintf("failed to push image from helper pod: %v", err),
			map[string]any{"pod": helperName, "namespace": helperNS, "target": target, "push_target": pushTarget, "component": "registry"},
		)
		core.Error("Failed to push image from helper pod")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to push image from helper pod")
		return wrappedErr
	}

	core.Success(fmt.Sprintf("Pushed %s via in-cluster helper", target))
	return nil
}

// rewriteTargetHostForInClusterPush replaces the host portion of an image reference
// with the in-cluster registry service DNS when the target points at the bundled
// internal registry (identified by the configured endpoint or ingress host). Image
// data in a Docker registry is keyed by repository path, so pushing via the service
// DNS stores the image at the same repo path, leaving the original hostname (e.g. the
// ingress host) usable for subsequent pulls. Targets outside the internal registry
// (e.g. a user-provided external registry) are returned unchanged.
func rewriteTargetHostForInClusterPush(target string, kubectl *core.KubectlClient) string {
	slash := strings.Index(target, "/")
	if slash <= 0 {
		return target
	}
	host := target[:slash]
	rest := target[slash:]

	lowerHost := strings.ToLower(host)
	if strings.Contains(lowerHost, ".svc.cluster.local") {
		return target
	}

	hostNoPort := lowerHost
	if idx := strings.LastIndex(hostNoPort, ":"); idx >= 0 {
		hostNoPort = hostNoPort[:idx]
	}

	internal := map[string]struct{}{}
	if ep := strings.ToLower(strings.TrimSpace(core.GetRegistryEndpoint())); ep != "" {
		if idx := strings.LastIndex(ep, ":"); idx >= 0 {
			ep = ep[:idx]
		}
		internal[ep] = struct{}{}
	}
	if ih := strings.ToLower(strings.TrimSpace(core.GetRegistryIngressHost())); ih != "" {
		internal[ih] = struct{}{}
	}

	if _, ok := internal[hostNoPort]; !ok {
		return target
	}

	port := core.GetRegistryPort()
	if kubectl != nil {
		// #nosec G204 -- fixed arguments, no user input.
		if portCmd, err := kubectl.CommandArgs([]string{"get", "service", core.RegistryServiceName, "-n", core.NamespaceRegistry, "-o", "jsonpath={.spec.ports[0].port}"}); err == nil {
			if out, err := portCmd.Output(); err == nil {
				if p := strings.TrimSpace(string(out)); p != "" {
					port = parsePortOrDefault(p, port)
				}
			}
		}
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d%s", core.RegistryServiceName, core.NamespaceRegistry, port, rest)
}

func parsePortOrDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return def
	}
	return n
}
