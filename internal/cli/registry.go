package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// RegistryManager handles registry operations with injected dependencies.
type RegistryManager struct {
	kubectl *KubectlClient
	exec    Executor
	logger  *zap.Logger
}

// NewRegistryManager creates a RegistryManager with the given dependencies.
func NewRegistryManager(kubectl *KubectlClient, exec Executor, logger *zap.Logger) *RegistryManager {
	return &RegistryManager{
		kubectl: kubectl,
		exec:    exec,
		logger:  logger,
	}
}

// DefaultRegistryManager returns a RegistryManager using default clients.
func DefaultRegistryManager(logger *zap.Logger) *RegistryManager {
	return NewRegistryManager(kubectlClient, execExecutor, logger)
}

// NewRegistryCmd builds the registry subcommand for managing registry lifecycle.
func NewRegistryCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultRegistryManager(logger)
	return NewRegistryCmdWithManager(mgr)
}

// NewRegistryCmdWithManager returns the registry subcommand using the provided manager.
func NewRegistryCmdWithManager(mgr *RegistryManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage container registry",
		Long:  "Commands for managing the container registry",
	}

	cmd.AddCommand(mgr.newRegistryStatusCmd())
	cmd.AddCommand(mgr.newRegistryInfoCmd())
	cmd.AddCommand(mgr.newRegistryProvisionCmd())
	cmd.AddCommand(mgr.newRegistryPushCmd())

	return cmd
}

func (m *RegistryManager) newRegistryStatusCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check registry status",
		Long:  "Check the status of the container registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.CheckRegistryStatus(namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceRegistry, "Registry namespace")

	return cmd
}

func (m *RegistryManager) newRegistryInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show registry information",
		Long:  "Show registry URL and connection information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ShowRegistryInfo()
		},
	}

	return cmd
}

func (m *RegistryManager) newRegistryProvisionCmd() *cobra.Command {
	var url string
	var username string
	var password string
	var operatorImage string

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Configure an external registry",
		Long:  "Configure an external registry to be used for operator/runtime images",
		RunE: func(cmd *cobra.Command, args []string) error {
			flagCfg := &ExternalRegistryConfig{
				URL:      url,
				Username: username,
				Password: password,
			}
			cfg, err := resolveExternalRegistryConfig(flagCfg)
			if err != nil {
				return err
			}
			if cfg == nil || cfg.URL == "" {
				return fmt.Errorf("registry url is required (flag, env PROVISIONED_REGISTRY_URL, or config file)")
			}
			if err := saveExternalRegistryConfig(cfg); err != nil {
				return fmt.Errorf("failed to save registry config: %w", err)
			}
			if cfg.Username != "" && cfg.Password != "" {
				m.logger.Info("Performing docker login to external registry", zap.String("url", cfg.URL))
				if err := m.LoginRegistry(cfg.URL, cfg.Username, cfg.Password); err != nil {
					return err
				}
			}
			if operatorImage != "" {
				m.logger.Info("Building and pushing operator image to external registry", zap.String("image", operatorImage))
				if err := buildOperatorImage(operatorImage); err != nil {
					return fmt.Errorf("failed to build operator image: %w", err)
				}
				if err := pushOperatorImage(operatorImage); err != nil {
					return fmt.Errorf("failed to push operator image: %w", err)
				}
			}
			m.logger.Info("External registry configured", zap.String("url", cfg.URL))
			fmt.Printf("External registry configured: %s\n", cfg.URL)
			return nil
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "External registry URL (e.g., registry.example.com)")
	cmd.Flags().StringVar(&username, "username", "", "Registry username (optional)")
	cmd.Flags().StringVar(&password, "password", "", "Registry password (optional)")
	cmd.Flags().StringVar(&operatorImage, "operator-image", "", "Optional: build and push operator image to this external registry (e.g., <registry>/mcp-runtime-operator:latest)")

	return cmd
}

