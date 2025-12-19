package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const defaultRegistrySecretName = "mcp-runtime-registry-creds"

// NewSetupCmd constructs the top-level setup command for installing the platform.
func NewSetupCmd(logger *zap.Logger) *cobra.Command {
	var registryType string
	var registryStorageSize string
	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool
	var tlsEnabled bool
	var testMode bool

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
			manifestPath := ingressManifest
			if !cmd.Flags().Changed("ingress-manifest") {
				if tlsEnabled {
					manifestPath = "config/ingress/overlays/prod"
				} else {
					manifestPath = "config/ingress/overlays/http"
				}
			}
			registryManifest := "config/registry"
			if tlsEnabled {
				registryManifest = "config/registry/overlays/tls"
			}

			return setupPlatform(logger, registryType, registryStorageSize, ingressMode, manifestPath, registryManifest, forceIngressInstall, tlsEnabled, testMode)
		},
	}

	cmd.Flags().StringVar(&registryType, "registry-type", "docker", "Registry type (docker; harbor coming soon)")
	cmd.Flags().StringVar(&registryStorageSize, "registry-storage", "20Gi", "Registry storage size (default: 20Gi)")
	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install automatically during setup (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/http", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force ingress install even if an ingress class already exists")
	cmd.Flags().BoolVar(&tlsEnabled, "with-tls", false, "Enable TLS overlays (ingress/registry); default is HTTP for dev")
	cmd.Flags().BoolVar(&testMode, "test-mode", false, "Test mode: skip operator build and use kind-loaded image")

	return cmd
}

