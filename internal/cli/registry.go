package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// NewRegistryCmd builds the registry subcommand for managing registry lifecycle.
func NewRegistryCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage container registry",
		Long:  "Commands for managing the container registry",
	}

	cmd.AddCommand(newRegistryStatusCmd(logger))
	cmd.AddCommand(newRegistryInfoCmd(logger))
	cmd.AddCommand(newRegistryProvisionCmd(logger))
	cmd.AddCommand(newRegistryPushCmd(logger))

	return cmd
}

func newRegistryStatusCmd(logger *zap.Logger) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check registry status",
		Long:  "Check the status of the container registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkRegistryStatus(logger, namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "registry", "Registry namespace")

	return cmd
}

func newRegistryInfoCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show registry information",
		Long:  "Show registry URL and connection information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showRegistryInfo(logger)
		},
	}

	return cmd
}

func newRegistryProvisionCmd(logger *zap.Logger) *cobra.Command {
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
				logger.Info("Performing docker login to external registry", zap.String("url", cfg.URL))
				if err := loginRegistry(logger, cfg.URL, cfg.Username, cfg.Password); err != nil {
					return err
				}
			}
			if operatorImage != "" {
				logger.Info("Building and pushing operator image to external registry", zap.String("image", operatorImage))
				if err := buildOperatorImage(operatorImage); err != nil {
					return fmt.Errorf("failed to build operator image: %w", err)
				}
				if err := pushOperatorImage(operatorImage); err != nil {
					return fmt.Errorf("failed to push operator image: %w", err)
				}
			}
			logger.Info("External registry configured", zap.String("url", cfg.URL))
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

func newRegistryPushCmd(logger *zap.Logger) *cobra.Command {
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
				targetRegistry = getPlatformRegistryURL(logger)
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

			logger.Info("Pushing image", zap.String("source", image), zap.String("target", target))

			switch mode {
			case "direct":
				return pushDirect(image, target)
			case "in-cluster":
				return pushInCluster(logger, image, target, helperNamespace)
			default:
				return fmt.Errorf("unknown mode %q (use direct|in-cluster)", mode)
			}
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Local image to push (required)")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Target registry (defaults to provisioned or internal)")
	cmd.Flags().StringVar(&name, "name", "", "Override target repo/name (default: source name without registry)")
	cmd.Flags().StringVar(&mode, "mode", "in-cluster", "Push mode: in-cluster (default, uses skopeo helper) or direct (docker push)")
	cmd.Flags().StringVar(&helperNamespace, "namespace", "registry", "Namespace to run the in-cluster helper pod")

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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

	if envURL := strings.TrimSpace(os.Getenv("PROVISIONED_REGISTRY_URL")); envURL != "" {
		cfg.URL = envURL
		sourceFound = true
	}
	if envUser := os.Getenv("PROVISIONED_REGISTRY_USERNAME"); envUser != "" {
		cfg.Username = envUser
		sourceFound = true
	}
	if envPass := os.Getenv("PROVISIONED_REGISTRY_PASSWORD"); envPass != "" {
		cfg.Password = envPass
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
	cmd := exec.Command("kubectl", "apply", "-k", manifestPath, "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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

	getCmd := exec.Command("kubectl", "get", "pvc", "registry-storage", "-n", namespace, "-o", "jsonpath={.spec.resources.requests.storage}")
	var stdout, stderr bytes.Buffer
	getCmd.Stdout = &stdout
	getCmd.Stderr = &stderr
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
	patchCmd := exec.Command("kubectl", "patch", "pvc", "registry-storage", "-n", namespace, "-p", patchPayload)
	patchCmd.Stdout = os.Stdout
	patchCmd.Stderr = os.Stderr
	if err := patchCmd.Run(); err != nil {
		return fmt.Errorf("failed to update registry storage size to %s: %w", storageSize, err)
	}

	return nil
}

func checkRegistryStatus(logger *zap.Logger, namespace string) error {
	logger.Info("Checking registry status")

	// Check deployment
	cmd := exec.Command("kubectl", "get", "deployment", "registry", "-n", namespace, "-o", "wide")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// Check service
	fmt.Println("\nService:")
	cmd = exec.Command("kubectl", "get", "service", "registry", "-n", namespace, "-o", "wide")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// Check pods
	fmt.Println("\nPods:")
	cmd = exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", "app=registry")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func loginRegistry(logger *zap.Logger, registryURL, username, password string) error {
	logger.Info("Logging into registry", zap.String("url", registryURL))

	cmd := exec.Command("docker", "login", "-u", username, "--password-stdin", registryURL)
	cmd.Stdin = strings.NewReader(password)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to login to registry: %w", err)
	}

	logger.Info("Successfully logged into registry")
	return nil
}

