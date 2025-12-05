package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func NewSetupCmd(logger *zap.Logger) *cobra.Command {
	var registryType string
	var registryStorageSize string
	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool

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
			return setupPlatform(logger, registryType, registryStorageSize, ingressMode, ingressManifest, forceIngressInstall)
		},
	}

	cmd.Flags().StringVar(&registryType, "registry-type", "docker", "Registry type (docker; harbor coming soon)")
	cmd.Flags().StringVar(&registryStorageSize, "registry-storage", "20Gi", "Registry storage size (default: 20Gi)")
	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install automatically during setup (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/prod", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force ingress install even if an ingress class already exists")

	return cmd
}

func setupPlatform(logger *zap.Logger, registryType, registryStorageSize, ingressMode, ingressManifest string, forceIngressInstall bool) error {
	printSection("MCP Runtime Setup")

	extRegistry, err := resolveExternalRegistryConfig(nil)
	if err != nil {
		printWarn(fmt.Sprintf("Could not load external registry config: %v", err))
	}
	usingExternalRegistry := extRegistry != nil
	registrySecretName := "mcp-runtime-registry-creds"

	// Step 1: Initialize cluster
	printStep("Step 1: Initialize cluster")
	printInfo("Installing CRD")
	if err := initCluster(logger, "", ""); err != nil {
		printError(fmt.Sprintf("Cluster initialization failed: %v", err))
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}
	printInfo("Cluster initialized")

	// Step 2: Configure cluster
	printStep("Step 2: Configure cluster")
	printInfo("Checking ingress controller")
	ingressOpts := ingressOptions{
		mode:     ingressMode,
		manifest: ingressManifest,
		force:    forceIngressInstall,
	}
	if err := configureCluster(logger, ingressOpts); err != nil {
		printError(fmt.Sprintf("Cluster configuration failed: %v", err))
		return fmt.Errorf("cluster configuration failed: %w", err)
	}
	printInfo("Cluster configuration complete")

	// Step 3: Deploy internal container registry
	printStep("Step 3: Configure registry")
	if usingExternalRegistry {
		printInfo(fmt.Sprintf("Using external registry: %s", extRegistry.URL))
		if extRegistry.Username != "" || extRegistry.Password != "" {
			printInfo("Logging into external registry")
			if err := loginRegistry(logger, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
				printError(fmt.Sprintf("Registry login failed: %v", err))
				return err
			}
		}
	} else {
		printInfo(fmt.Sprintf("Type: %s", registryType))
		if err := deployRegistry(logger, "registry", 5000, registryType, registryStorageSize); err != nil {
			printError(fmt.Sprintf("Registry deployment failed: %v", err))
			return fmt.Errorf("failed to deploy registry: %w", err)
		}

		printInfo("Waiting for registry to be ready...")
		if err := waitForDeploymentAvailable(logger, "registry", "registry", "app=registry", 5*time.Minute); err != nil {
			printError(fmt.Sprintf("Registry failed to become ready: %v", err))
			return err
		}

		showRegistryInfo(logger)
	}

	// Step 4: Deploy operator
	printStep("Step 4: Deploy operator")
	operatorImage := getOperatorImage(extRegistry)
	printInfo(fmt.Sprintf("Image: %s", operatorImage))

	// Check if operator build/push should be skipped (for Kind/local testing)
	skipOperatorBuild := os.Getenv("SKIP_OPERATOR_BUILD") == "1"

	if skipOperatorBuild {
		printInfo("Skipping operator build (SKIP_OPERATOR_BUILD=1, using pre-loaded image)")
	} else {
		printInfo("Building operator image")
		if err := buildOperatorImage(operatorImage); err != nil {
			printError(fmt.Sprintf("Operator image build failed: %v", err))
			return fmt.Errorf("operator image build failed: %w", err)
		}

		if usingExternalRegistry {
			printInfo("Pushing operator image to external registry")
			if err := pushOperatorImage(operatorImage); err != nil {
				printWarn(fmt.Sprintf("Could not push image to external registry: %v", err))
			}
		} else {
			printInfo("Pushing operator image to internal registry")
			internalRegistryURL := getPlatformRegistryURL(logger)
			internalOperatorImage := internalRegistryURL + "/mcp-runtime-operator:latest"

			if err := ensureNamespace("registry"); err != nil {
				return fmt.Errorf("failed to ensure registry namespace: %w", err)
			}

			pushErr := pushOperatorImageToInternalRegistry(logger, operatorImage, internalOperatorImage, "registry")
			operatorImage = internalOperatorImage
			if pushErr != nil {
				printError(fmt.Sprintf("Could not push image to internal registry via in-cluster helper: %v", pushErr))
				return fmt.Errorf("failed to push operator image to internal registry: %w", pushErr)
			}
			printInfo(fmt.Sprintf("Using internal registry image: %s", operatorImage))
		}
	}

	printInfo("Deploying operator manifests")
	if err := deployOperatorManifests(logger, operatorImage); err != nil {
		printError(fmt.Sprintf("Operator deployment failed: %v", err))
		return fmt.Errorf("operator deployment failed: %w", err)
	}

	if usingExternalRegistry {
		if err := configureProvisionedRegistryEnv(extRegistry, registrySecretName); err != nil {
			printWarn(fmt.Sprintf("Could not set PROVISIONED_REGISTRY_* env on operator: %v", err))
		}
	}
	if err := restartDeployment("mcp-runtime-operator-controller-manager", "mcp-runtime"); err != nil {
		printWarn(fmt.Sprintf("Could not restart operator deployment: %v", err))
	}

	// Step 5: Verify components
	if err := verifySetup(usingExternalRegistry); err != nil {
		printError(fmt.Sprintf("Post-setup verification failed: %v", err))
		return err
	}

	printSuccess("Platform setup complete")
	fmt.Println(colorGreen("\nPlatform is ready. Use 'mcp-runtime status' to check everything."))
	return nil
}