func setupPlatform(logger *zap.Logger, registryType, registryStorageSize, ingressMode, ingressManifest, registryManifest string, forceIngressInstall, tlsEnabled, testMode bool) error {
	Section("MCP Runtime Setup")

	extRegistry, err := resolveExternalRegistryConfig(nil)
	if err != nil {
		Warn(fmt.Sprintf("Could not load external registry config: %v", err))
	}
	usingExternalRegistry := extRegistry != nil
	registrySecretName := defaultRegistrySecretName

	// Step 1: Initialize cluster
	Step("Step 1: Initialize cluster")
	Info("Installing CRD")
	if err := initCluster(logger, "", ""); err != nil {
		Error(fmt.Sprintf("Cluster initialization failed: %v", err))
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}
	Info("Cluster initialized")

	// Step 2: Configure cluster
	Step("Step 2: Configure cluster")
	Info("Checking ingress controller")
	ingressOpts := ingressOptions{
		mode:     ingressMode,
		manifest: ingressManifest,
		force:    forceIngressInstall,
	}
	if err := configureCluster(logger, ingressOpts); err != nil {
		Error(fmt.Sprintf("Cluster configuration failed: %v", err))
		return fmt.Errorf("cluster configuration failed: %w", err)
	}
	Info("Cluster configuration complete")

	// Step 3: Configure TLS (if enabled)
	Step("Step 3: Configure TLS")
	if tlsEnabled {
		if err := setupTLS(logger); err != nil {
			Error(fmt.Sprintf("TLS setup failed: %v", err))
			return fmt.Errorf("TLS setup failed: %w", err)
		}
		Success("TLS configured successfully")
	} else {
		Info("Skipped (TLS disabled, use --with-tls to enable)")
	}

	// Step 4: Deploy internal container registry
	Step("Step 4: Configure registry")
	if usingExternalRegistry {
		Info(fmt.Sprintf("Using external registry: %s", extRegistry.URL))
		if extRegistry.Username != "" || extRegistry.Password != "" {
			Info("Logging into external registry")
			if err := loginRegistry(logger, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
				Error(fmt.Sprintf("Registry login failed: %v", err))
				return err
			}
		}
	} else {
		Info(fmt.Sprintf("Type: %s", registryType))
		if tlsEnabled {
			Info("TLS: enabled (registry overlay)")
		} else {
			Info("TLS: disabled (dev HTTP mode)")
		}
		if err := deployRegistry(logger, "registry", GetRegistryPort(), registryType, registryStorageSize, registryManifest); err != nil {
			Error(fmt.Sprintf("Registry deployment failed: %v", err))
			return fmt.Errorf("failed to deploy registry: %w", err)
		}

		Info("Waiting for registry to be ready...")
		if err := waitForDeploymentAvailable(logger, "registry", "registry", "app=registry", GetDeploymentTimeout()); err != nil {
			printDeploymentDiagnostics("registry", "registry", "app=registry")
			Error(fmt.Sprintf("Registry failed to become ready: %v", err))
			return err
		}

		showRegistryInfo(logger)
	}

	// Step 5: Deploy operator
	Step("Step 5: Deploy operator")

	operatorImage := getOperatorImage(extRegistry, testMode)
	Info(fmt.Sprintf("Image: %s", operatorImage))

	if testMode {
		Info("Test mode: skipping operator build, using kind-loaded image")
	} else {
		Info("Building operator image")
		if err := buildOperatorImage(operatorImage); err != nil {
			Error(fmt.Sprintf("Operator image build failed: %v", err))
			return fmt.Errorf("operator image build failed: %w", err)
		}

		if usingExternalRegistry {
			Info("Pushing operator image to external registry")
			if err := pushOperatorImage(operatorImage); err != nil {
				Warn(fmt.Sprintf("Could not push image to external registry: %v", err))
			}
		} else {
			Info("Pushing operator image to internal registry")
			internalRegistryURL := getPlatformRegistryURL(logger)
			internalOperatorImage := internalRegistryURL + "/mcp-runtime-operator:latest"

			if err := ensureNamespace("registry"); err != nil {
				return fmt.Errorf("failed to ensure registry namespace: %w", err)
			}

			pushErr := pushOperatorImageToInternalRegistry(logger, operatorImage, internalOperatorImage, "registry")
			operatorImage = internalOperatorImage
			if pushErr != nil {
				Error(fmt.Sprintf("Could not push image to internal registry via in-cluster helper: %v", pushErr))
				return fmt.Errorf("failed to push operator image to internal registry: %w", pushErr)
			}
			Info(fmt.Sprintf("Using internal registry image: %s", operatorImage))
		}
	}

	Info("Deploying operator manifests")
	if err := deployOperatorManifests(logger, operatorImage); err != nil {
		Error(fmt.Sprintf("Operator deployment failed: %v", err))
		return fmt.Errorf("operator deployment failed: %w", err)
	}

	if usingExternalRegistry {
		if err := configureProvisionedRegistryEnv(extRegistry, registrySecretName); err != nil {
			Error(fmt.Sprintf("Failed to set PROVISIONED_REGISTRY_* env on operator: %v", err))
			return fmt.Errorf("failed to configure external registry env on operator: %w", err)
		}
	}
	if err := restartDeployment("mcp-runtime-operator-controller-manager", "mcp-runtime"); err != nil {
		if usingExternalRegistry {
			Error(fmt.Sprintf("Failed to restart operator deployment to pick up registry env: %v", err))
			return fmt.Errorf("failed to restart operator deployment after registry env update: %w", err)
		}
		Warn(fmt.Sprintf("Could not restart operator deployment: %v", err))
	}

	// Step 6: Verify components
	if err := verifySetup(usingExternalRegistry); err != nil {
		Error(fmt.Sprintf("Post-setup verification failed: %v", err))
		return err
	}

	Success("Platform setup complete")
	fmt.Println(Green("\nPlatform is ready. Use 'mcp-runtime status' to check everything."))
	return nil
}

