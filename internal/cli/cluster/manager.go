// Package cluster implements cluster operations for the cluster CLI command.
package cluster

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kubeerr"
)

const defaultClusterName = "mcp-runtime"

// ClusterManager handles cluster operations with injected dependencies.
type ClusterManager struct {
	kubectl *core.KubectlClient
	exec    core.Executor
	logger  *zap.Logger
}

// NewClusterManager creates a ClusterManager with the given dependencies.
func NewClusterManager(kubectl *core.KubectlClient, exec core.Executor, logger *zap.Logger) *ClusterManager {
	return &ClusterManager{
		kubectl: kubectl,
		exec:    exec,
		logger:  logger,
	}
}

// DefaultClusterManager returns a ClusterManager using default clients.
func DefaultClusterManager(logger *zap.Logger) *ClusterManager {
	return NewClusterManager(core.DefaultKubectlClient(), core.DefaultExecutor(), logger)
}

// KubectlRunner exposes the shared kubectl runner for foldered command routing.
func (m *ClusterManager) KubectlRunner() core.KubectlRunner {
	return m.kubectl
}

// Logger exposes the shared logger for foldered command routing.
func (m *ClusterManager) Logger() *zap.Logger {
	return m.logger
}

// InitCluster initializes cluster configuration.
func (m *ClusterManager) InitCluster(kubeconfig, context string) error {
	m.logger.Info("Initializing cluster configuration")

	if err := m.ConfigureKubeconfig(kubeconfig, context); err != nil {
		return err
	}

	// Install CRD
	m.logger.Info("Installing CRD")
	// #nosec G204 -- fixed file path from repository.
	if err := m.kubectl.Run([]string{"apply", "--validate=false", "-f", "config/crd/bases/mcpruntime.org_mcpservers.yaml"}); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrInstallCRDFailed, err, fmt.Sprintf("failed to install CRD: %v", err))
		core.Error("Failed to install CRD")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to install CRD")
		return wrappedErr
	}

	// Create namespace
	m.logger.Info("Creating mcp-runtime namespace")
	if err := m.EnsureNamespace(core.NamespaceMCPRuntime); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrEnsureRuntimeNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure mcp-runtime namespace: %v", err),
			map[string]any{"namespace": core.NamespaceMCPRuntime, "component": "cluster"},
		)
		core.Error("Failed to ensure mcp-runtime namespace")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to ensure mcp-runtime namespace")
		return wrappedErr
	}

	m.logger.Info("Creating mcp-servers namespace")
	if err := m.EnsureNamespace(core.NamespaceMCPServers); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrEnsureServersNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure mcp-servers namespace: %v", err),
			map[string]any{"namespace": core.NamespaceMCPServers, "component": "cluster"},
		)
		core.Error("Failed to ensure mcp-servers namespace")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to ensure mcp-servers namespace")
		return wrappedErr
	}

	m.logger.Info("Cluster initialized successfully")
	return nil
}

func resolveKubeconfigPath(kubeconfig string) (string, error) {
	if kubeconfig != "" {
		return kubeconfig, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrGetHomeDirectoryFailed, err, fmt.Sprintf("failed to get home directory: %v", err))
		core.Error("Failed to get home directory")
		// Note: No logger available in this helper function
		return "", wrappedErr
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// ConfigureKubeconfig sets KUBECONFIG and optionally switches context.
func (m *ClusterManager) ConfigureKubeconfig(kubeconfig, context string) error {
	path, err := resolveKubeconfigPath(kubeconfig)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		msg := fmt.Sprintf("kubeconfig %q not found or not readable: %v", path, err)
		if hint, handled := kubeerr.SetupHint(err.Error()); handled {
			msg = hint
		}
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrKubeconfigNotReadable,
			err,
			msg,
			map[string]any{"kubeconfig": path, "component": "cluster"},
		)
		core.Error("Kubeconfig not readable")
		core.LogStructuredError(m.logger, wrappedErr, "Kubeconfig not readable")
		return wrappedErr
	}

	if err := os.Setenv("KUBECONFIG", path); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrSetKubeconfigFailed,
			err,
			fmt.Sprintf("failed to set KUBECONFIG: %v", err),
			map[string]any{"kubeconfig": path, "component": "cluster"},
		)
		core.Error("Failed to set KUBECONFIG")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to set KUBECONFIG")
		return wrappedErr
	}

	if context != "" {
		// #nosec G204 -- context from CLI flag, kubectl validates context names.
		if err := m.kubectl.Run([]string{"config", "use-context", context}); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrSetContextFailed,
				err,
				fmt.Sprintf("failed to set context: %v", err),
				map[string]any{"context": context, "component": "cluster"},
			)
			core.Error("Failed to set context")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to set context")
			return wrappedErr
		}
	}
	return nil
}