func (m *RegistryManager) newRegistryPushCmd() *cobra.Command {
	var image string
	var registryURL string
	var name string
	var mode string
	var helperNamespace string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Retag and push an image to the platform or provisioned registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if image == "" {
				return fmt.Errorf("image is required (use --image)")
			}
			targetRegistry := registryURL
			if targetRegistry == "" {
				if ext, err := resolveExternalRegistryConfig(nil); err == nil && ext != nil && ext.URL != "" {
					targetRegistry = strings.TrimSuffix(ext.URL, "/")
				}
			}
			if targetRegistry == "" {
				targetRegistry = getPlatformRegistryURL(m.logger)
			}

			repo, tag := splitImage(image)
			if name != "" {
				repo = name
			} else {
				repo = dropRegistryPrefix(repo)
			}
			target := targetRegistry + "/" + repo
			if tag != "" {
				target = target + ":" + tag
			}

			m.logger.Info("Pushing image", zap.String("source", image), zap.String("target", target))

			switch mode {
			case "direct":
				return m.PushDirect(image, target)
			case "in-cluster":
				return m.PushInCluster(image, target, helperNamespace)
			default:
				return fmt.Errorf("unknown mode %q (use direct|in-cluster)", mode)
			}
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Local image to push (required)")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Target registry (defaults to provisioned or internal)")
	cmd.Flags().StringVar(&name, "name", "", "Override target repo/name (default: source name without registry)")
	cmd.Flags().StringVar(&mode, "mode", "in-cluster", "Push mode: in-cluster (default, uses skopeo helper) or direct (docker push)")
	cmd.Flags().StringVar(&helperNamespace, "namespace", NamespaceRegistry, "Namespace to run the in-cluster helper pod")

	return cmd
}

type ExternalRegistryConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

func registryConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mcp-runtime", "registry.yaml"), nil
}

func saveExternalRegistryConfig(cfg *ExternalRegistryConfig) error {
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("registry url is required")
	}
	path, err := registryConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadExternalRegistryConfig() (*ExternalRegistryConfig, error) {
	path, err := registryConfigPath()
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path is scoped to the user's config directory.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg ExternalRegistryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("registry url missing in config")
	}
	return &cfg, nil
}