func verifySetup(usingExternalRegistry bool) error {
	Step("Step 6: Verify platform components")

	if usingExternalRegistry {
		Info("Skipping internal registry availability check (using external registry)")
	} else {
		Info("Waiting for registry deployment to be available")
		if err := waitForDeploymentAvailable(nil, "registry", "registry", "app=registry", GetDeploymentTimeout()); err != nil {
			printDeploymentDiagnostics("registry", "registry", "app=registry")
			return fmt.Errorf("registry not ready: %w", err)
		}
	}

	Info("Waiting for operator deployment to be available")
	if err := waitForDeploymentAvailable(nil, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager", GetDeploymentTimeout()); err != nil {
		printDeploymentDiagnostics("mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		return fmt.Errorf("operator not ready: %w", err)
	}

	Info("Checking MCPServer CRD presence")
	if err := checkCRDInstalled("mcpservers.mcp.agent-hellboy.io"); err != nil {
		return fmt.Errorf("CRD check failed: %w", err)
	}

	Success("Verification complete")
	return nil
}

func getOperatorImage(ext *ExternalRegistryConfig, testMode bool) string {
	// Check for explicit override first
	if override := GetOperatorImageOverride(); override != "" {
		return override
	}

	// In test mode, use the standard kind-loaded image
	if testMode {
		return "docker.io/library/mcp-runtime-operator:latest"
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
		if err := ensureProvisionedRegistrySecret(secretName, ext.Username, ext.Password); err != nil {
			return err
		}
		args = append(args, "PROVISIONED_REGISTRY_SECRET_NAME="+secretName)
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
			Info(fmt.Sprintf("Still waiting for deployment/%s in %s (selector %s, timeout %s)", name, namespace, selector, timeout.Round(time.Second)))
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
	Warn(fmt.Sprintf("Deployment %s in %s is not ready. Showing pod statuses:", deploy, namespace))
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "wide")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// deployOperatorManifests deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifests(logger *zap.Logger, operatorImage string) error {
	// Step 1: Apply CRD
	Info("Applying CRD manifests")
	cmd := exec.Command("kubectl", "apply", "--validate=false", "-f", "config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply CRD: %w", err)
	}

	// Step 2: Apply RBAC (ServiceAccount, Role, RoleBinding)
	Info("Applying RBAC manifests")
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
	Info("Applying operator deployment")
	// Read manager.yaml, replace image, and apply
	managerYAML, err := os.ReadFile("config/manager/manager.yaml")
	if err != nil {
		return fmt.Errorf("failed to read manager.yaml: %w", err)
	}

	// Replace image name using a broad regex with captured indentation to handle registry-customized image values.
	// This targets the first image field in the file (the manager container).
	re := regexp.MustCompile(`(?m)^(\s*)image:\s*\S+`)
	managerYAMLStr := re.ReplaceAllString(string(managerYAML), fmt.Sprintf("${1}image: %s", operatorImage))

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

	Success("Operator manifests deployed successfully")
	return nil
}

// setupTLS configures TLS by applying cert-manager resources.
// Prerequisites: cert-manager must be installed and CA secret must exist.
func setupTLS(logger *zap.Logger) error {
	// Check if cert-manager CRDs are installed
	Info("Checking cert-manager installation")
	cmd := exec.Command("kubectl", "get", "crd", "certificates.cert-manager.io")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
	}
	Info("cert-manager CRDs found")

	// Check if CA secret exists
	Info("Checking CA secret")
	cmd = exec.Command("kubectl", "get", "secret", "mcp-runtime-ca", "-n", "cert-manager")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("CA secret 'mcp-runtime-ca' not found in cert-manager namespace. Create it first:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
	}
	Info("CA secret found")

	// Apply ClusterIssuer
	Info("Applying ClusterIssuer")
	cmd = exec.Command("kubectl", "apply", "-f", "config/cert-manager/cluster-issuer.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply ClusterIssuer: %w", err)
	}

	// Ensure registry namespace exists before applying Certificate
	if err := ensureNamespace("registry"); err != nil {
		return fmt.Errorf("failed to create registry namespace: %w", err)
	}

	// Apply Certificate
	Info("Applying Certificate for registry")
	cmd = exec.Command("kubectl", "apply", "-f", "config/cert-manager/example-registry-certificate.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply Certificate: %w", err)
	}

	// Wait for certificate to be ready using kubectl wait
	certTimeout := GetCertTimeout()
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	cmd = exec.Command("kubectl", "wait", "--for=condition=Ready",
		"certificate/registry-cert", "-n", "registry",
		fmt.Sprintf("--timeout=%s", certTimeout))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout)
	}
	Info("Certificate issued successfully")
	return nil
}