// ConfigureKubeconfigFromProvider updates kubeconfig using a cloud provider CLI.
func (m *ClusterManager) ConfigureKubeconfigFromProvider(provider, region, clusterName, resourceGroup, project, zone, kubeconfig string) error {
	switch strings.ToLower(provider) {
	case "eks":
		return configureEKSKubeconfig(m.exec, region, clusterName, kubeconfig)
	case "aks":
		err := core.NewWithSentinel(core.ErrAKSKubeconfigNotImplemented, "AKS kubeconfig not yet implemented; planned support (use `az aks get-credentials --name <cluster> --resource-group <rg>`)")
		core.Error("AKS kubeconfig not implemented")
		core.LogStructuredError(m.logger, err, "AKS kubeconfig not implemented")
		return err
	case "gke":
		err := core.NewWithSentinel(core.ErrGKEKubeconfigNotImplemented, "GKE kubeconfig not yet implemented; planned support (use `gcloud container clusters get-credentials <cluster> --region <region> --project <project>`)")
		core.Error("GKE kubeconfig not implemented")
		core.LogStructuredError(m.logger, err, "GKE kubeconfig not implemented")
		return err
	default:
		err := core.NewWithSentinel(core.ErrUnsupportedProvider, fmt.Sprintf("unsupported provider: %s", provider))
		core.Error("Unsupported provider")
		core.LogStructuredError(m.logger, err, "Unsupported provider")
		return err
	}
}

func configureEKSKubeconfig(exec core.Executor, region, clusterName, kubeconfig string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	args := []string{
		"eks",
		"update-kubeconfig",
		"--name", clusterName,
		"--region", region,
	}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := exec.Command("aws", args, core.AllowlistBins("aws"), core.NoShellMeta(), core.NoControlChars())
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

// CheckClusterStatus checks and displays cluster status.
func (m *ClusterManager) CheckClusterStatus() error {
	m.logger.Info("Checking cluster status")

	// Check cluster connectivity
	// #nosec G204 -- fixed kubectl command.
	output, err := m.kubectl.CombinedOutput([]string{"cluster-info"})
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterNotAccessible, err, fmt.Sprintf("cluster not accessible: %v", err))
		core.Error("Cluster not accessible")
		core.LogStructuredError(m.logger, wrappedErr, "Cluster not accessible")
		return wrappedErr
	}
	core.DefaultPrinter.Println(string(output))

	// Check nodes
	core.Section("Nodes")
	// #nosec G204 -- fixed kubectl command.
	if err := m.kubectl.RunWithOutput([]string{"get", "nodes"}, os.Stdout, os.Stderr); err != nil {
		core.Warn(fmt.Sprintf("Failed to get nodes: %v", err))
	}

	// Check CRD
	core.Section("MCP CRD")
	// #nosec G204 -- fixed kubectl command.
	if err := m.kubectl.RunWithOutput([]string{"get", "crd", core.MCPServerCRDName}, os.Stdout, os.Stderr); err != nil {
		core.Warn(fmt.Sprintf("Failed to get MCP CRD: %v", err))
	}

	// Check operator
	core.Section("Operator")
	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	if err := m.kubectl.RunWithOutput([]string{"get", "pods", "-n", core.NamespaceMCPRuntime}, os.Stdout, os.Stderr); err != nil {
		core.Warn(fmt.Sprintf("Failed to get operator pods: %v", err))
	}

	return nil
}

