package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const defaultRegistrySecretName = "mcp-runtime-registry-creds" // #nosec G101 -- default secret name, not a credential.

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
	SetupTLS                        func(logger *zap.Logger) error
	BuildOperatorImage              func(image string) error
	PushOperatorImage               func(image string) error
	EnsureNamespace                 func(namespace string) error
	GetPlatformRegistryURL          func(logger *zap.Logger) string
	PushOperatorImageToInternal     func(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error
	DeployOperatorManifests         func(logger *zap.Logger, operatorImage string) error
	ConfigureProvisionedRegistryEnv func(ext *ExternalRegistryConfig, secretName string) error
	RestartDeployment               func(name, namespace string) error
	CheckCRDInstalled               func(name string) error
	GetDeploymentTimeout            func() time.Duration
	GetRegistryPort                 func() int
	OperatorImageFor                func(ext *ExternalRegistryConfig, testMode bool) string
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
		d.SetupTLS = setupTLS
	}
	if d.BuildOperatorImage == nil {
		d.BuildOperatorImage = buildOperatorImage
	}
	if d.PushOperatorImage == nil {
		d.PushOperatorImage = pushOperatorImage
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
	if d.DeployOperatorManifests == nil {
		d.DeployOperatorManifests = deployOperatorManifests
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
	return d
}

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
			plan := BuildSetupPlan(SetupPlanInput{
				RegistryType:           registryType,
				RegistryStorageSize:    registryStorageSize,
				IngressMode:            ingressMode,
				IngressManifest:        ingressManifest,
				IngressManifestChanged: cmd.Flags().Changed("ingress-manifest"),
				ForceIngressInstall:    forceIngressInstall,
				TLSEnabled:             tlsEnabled,
				TestMode:               testMode,
			})

			return setupPlatform(logger, plan)
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

func setupPlatform(logger *zap.Logger, plan SetupPlan) error {
	return setupPlatformWithDeps(logger, plan, SetupDeps{}.withDefaults(logger))
}

func setupPlatformWithDeps(logger *zap.Logger, plan SetupPlan, deps SetupDeps) error {
	deps = deps.withDefaults(logger)
	Section("MCP Runtime Setup")

	extRegistry, usingExternalRegistry, registrySecretName := resolveRegistrySetup(logger, deps)
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

func setupClusterSteps(logger *zap.Logger, ingressOpts ingressOptions, deps SetupDeps) error {
	// Step 1: Initialize cluster
	Step("Step 1: Initialize cluster")
	Info("Installing CRD")
	if err := deps.ClusterManager.InitCluster("", ""); err != nil {
		Error(fmt.Sprintf("Cluster initialization failed: %v", err))
		return fmt.Errorf("failed to initialize cluster: %w", err)
	}
	Info("Cluster initialized")

	// Step 2: Configure cluster
	Step("Step 2: Configure cluster")
	Info("Checking ingress controller")
	if err := deps.ClusterManager.ConfigureCluster(ingressOpts); err != nil {
		Error(fmt.Sprintf("Cluster configuration failed: %v", err))
		return fmt.Errorf("cluster configuration failed: %w", err)
	}
	Info("Cluster configuration complete")
	return nil
}

func setupTLSStep(logger *zap.Logger, tlsEnabled bool, deps SetupDeps) error {
	// Step 3: Configure TLS (if enabled)
	Step("Step 3: Configure TLS")
	if !tlsEnabled {
		Info("Skipped (TLS disabled, use --with-tls to enable)")
		return nil
	}
	if err := deps.SetupTLS(logger); err != nil {
		Error(fmt.Sprintf("TLS setup failed: %v", err))
		return fmt.Errorf("TLS setup failed: %w", err)
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
				Error(fmt.Sprintf("Registry login failed: %v", err))
				return err
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
		Error(fmt.Sprintf("Registry deployment failed: %v", err))
		return fmt.Errorf("failed to deploy registry: %w", err)
	}

	Info("Waiting for registry to be ready...")
	if err := deps.WaitForDeploymentAvailable(logger, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
		Error(fmt.Sprintf("Registry failed to become ready: %v", err))
		return err
	}

	if err := deps.RegistryManager.ShowRegistryInfo(); err != nil {
		Warn(fmt.Sprintf("Failed to show registry info: %v", err))
	}
	return nil
}

func prepareOperatorImage(logger *zap.Logger, extRegistry *ExternalRegistryConfig, usingExternalRegistry, testMode bool, deps SetupDeps) (string, error) {
	// Step 5: Deploy operator
	Step("Step 5: Deploy operator")

	operatorImage := deps.OperatorImageFor(extRegistry, testMode)
	Info(fmt.Sprintf("Image: %s", operatorImage))

	if testMode {
		Info("Test mode: skipping operator build, using kind-loaded image")
		return operatorImage, nil
	}

	Info("Building operator image")
	if err := deps.BuildOperatorImage(operatorImage); err != nil {
		Error(fmt.Sprintf("Operator image build failed: %v", err))
		return "", fmt.Errorf("operator image build failed: %w", err)
	}

	if usingExternalRegistry {
		Info("Pushing operator image to external registry")
		if err := deps.PushOperatorImage(operatorImage); err != nil {
			Warn(fmt.Sprintf("Could not push image to external registry: %v", err))
		}
		return operatorImage, nil
	}

	Info("Pushing operator image to internal registry")
	internalRegistryURL := deps.GetPlatformRegistryURL(logger)
	internalOperatorImage := internalRegistryURL + "/mcp-runtime-operator:latest"

	if err := deps.EnsureNamespace("registry"); err != nil {
		return "", fmt.Errorf("failed to ensure registry namespace: %w", err)
	}

	if err := deps.PushOperatorImageToInternal(logger, operatorImage, internalOperatorImage, "registry"); err != nil {
		Error(fmt.Sprintf("Could not push image to internal registry via in-cluster helper: %v", err))
		return "", fmt.Errorf("failed to push operator image to internal registry: %w", err)
	}
	Info(fmt.Sprintf("Using internal registry image: %s", internalOperatorImage))
	return internalOperatorImage, nil
}

func deployOperatorStep(logger *zap.Logger, operatorImage string, extRegistry *ExternalRegistryConfig, registrySecretName string, usingExternalRegistry bool, deps SetupDeps) error {
	Info("Deploying operator manifests")
	if err := deps.DeployOperatorManifests(logger, operatorImage); err != nil {
		Error(fmt.Sprintf("Operator deployment failed: %v", err))
		return fmt.Errorf("operator deployment failed: %w", err)
	}

	if usingExternalRegistry {
		if err := deps.ConfigureProvisionedRegistryEnv(extRegistry, registrySecretName); err != nil {
			Error(fmt.Sprintf("Failed to set PROVISIONED_REGISTRY_* env on operator: %v", err))
			return fmt.Errorf("failed to configure external registry env on operator: %w", err)
		}
	}

	if err := deps.RestartDeployment("mcp-runtime-operator-controller-manager", "mcp-runtime"); err != nil {
		if usingExternalRegistry {
			Error(fmt.Sprintf("Failed to restart operator deployment to pick up registry env: %v", err))
			return fmt.Errorf("failed to restart operator deployment after registry env update: %w", err)
		}
		Warn(fmt.Sprintf("Could not restart operator deployment: %v", err))
	}
	return nil
}

func verifySetup(usingExternalRegistry bool, deps SetupDeps) error {
	Step("Step 6: Verify platform components")

	if usingExternalRegistry {
		Info("Skipping internal registry availability check (using external registry)")
	} else {
		Info("Waiting for registry deployment to be available")
		if err := deps.WaitForDeploymentAvailable(nil, "registry", "registry", "app=registry", deps.GetDeploymentTimeout()); err != nil {
			deps.PrintDeploymentDiagnostics("registry", "registry", "app=registry")
			return fmt.Errorf("registry not ready: %w", err)
		}
	}

	Info("Waiting for operator deployment to be available")
	if err := deps.WaitForDeploymentAvailable(nil, "mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager", deps.GetDeploymentTimeout()); err != nil {
		deps.PrintDeploymentDiagnostics("mcp-runtime-operator-controller-manager", "mcp-runtime", "control-plane=controller-manager")
		return fmt.Errorf("operator not ready: %w", err)
	}

	Info("Checking MCPServer CRD presence")
	if err := deps.CheckCRDInstalled("mcpservers.mcp-runtime.org"); err != nil {
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
		return fmt.Errorf("render secret manifest: %w", err)
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
		return fmt.Errorf("apply secret manifest: %w", err)
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
		return fmt.Errorf("marshal docker config: %w", err)
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
		return fmt.Errorf("apply imagePullSecret: %w", err)
	}

	return nil
}

func buildOperatorImage(image string) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := execCommandWithValidators("make", []string{"-f", "Makefile.operator", "docker-build-operator", "IMG=" + image})
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

func pushOperatorImageToInternalRegistry(logger *zap.Logger, sourceImage, targetImage, helperNamespace string) error {
	mgr := DefaultRegistryManager(logger)
	if err := mgr.PushInCluster(sourceImage, targetImage, helperNamespace); err != nil {
		return fmt.Errorf("failed to push image in-cluster: %w", err)
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
	printDeploymentDiagnosticsWithKubectl(kubectlClient, deploy, namespace, selector)
}

// printDeploymentDiagnosticsWithKubectl prints a quick status of pods for a deployment selector.
func printDeploymentDiagnosticsWithKubectl(kubectl KubectlRunner, deploy, namespace, selector string) {
	Warn(fmt.Sprintf("Deployment %s in %s is not ready. Showing pod statuses:", deploy, namespace))
	// #nosec G204 -- namespace/selector from internal diagnostics, not user input.
	_ = kubectl.RunWithOutput([]string{"get", "pods", "-n", namespace, "-l", selector, "-o", "wide"}, os.Stdout, os.Stderr)
}

// deployOperatorManifests deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifests(logger *zap.Logger, operatorImage string) error {
	return deployOperatorManifestsWithKubectl(kubectlClient, logger, operatorImage)
}

// deployOperatorManifestsWithKubectl deploys operator manifests without requiring kustomize or controller-gen.
// It applies CRD, RBAC, and manager manifests directly, replacing the image name in the process.
func deployOperatorManifestsWithKubectl(kubectl KubectlRunner, logger *zap.Logger, operatorImage string) error {
	// Step 1: Apply CRD
	Info("Applying CRD manifests")
	// #nosec G204 -- fixed file path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "--validate=false", "-f", "config/crd/bases/mcp-runtime.org_mcpservers.yaml"}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to apply CRD: %w", err)
	}

	// Step 2: Apply RBAC (ServiceAccount, Role, RoleBinding)
	Info("Applying RBAC manifests")
	if err := ensureNamespace(NamespaceMCPRuntime); err != nil {
		return fmt.Errorf("failed to ensure operator namespace: %w", err)
	}

	// #nosec G204 -- fixed kustomize path from repository.
	if err := kubectl.RunWithOutput([]string{"apply", "-k", "config/rbac/"}, os.Stdout, os.Stderr); err != nil {
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

	// Write to temp file under the working directory so kubectl path validation passes.
	tmpFile, err := os.CreateTemp(".", "manager-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(managerYAMLStr); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return fmt.Errorf("failed to close temp file after write error: %w", closeErr)
		}
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Delete existing deployment to avoid immutable selector conflicts on reapply.
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	_ = kubectl.Run([]string{"delete", "deployment/" + OperatorDeploymentName, "-n", NamespaceMCPRuntime, "--ignore-not-found"})

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	if err := kubectl.RunWithOutput([]string{"apply", "-f", tmpFile.Name()}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to apply manager deployment: %w", err)
	}

	Success("Operator manifests deployed successfully")
	return nil
}

// setupTLS configures TLS by applying cert-manager resources.
// Prerequisites: cert-manager must be installed and CA secret must exist.
func setupTLS(logger *zap.Logger) error {
	return setupTLSWithKubectl(kubectlClient, logger)
}

// setupTLSWithKubectl configures TLS by applying cert-manager resources.
// Prerequisites: cert-manager must be installed and CA secret must exist.
func setupTLSWithKubectl(kubectl KubectlRunner, logger *zap.Logger) error {
	// Check if cert-manager CRDs are installed
	Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
		return fmt.Errorf("cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
	}
	Info("cert-manager CRDs found")

	// Check if CA secret exists
	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(kubectl); err != nil {
		return fmt.Errorf("CA secret 'mcp-runtime-ca' not found in cert-manager namespace. Create it first:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
	}
	Info("CA secret found")

	// Apply ClusterIssuer
	Info("Applying ClusterIssuer")
	if err := applyClusterIssuerWithKubectl(kubectl); err != nil {
		return fmt.Errorf("failed to apply ClusterIssuer: %w", err)
	}

	// Ensure registry namespace exists before applying Certificate
	if err := ensureNamespace(NamespaceRegistry); err != nil {
		return fmt.Errorf("failed to create registry namespace: %w", err)
	}

	// Apply Certificate
	Info("Applying Certificate for registry")
	if err := applyRegistryCertificateWithKubectl(kubectl); err != nil {
		return fmt.Errorf("failed to apply Certificate: %w", err)
	}

	// Wait for certificate to be ready using kubectl wait
	certTimeout := GetCertTimeout()
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, NamespaceRegistry, certTimeout); err != nil {
		return fmt.Errorf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout)
	}
	Info("Certificate issued successfully")
	return nil
}