// resolveExternalRegistryConfig returns the external registry config using precedence:
// CLI flags > environment variables (PROVISIONED_REGISTRY_*) > config file.
// Returns (nil, nil) if no source provides a URL.
func resolveExternalRegistryConfig(flagCfg *ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
	var cfg ExternalRegistryConfig
	sourceFound := false

	if fileCfg, err := loadExternalRegistryConfig(); err == nil && fileCfg != nil {
		cfg = *fileCfg
		if cfg.URL != "" {
			sourceFound = true
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Load from CLIConfig (which reads from env vars at startup)
	if DefaultCLIConfig.ProvisionedRegistryURL != "" {
		cfg.URL = DefaultCLIConfig.ProvisionedRegistryURL
		sourceFound = true
	}
	if DefaultCLIConfig.ProvisionedRegistryUsername != "" {
		cfg.Username = DefaultCLIConfig.ProvisionedRegistryUsername
		sourceFound = true
	}
	if DefaultCLIConfig.ProvisionedRegistryPassword != "" {
		cfg.Password = DefaultCLIConfig.ProvisionedRegistryPassword
		sourceFound = true
	}

	if flagCfg != nil {
		if flagCfg.URL != "" {
			cfg.URL = flagCfg.URL
			sourceFound = true
		}
		if flagCfg.Username != "" {
			cfg.Username = flagCfg.Username
			sourceFound = true
		}
		if flagCfg.Password != "" {
			cfg.Password = flagCfg.Password
			sourceFound = true
		}
	}

	if cfg.URL == "" {
		if sourceFound {
			return nil, fmt.Errorf("registry url is required")
		}
		return nil, nil
	}

	return &cfg, nil
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
		return fmt.Errorf("unsupported registry type %q (supported: docker; harbor coming soon)", registryType)
	}

	if manifestPath == "" {
		manifestPath = "config/registry"
	}

	// Ensure Namespace
	if err := ensureNamespace(namespace); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}
	// Apply registry manifests via kustomize with namespace override
	logger.Info("Applying registry manifests")
	// #nosec G204 -- manifestPath from internal config, namespace from setup flags.
	if err := kubectlClient.RunWithOutput([]string{"apply", "-k", manifestPath, "-n", namespace}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to deploy registry: %w", err)
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

func ensureRegistryStorageSize(logger *zap.Logger, namespace, registryStorageSize string) error {
	storageSize := strings.TrimSpace(registryStorageSize)
	if storageSize == "" {
		return nil
	}

	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	getCmd, err := kubectlClient.CommandArgs([]string{"get", "pvc", RegistryPVCName, "-n", namespace, "-o", "jsonpath={.spec.resources.requests.storage}"})
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	getCmd.SetStdout(&stdout)
	getCmd.SetStderr(&stderr)
	if err := getCmd.Run(); err != nil {
		return fmt.Errorf("failed to read current registry storage size: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	currentSize := strings.TrimSpace(stdout.String())
	if currentSize == storageSize {
		logger.Info("Registry storage size already matches requested value", zap.String("size", storageSize))
		return nil
	}

	logger.Info("Updating registry storage size", zap.String("from", currentSize), zap.String("to", storageSize))
	patchPayload := fmt.Sprintf(`{"spec":{"resources":{"requests":{"storage":"%s"}}}}`, storageSize)
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := kubectlClient.RunWithOutput([]string{"patch", "pvc", RegistryPVCName, "-n", namespace, "-p", patchPayload}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to update registry storage size to %s: %w", storageSize, err)
	}

	return nil
}

// CheckRegistryStatus checks and displays registry status.
func (m *RegistryManager) CheckRegistryStatus(namespace string) error {
	m.logger.Info("Checking registry status")

	Header("Registry Status")
	DefaultPrinter.Println()

	// Get deployment status
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	readyOut, err := m.kubectl.Output([]string{"get", "deployment", RegistryDeploymentName, "-n", namespace, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		Error("Registry deployment not found")
		return err
	}

	// Get service IP
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	ipOut, _ := m.kubectl.Output([]string{"get", "service", RegistryServiceName, "-n", namespace, "-o", "jsonpath={.spec.clusterIP}:{.spec.ports[0].port}"})

	// Get pod status
	// #nosec G204 -- fixed kubectl command, namespace from internal config.
	podOut, _ := m.kubectl.Output([]string{"get", "pods", "-n", namespace, "-l", SelectorRegistry, "-o", "jsonpath={.items[0].status.phase}"})

	// Build status table
	replicas := strings.TrimSpace(string(readyOut))
	status := Green("Healthy")
	if replicas == "" || strings.HasPrefix(replicas, "/") || strings.HasPrefix(replicas, "0/") {
		status = Yellow("Starting")
	}

	tableData := [][]string{
		{"Property", "Value"},
		{"Status", status},
		{"Replicas", replicas},
		{"Endpoint", strings.TrimSpace(string(ipOut))},
		{"Pod Phase", strings.TrimSpace(string(podOut))},
	}

	TableBoxed(tableData)

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
		return fmt.Errorf("failed to login to registry: %w", err)
	}

	m.logger.Info("Successfully logged into registry")
	return nil
}

// ShowRegistryInfo displays registry connection information.
func (m *RegistryManager) ShowRegistryInfo() error {
	ns := NamespaceRegistry
	// Get registry service
	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	clusterIP, err := m.kubectl.Output([]string{"get", "service", RegistryServiceName, "-n", ns, "-o", "jsonpath={.spec.clusterIP}"})
	if err != nil {
		m.logger.Debug("Failed to get registry cluster IP", zap.Error(err))
	}

	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	port, err := m.kubectl.Output([]string{"get", "service", RegistryServiceName, "-n", ns, "-o", "jsonpath={.spec.ports[0].port}"})
	if err != nil {
		m.logger.Debug("Failed to get registry port", zap.Error(err))
	}

	if len(clusterIP) > 0 && len(port) > 0 {
		Header("Registry Information")
		DefaultPrinter.Println()

		ip := strings.TrimSpace(string(clusterIP))
		p := strings.TrimSpace(string(port))

		tableData := [][]string{
			{"Property", "Value"},
			{"Internal URL", fmt.Sprintf("%s:%s", ip, p)},
			{"Service DNS", fmt.Sprintf("registry.registry.svc.cluster.local:%s", p)},
		}
		TableBoxed(tableData)

		DefaultPrinter.Println()
		Section("Local Access")
		Info("Option 1: Add to /etc/docker/daemon.json:")
		DefaultPrinter.Printf("  \"insecure-registries\": [\"%s:%s\"]\n", ip, p)
		DefaultPrinter.Println()
		Info("Option 2: Use port-forward:")
		DefaultPrinter.Printf("  kubectl port-forward -n registry svc/registry %s:%s\n", p, p)
		DefaultPrinter.Printf("  Then use: localhost:%s\n", p)
	} else {
		Warn("Registry not found. Deploy it with: mcp-runtime setup")
	}

	return nil
}

// loginRegistry is a package-level helper for backward compatibility.
func loginRegistry(logger *zap.Logger, registryURL, username, password string) error {
	mgr := DefaultRegistryManager(logger)
	return mgr.LoginRegistry(registryURL, username, password)
}

func splitImage(image string) (string, string) {
	tag := ""
	parts := strings.Split(image, ":")
	if len(parts) > 1 && !strings.Contains(parts[len(parts)-1], "/") {
		tag = parts[len(parts)-1]
		image = strings.Join(parts[:len(parts)-1], ":")
	}
	return image, tag
}

// dropRegistryPrefix removes registry prefix from image repository name
// Example: "registry.example.com/my-image" -> "my-image"
func dropRegistryPrefix(repo string) string {
	parts := strings.Split(repo, "/")
	if len(parts) <= 1 {
		return repo
	}
	first := parts[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return strings.Join(parts[1:], "/")
	}
	return repo
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
		return fmt.Errorf("failed to tag image: %w", err)
	}

	// #nosec G204 -- target is image reference from internal push logic.
	pushCmd, err := m.exec.Command("docker", []string{"push", target})
	if err != nil {
		return err
	}
	pushCmd.SetStdout(os.Stdout)
	pushCmd.SetStderr(os.Stderr)
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	Success(fmt.Sprintf("Pushed %s", target))
	return nil
}

// PushInCluster pushes an image using an in-cluster helper pod.
func (m *RegistryManager) PushInCluster(source, target, helperNS string) error {
	helperName := fmt.Sprintf("registry-pusher-%d", time.Now().UnixNano())

	// #nosec G204 -- helperNS from CLI flag, kubectl validates namespace names.
	if err := m.kubectl.Run([]string{"get", "namespace", helperNS}); err != nil {
		return fmt.Errorf("helper namespace %q not found (create it or pass --namespace): %w", helperNS, err)
	}

	// Ensure source is saved to tar
	tmpFile, err := os.CreateTemp("", "mcp-img-*.tar")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
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
		return fmt.Errorf("failed to save image: %w", err)
	}

	// Start helper pod with skopeo
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"run", helperName, "-n", helperNS, "--image=" + GetSkopeoImage(), "--restart=Never", "--command", "--", "sh", "-c", "while true; do sleep 3600; done"}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to start helper pod: %w", err)
	}
	defer func() {
		// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
		_ = m.kubectl.Run([]string{"delete", "pod", helperName, "-n", helperNS, "--ignore-not-found"})
	}()

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"wait", "--for=condition=Ready", "pod/" + helperName, "-n", helperNS, "--timeout=60s"}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("helper pod not ready: %w", err)
	}

	// Copy tar into pod
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"cp", tmpPath, fmt.Sprintf("%s/%s:%s", helperNS, helperName, "/tmp/image.tar")}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to copy image tar to helper pod: %w", err)
	}

	// Push using skopeo from inside cluster (registry is http, so disable tls verify)
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := m.kubectl.RunWithOutput([]string{"exec", "-n", helperNS, helperName, "--",
		"skopeo", "copy", "--dest-tls-verify=false", "docker-archive:/tmp/image.tar", "docker://" + target}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to push image from helper pod: %w", err)
	}

	Success(fmt.Sprintf("Pushed %s via in-cluster helper", target))
	return nil
}