// ConfigureCluster configures cluster settings like ingress.
func (m *ClusterManager) ConfigureCluster(opts IngressOptions) error {
	m.logger.Info("Configuring cluster", zap.String("ingress", opts.Mode))

	mode := strings.ToLower(opts.Mode)
	switch mode {
	case "none":
		m.logger.Info("Skipping ingress controller install (ingress=none)")
		return nil
	case "traefik":
	default:
		err := core.NewWithSentinel(core.ErrUnsupportedIngressController, fmt.Sprintf("unsupported ingress controller: %s", opts.Mode))
		core.Error("Unsupported ingress controller")
		core.LogStructuredError(m.logger, err, "Unsupported ingress controller")
		return err
	}

	// Detect existing ingress classes to avoid double-install unless forced.
	hasIngress := false
	// #nosec G204 -- fixed kubectl command.
	out, err := m.kubectl.CombinedOutput([]string{"get", "ingressclass", "-o", "name"})
	if err == nil && strings.TrimSpace(string(out)) != "" {
		hasIngress = true
	}
	if hasIngress && !opts.Force {
		m.logger.Info("Ingress controller already present; skipping install", zap.String("ingress", opts.Mode))
		return nil
	}

	manifest := opts.Manifest
	if manifest == "" {
		manifest = "config/ingress/overlays/prod"
	}

	m.logger.Info("Installing ingress controller", zap.String("ingress", opts.Mode), zap.String("manifest", manifest))
	useKustomize := false
	manifestArg := manifest

	if info, err := os.Stat(manifest); err == nil {
		if info.IsDir() {
			useKustomize = true
		} else if strings.EqualFold(filepath.Base(manifest), "kustomization.yaml") {
			useKustomize = true
			manifestArg = filepath.Dir(manifest)
		}
	}

	args := []string{"apply"}
	if useKustomize {
		args = append(args, "-k", manifestArg)
	} else {
		args = append(args, "-f", manifest)
	}

	// #nosec G204 -- manifest path from internal config or CLI flag with file validation.
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to install ingress controller (%s): %v", opts.Mode, err),
			map[string]any{"ingress_mode": opts.Mode, "manifest": manifest, "component": "cluster"},
		)
		core.Error("Failed to install ingress controller")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to install ingress controller")
		return wrappedErr
	}

	m.logger.Info("Ingress controller installed successfully", zap.String("ingress", opts.Mode))
	m.logger.Info("Cluster configuration complete")
	return nil
}

// ConfigureClusterWithValues adapts exported flag values into the internal ingress options shape.
func (m *ClusterManager) ConfigureClusterWithValues(mode, manifest string, force bool) error {
	return m.ConfigureCluster(IngressOptions{
		Mode:     mode,
		Manifest: manifest,
		Force:    force,
	})
}

// ProvisionCluster provisions a new Kubernetes cluster. When dryRun is true,
// it prints the configuration and command that would run without creating
// any cluster or calling out to cloud APIs.
func (m *ClusterManager) ProvisionCluster(provider, region string, nodeCount int, clusterName string, dryRun bool) error {
	m.logger.Info("Provisioning cluster", zap.String("provider", provider), zap.String("region", region), zap.String("name", clusterName), zap.Bool("dry_run", dryRun))

	switch provider {
	case "kind":
		return m.provisionKindCluster(nodeCount, clusterName, dryRun)
	case "gke":
		return provisionGKECluster(m.logger, region, nodeCount, clusterName, dryRun)
	case "eks":
		return provisionEKSCluster(m.logger, m.exec, region, nodeCount, clusterName, dryRun)
	case "aks":
		return provisionAKSCluster(m.logger, region, nodeCount, clusterName, dryRun)
	default:
		err := core.NewWithSentinel(core.ErrUnsupportedProvider, fmt.Sprintf("unsupported provider: %s", provider))
		core.Error("Unsupported provider")
		core.LogStructuredError(m.logger, err, "Unsupported provider")
		return err
	}
}