func verifySetup(usingExternalRegistry bool) error {
	printStep("Step 5: Verify platform components")

	if usingExternalRegistry {
		printInfo("Skipping internal registry availability check (using external registry)")
	} else {
		printInfo("Waiting for registry deployment to be available")
		if err := waitForDeploymentAvailable(nil, "registry", "registry", "app=registry", 5*time.Minute); err != nil {
			printDeploymentDiagnostics("registry", "registry", "app=registry")
			return fmt.Errorf("registry not ready: %w", err)
		}
	}

	printInfo("Waiting for operator deployment to be available")
	if err := waitForDeploymentAvailable(nil, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager", 5*time.Minute); err != nil {
		printDeploymentDiagnostics("mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		return fmt.Errorf("operator not ready: %w", err)
	}

	printInfo("Checking MCPServer CRD presence")
	if err := checkCRDInstalled("mcpservers.mcp.agent-hellboy.io"); err != nil {
		return fmt.Errorf("CRD check failed: %w", err)
	}

	printSuccess("Verification complete")
	return nil
}

func getOperatorImage(ext *ExternalRegistryConfig) string {
	if val := os.Getenv("OPERATOR_IMG"); val != "" {
		return val
	}
	if ext != nil && ext.URL != "" {
		return strings.TrimSuffix(ext.URL, "/") + "/mcp-runtime-operator:latest"
	}
	// Fallback to an internal-cluster reachable URL (resolved via ClusterIP).
	return fmt.Sprintf("%s/mcp-runtime-operator:latest", getPlatformRegistryURL(nil))
}

func configureProvisionedRegistryEnv(ext *ExternalRegistryConfig, secretName string) error {
	if ext == nil || ext.URL == "" {
		return nil
	}
	if secretName == "" {
		secretName = "mcp-runtime-registry-creds"
	}
	args := []string{
		"set", "env", "deployment/mcp-runtime-operator-controller-manager",
		"-n", "mcp-runtime",
		"PROVISIONED_REGISTRY_URL=" + ext.URL,
	}
	if secretName != "" {
		args = append(args, "PROVISIONED_REGISTRY_SECRET_NAME="+secretName)
	}
	if ext.Username != "" || ext.Password != "" {
		if err := ensureProvisionedRegistrySecret(secretName, ext.Username, ext.Password); err != nil {
			return err
		}
		// Populate env vars from the secret instead of literals to avoid leaking creds in args/history.
		args = append(args, "--from=secret/"+secretName)
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureProvisionedRegistrySecret(name, username, password string) error {
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

	createCmd := exec.Command(
		"kubectl", "create", "secret", "generic", name,
		"--from-env-file=-",
		"-n", "mcp-runtime",
		"--dry-run=client",
		"-o", "yaml",
	)
	createCmd.Stdin = strings.NewReader(envData.String())
	var rendered bytes.Buffer
	createCmd.Stdout = &rendered
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("render secret manifest: %w", err)
	}

	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = &rendered
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("apply secret manifest: %w", err)
	}

	return nil
}

func buildOperatorImage(image string) error {
	cmd := exec.Command("make", "-f", "Makefile.operator", "docker-build-operator", "IMG="+image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func restartDeployment(name, namespace string) error {
	cmd := exec.Command("kubectl", "rollout", "restart", "deployment/"+name, "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pushOperatorImage(image string) error {
	cmd := exec.Command("docker", "push", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pushOperatorImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	if err := pushInCluster(logger, sourceImage, targetImage, helperNamespace); err != nil {
		return fmt.Errorf("failed to push image in-cluster: %w", err)
	}
	return nil
}

func checkCRDInstalled(name string) error {
	cmd := exec.Command("kubectl", "get", "crd", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForDeploymentAvailable polls a deployment until it has at least one available replica or times out.
func waitForDeploymentAvailable(logger *zap.Logger, name, namespace, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastLog := time.Time{}
	for {
		cmd := exec.Command("kubectl", "get", "deployment", name, "-n", namespace, "-o", "jsonpath={.status.availableReplicas}")
		out, err := cmd.Output()
		if err == nil {
			val := strings.TrimSpace(string(out))
			if val == "" {
				val = "0"
			}
			if n, convErr := strconv.Atoi(val); convErr == nil && n > 0 {
				return nil
			}
		}
		if time.Since(lastLog) > 10*time.Second {
			printInfo(fmt.Sprintf("Still waiting for deployment/%s in %s (selector %s, timeout %s)", name, namespace, selector, timeout.Round(time.Second)))
			if logger != nil {
				logger.Info("Waiting for deployment", zap.String("deployment", name), zap.String("namespace", namespace), zap.String("selector", selector))
			}
			lastLog = time.Now()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for deployment %s in namespace %s", name, namespace)
		}
		time.Sleep(5 * time.Second)
	}
}

// printDeploymentDiagnostics prints a quick status of pods for a deployment selector to help users triage readiness issues.
func printDeploymentDiagnostics(deploy, namespace, selector string) {
	printWarn(fmt.Sprintf("Deployment %s in %s is not ready. Showing pod statuses:", deploy, namespace))
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "wide")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// deployOperatorManifests deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifests(logger *zap.Logger, operatorImage string) error {
	// Step 1: Apply CRD
	printInfo("Applying CRD manifests")
	cmd := exec.Command("kubectl", "apply", "-f", "config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply CRD: %w", err)
	}

	// Step 2: Apply RBAC (ServiceAccount, Role, RoleBinding)
	printInfo("Applying RBAC manifests")
	if err := ensureNamespace("mcp-runtime"); err != nil {
		return fmt.Errorf("failed to ensure operator namespace: %w", err)
	}

	cmd = exec.Command("kubectl", "apply", "-k", "config/rbac/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply RBAC: %w", err)
	}

	// Step 3: Apply manager deployment with image replacement
	printInfo("Applying operator deployment")
	// Read manager.yaml, replace image, and apply
	managerYAML, err := os.ReadFile("config/manager/manager.yaml")
	if err != nil {
		return fmt.Errorf("failed to read manager.yaml: %w", err)
	}

	// Replace image name (simple string replacement)
	// Original: image: mcp-runtime-operator:latest
	// Replace with: image: <operatorImage>
	managerYAMLStr := strings.ReplaceAll(string(managerYAML), "image: mcp-runtime-operator:latest", fmt.Sprintf("image: %s", operatorImage))

	// Write to temp file and apply
	tmpFile, err := os.CreateTemp("", "manager-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(managerYAMLStr); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Delete existing deployment to avoid immutable selector conflicts on reapply.
	_ = exec.Command("kubectl", "delete", "deployment/mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "--ignore-not-found").Run()

	cmd = exec.Command("kubectl", "apply", "-f", tmpFile.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply manager deployment: %w", err)
	}

	printSuccess("Operator manifests deployed successfully")
	return nil
}