func showRegistryInfo(logger *zap.Logger) error {
	ns := "registry"
	// Get registry service
	cmd := exec.Command("kubectl", "get", "service", "registry", "-n", ns, "-o", "jsonpath={.spec.clusterIP}")
	clusterIP, err := cmd.Output()
	if err != nil {
		logger.Debug("Failed to get registry cluster IP", zap.Error(err))
	}

	cmd = exec.Command("kubectl", "get", "service", "registry", "-n", ns, "-o", "jsonpath={.spec.ports[0].port}")
	port, portErr := cmd.Output()
	if portErr != nil {
		logger.Debug("Failed to get registry port", zap.Error(portErr))
	}

	if len(clusterIP) > 0 && len(port) > 0 {
		fmt.Println("\n=== Registry Information ===")
		fmt.Printf("Internal URL: %s:%s\n", string(clusterIP), string(port))
		fmt.Printf("Service URL: registry.registry.svc.cluster.local:%s\n", string(port))
		fmt.Printf("\nTo use from local machine, configure Docker:\n")
		fmt.Printf("  Add to /etc/docker/daemon.json:\n")
		fmt.Printf("    \"insecure-registries\": [\"%s:%s\"]\n", string(clusterIP), string(port))
		fmt.Printf("\nOr use port-forward:\n")
		fmt.Printf("  kubectl port-forward -n registry svc/registry 5000:5000\n")
		fmt.Printf("  Then use: localhost:5000\n")
	} else {
		fmt.Println("Registry not found. Deploy it with: mcp-runtime registry deploy")
	}

	return nil
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

func pushDirect(source, target string) error {
	tagCmd := exec.Command("docker", "tag", source, target)
	tagCmd.Stdout = os.Stdout
	tagCmd.Stderr = os.Stderr
	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	pushCmd := exec.Command("docker", "push", target)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	fmt.Printf("Pushed %s\n", target)
	return nil
}

func pushInCluster(logger *zap.Logger, source, target, helperNS string) error {
	helperName := fmt.Sprintf("registry-pusher-%d", time.Now().UnixNano())

	nsCheck := exec.Command("kubectl", "get", "namespace", helperNS)
	if err := nsCheck.Run(); err != nil {
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

	saveCmd := exec.Command("docker", "save", "-o", tmpPath, source)
	saveCmd.Stdout = os.Stdout
	saveCmd.Stderr = os.Stderr
	if err := saveCmd.Run(); err != nil {
		return fmt.Errorf("failed to save image: %w", err)
	}

	// Start helper pod with skopeo
	runCmd := exec.Command("kubectl", "run", helperName, "-n", helperNS, "--image="+getSkopeoImage(), "--restart=Never", "--command", "--", "sh", "-c", "while true; do sleep 3600; done")
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("failed to start helper pod: %w", err)
	}
	defer func() {
		_ = exec.Command("kubectl", "delete", "pod", helperName, "-n", helperNS, "--ignore-not-found").Run()
	}()

	waitCmd := exec.Command("kubectl", "wait", "--for=condition=Ready", "pod/"+helperName, "-n", helperNS, "--timeout=60s")
	waitCmd.Stdout = os.Stdout
	waitCmd.Stderr = os.Stderr
	if err := waitCmd.Run(); err != nil {
		return fmt.Errorf("helper pod not ready: %w", err)
	}

	// Copy tar into pod
	cpCmd := exec.Command("kubectl", "cp", tmpPath, fmt.Sprintf("%s/%s:%s", helperNS, helperName, "/tmp/image.tar"))
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy image tar to helper pod: %w", err)
	}

	// Push using skopeo from inside cluster (registry is http, so disable tls verify)
	pushCmd := exec.Command("kubectl", "exec", "-n", helperNS, helperName, "--",
		"skopeo", "copy", "--dest-tls-verify=false", "docker-archive:/tmp/image.tar", "docker://"+target)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("failed to push image from helper pod: %w", err)
	}

	fmt.Printf("Pushed %s via in-cluster helper\n", target)
	return nil
}