func (m *ClusterManager) provisionKindCluster(nodeCount int, name string, dryRun bool) error {
	m.logger.Info("Provisioning Kind cluster")

	clusterName := name
	if clusterName == "" {
		clusterName = defaultClusterName
	}

	config := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
`
	for i := 1; i < nodeCount; i++ {
		config += "- role: worker\n"
	}

	if dryRun {
		core.Info(fmt.Sprintf("[dry-run] would write kind config and run: kind create cluster --name %s --config <tmp.yaml>", clusterName))
		core.Info("[dry-run] kind config that would be written:")
		core.DefaultPrinter.Println(config)
		core.Success("Dry-run complete; no cluster created")
		return nil
	}

	// Write config to temp file
	tmp, err := os.CreateTemp("", "mcp-kind-config-*.yaml")
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCreateKindConfigFailed, err, fmt.Sprintf("failed to create temp kind config: %v", err))
		core.Error("Failed to create kind config")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create kind config")
		return wrappedErr
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(config); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrCloseKindConfigFailed, errors.Join(err, closeErr), fmt.Sprintf("failed to close kind config after write error: %v", closeErr))
			core.Error("Failed to close kind config")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to close kind config")
			return wrappedErr
		}
		wrappedErr := core.WrapWithSentinel(core.ErrWriteKindConfigFailed, err, fmt.Sprintf("failed to write kind config: %v", err))
		core.Error("Failed to write kind config")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to write kind config")
		return wrappedErr
	}
	if err := tmp.Close(); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrCloseKindConfigFailed, err, fmt.Sprintf("failed to close kind config: %v", err))
		core.Error("Failed to close kind config")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to close kind config")
		return wrappedErr
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := m.exec.Command("kind", []string{"create", "cluster", "--config", tmp.Name(), "--name", clusterName})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	if err := cmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateKindClusterFailed,
			err,
			fmt.Sprintf("failed to create kind cluster: %v", err),
			map[string]any{"cluster_name": clusterName, "node_count": nodeCount, "component": "cluster"},
		)
		core.Error("Failed to create kind cluster")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create kind cluster")
		return wrappedErr
	}

	m.logger.Info("Kind cluster provisioned successfully")
	return nil
}

func provisionGKECluster(logger *zap.Logger, region string, nodeCount int, clusterName string, dryRun bool) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	if dryRun {
		core.Info(fmt.Sprintf("[dry-run] would run: gcloud container clusters create %s --region %s --num-nodes %d", clusterName, region, nodeCount))
		core.Success("Dry-run complete; no GKE call made")
		return nil
	}
	err := core.NewWithSentinel(core.ErrGKEProvisioningNotImplemented, fmt.Sprintf("GKE provisioning not yet implemented; create the cluster with gcloud, e.g. `gcloud container clusters create %s --region %s --num-nodes %d`", clusterName, region, nodeCount))
	core.Error("GKE provisioning not implemented")
	core.LogStructuredError(logger, err, "GKE provisioning not implemented")
	return err
}

func provisionEKSCluster(logger *zap.Logger, exec core.Executor, region string, nodeCount int, clusterName string, dryRun bool) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}

	args := []string{
		"create",
		"cluster",
		"--name", clusterName,
		"--region", region,
		"--nodes", fmt.Sprintf("%d", nodeCount),
	}
	if dryRun {
		core.Info(fmt.Sprintf("[dry-run] would run: eksctl %s", strings.Join(args, " ")))
		core.Success("Dry-run complete; no EKS call made")
		return nil
	}
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := exec.Command("eksctl", args, core.AllowlistBins("eksctl"), core.NoShellMeta(), core.NoControlChars())
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	logger.Info("Provisioning EKS cluster with eksctl", zap.String("name", clusterName), zap.String("region", region), zap.Int("nodes", nodeCount))
	if err := cmd.Run(); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrProvisionEKSFailed,
			err,
			fmt.Sprintf("failed to provision EKS cluster: %v", err),
			map[string]any{"cluster_name": clusterName, "region": region, "node_count": nodeCount, "component": "cluster"},
		)
		core.Error("Failed to provision EKS cluster")
		core.LogStructuredError(logger, wrappedErr, "Failed to provision EKS cluster")
		return wrappedErr
	}
	logger.Info("EKS cluster provisioned successfully", zap.String("name", clusterName))
	return nil
}

func provisionAKSCluster(logger *zap.Logger, region string, nodeCount int, clusterName string, dryRun bool) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	if dryRun {
		core.Info(fmt.Sprintf("[dry-run] would run: az aks create --name %s --resource-group <rg> --location %s --node-count %d", clusterName, region, nodeCount))
		core.Success("Dry-run complete; no AKS call made")
		return nil
	}
	err := core.NewWithSentinel(core.ErrAKSProvisioningNotImplemented, fmt.Sprintf("AKS provisioning not yet implemented; create the cluster with az, e.g. `az aks create --name %s --resource-group <rg> --location %s --node-count %d`", clusterName, region, nodeCount))
	core.Error("AKS provisioning not implemented")
	core.LogStructuredError(logger, err, "AKS provisioning not implemented")
	return err
}

// EnsureNamespace applies/creates a namespace idempotently.
func (m *ClusterManager) EnsureNamespace(name string) error {
	nsYAML := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, name)
	if name == core.NamespaceMCPServers {
		nsYAML = fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
`, name)
	}
	// #nosec G204 -- fixed kubectl command, input via stdin; name from internal code.
	cmd, err := m.kubectl.CommandArgs([]string{"apply", "--validate=false", "-f", "-"})
	if err != nil {
		return err
	}
	cmd.SetStdin(strings.NewReader(nsYAML))
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}
