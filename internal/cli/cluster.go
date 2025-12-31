package cli

// This file implements the "cluster" command for managing Kubernetes cluster operations.
// It handles cluster initialization, status checks, configuration, provisioning, and certificate management.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const defaultClusterName = "mcp-runtime"

type ingressOptions struct {
	mode     string
	manifest string
	force    bool
}

// ClusterManager handles cluster operations with injected dependencies.
type ClusterManager struct {
	kubectl *KubectlClient
	exec    Executor
	logger  *zap.Logger
}

// NewClusterManager creates a ClusterManager with the given dependencies.
func NewClusterManager(kubectl *KubectlClient, exec Executor, logger *zap.Logger) *ClusterManager {
	return &ClusterManager{
		kubectl: kubectl,
		exec:    exec,
		logger:  logger,
	}
}

// DefaultClusterManager returns a ClusterManager using default clients.
func DefaultClusterManager(logger *zap.Logger) *ClusterManager {
	return NewClusterManager(kubectlClient, execExecutor, logger)
}

// NewClusterCmd returns the root cluster subcommand (status/init/provision).
func NewClusterCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultClusterManager(logger)
	return NewClusterCmdWithManager(mgr)
}

// NewClusterCmdWithManager returns the cluster subcommand using the provided manager.
func NewClusterCmdWithManager(mgr *ClusterManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Kubernetes cluster",
		Long:  "Commands for managing the Kubernetes cluster",
	}

	cmd.AddCommand(mgr.newClusterInitCmd())
	cmd.AddCommand(mgr.newClusterStatusCmd())
	cmd.AddCommand(mgr.newClusterConfigCmd())
	cmd.AddCommand(mgr.newClusterProvisionCmd())
	cmd.AddCommand(mgr.newClusterCertCmd())

	return cmd
}

func (m *ClusterManager) newClusterInitCmd() *cobra.Command {
	var kubeconfig string
	var context string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize cluster configuration",
		Long:  "Initialize and configure the Kubernetes cluster for MCP platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.InitCluster(kubeconfig, context)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.Flags().StringVar(&context, "context", "", "Kubernetes context to use")

	return cmd
}

func (m *ClusterManager) newClusterStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check cluster status",
		Long:  "Check the status of the Kubernetes cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.CheckClusterStatus()
		},
	}

	return cmd
}

func (m *ClusterManager) newClusterConfigCmd() *cobra.Command {
	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool
	var kubeconfig string
	var context string
	var provider string
	var region string
	var clusterName string
	var resourceGroup string
	var project string
	var zone string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure cluster settings",
		Long:  "Configure cluster settings like ingress and kubeconfig context",
		RunE: func(cmd *cobra.Command, args []string) error {
			if provider != "" {
				if err := m.ConfigureKubeconfigFromProvider(provider, region, clusterName, resourceGroup, project, zone, kubeconfig); err != nil {
					return err
				}
			}
			if kubeconfig != "" || context != "" || provider != "" {
				if err := m.ConfigureKubeconfig(kubeconfig, context); err != nil {
					return err
				}
			}
			opts := ingressOptions{
				mode:     ingressMode,
				manifest: ingressManifest,
				force:    forceIngressInstall,
			}
			return m.ConfigureCluster(opts)
		},
	}

	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/prod", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force ingress install even if an ingress class already exists")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.Flags().StringVar(&context, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&provider, "provider", "", "Cloud provider for kubeconfig (eks; aks/gke planned)")
	cmd.Flags().StringVar(&region, "region", "us-west-1", "Region for cloud provider kubeconfig")
	cmd.Flags().StringVar(&clusterName, "name", defaultClusterName, "Cluster name for cloud provider kubeconfig")
	cmd.Flags().StringVar(&resourceGroup, "resource-group", "", "Resource group (AKS, planned)")
	cmd.Flags().StringVar(&project, "project", "", "Project ID (GKE, planned)")
	cmd.Flags().StringVar(&zone, "zone", "", "Zone (GKE, planned)")

	return cmd
}

func (m *ClusterManager) newClusterProvisionCmd() *cobra.Command {
	var provider string
	var region string
	var nodeCount int
	var clusterName string

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Provision a new cluster",
		Long:  "Provision a new Kubernetes cluster (requires cloud provider credentials)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ProvisionCluster(provider, region, nodeCount, clusterName)
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "kind", "Cloud provider (kind, gke, eks, aks)")
	cmd.Flags().StringVar(&region, "region", "us-west-1", "Region for cluster")
	cmd.Flags().IntVar(&nodeCount, "nodes", 3, "Number of nodes")
	cmd.Flags().StringVar(&clusterName, "name", defaultClusterName, "Cluster name (used by supported providers)")

	return cmd
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
		wrappedErr := wrapWithSentinel(ErrInstallCRDFailed, err, fmt.Sprintf("failed to install CRD: %v", err))
		Error("Failed to install CRD")
		logStructuredError(m.logger, wrappedErr, "Failed to install CRD")
		return wrappedErr
	}

	// Create namespace
	m.logger.Info("Creating mcp-runtime namespace")
	if err := m.EnsureNamespace(NamespaceMCPRuntime); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrEnsureRuntimeNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure mcp-runtime namespace: %v", err),
			map[string]any{"namespace": NamespaceMCPRuntime, "component": "cluster"},
		)
		Error("Failed to ensure mcp-runtime namespace")
		logStructuredError(m.logger, wrappedErr, "Failed to ensure mcp-runtime namespace")
		return wrappedErr
	}

	m.logger.Info("Creating mcp-servers namespace")
	if err := m.EnsureNamespace(NamespaceMCPServers); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrEnsureServersNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure mcp-servers namespace: %v", err),
			map[string]any{"namespace": NamespaceMCPServers, "component": "cluster"},
		)
		Error("Failed to ensure mcp-servers namespace")
		logStructuredError(m.logger, wrappedErr, "Failed to ensure mcp-servers namespace")
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
		wrappedErr := wrapWithSentinel(ErrGetHomeDirectoryFailed, err, fmt.Sprintf("failed to get home directory: %v", err))
		Error("Failed to get home directory")
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
		wrappedErr := wrapWithSentinelAndContext(
			ErrKubeconfigNotReadable,
			err,
			fmt.Sprintf("kubeconfig %q not found or not readable: %v", path, err),
			map[string]any{"kubeconfig": path, "component": "cluster"},
		)
		Error("Kubeconfig not readable")
		logStructuredError(m.logger, wrappedErr, "Kubeconfig not readable")
		return wrappedErr
	}

	if err := os.Setenv("KUBECONFIG", path); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrSetKubeconfigFailed,
			err,
			fmt.Sprintf("failed to set KUBECONFIG: %v", err),
			map[string]any{"kubeconfig": path, "component": "cluster"},
		)
		Error("Failed to set KUBECONFIG")
		logStructuredError(m.logger, wrappedErr, "Failed to set KUBECONFIG")
		return wrappedErr
	}

	if context != "" {
		// #nosec G204 -- context from CLI flag, kubectl validates context names.
		if err := m.kubectl.Run([]string{"config", "use-context", context}); err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				ErrSetContextFailed,
				err,
				fmt.Sprintf("failed to set context: %v", err),
				map[string]any{"context": context, "component": "cluster"},
			)
			Error("Failed to set context")
			logStructuredError(m.logger, wrappedErr, "Failed to set context")
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
		err := newWithSentinel(ErrAKSKubeconfigNotImplemented, "AKS kubeconfig not yet implemented; planned support (use `az aks get-credentials --name <cluster> --resource-group <rg>`)")
		Error("AKS kubeconfig not implemented")
		logStructuredError(m.logger, err, "AKS kubeconfig not implemented")
		return err
	case "gke":
		err := newWithSentinel(ErrGKEKubeconfigNotImplemented, "GKE kubeconfig not yet implemented; planned support (use `gcloud container clusters get-credentials <cluster> --region <region> --project <project>`)")
		Error("GKE kubeconfig not implemented")
		logStructuredError(m.logger, err, "GKE kubeconfig not implemented")
		return err
	default:
		err := newWithSentinel(ErrUnsupportedProvider, fmt.Sprintf("unsupported provider: %s", provider))
		Error("Unsupported provider")
		logStructuredError(m.logger, err, "Unsupported provider")
		return err
	}
}

func configureEKSKubeconfig(exec Executor, region, clusterName, kubeconfig string) error {
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
	cmd, err := exec.Command("aws", args, AllowlistBins("aws"), NoShellMeta(), NoControlChars())
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
		wrappedErr := wrapWithSentinel(ErrClusterNotAccessible, err, fmt.Sprintf("cluster not accessible: %v", err))
		Error("Cluster not accessible")
		logStructuredError(m.logger, wrappedErr, "Cluster not accessible")
		return wrappedErr
	}
	DefaultPrinter.Println(string(output))

	// Check nodes
	Section("Nodes")
	// #nosec G204 -- fixed kubectl command.
	if err := m.kubectl.RunWithOutput([]string{"get", "nodes"}, os.Stdout, os.Stderr); err != nil {
		Warn(fmt.Sprintf("Failed to get nodes: %v", err))
	}

	// Check CRD
	Section("MCP CRD")
	// #nosec G204 -- fixed kubectl command.
	if err := m.kubectl.RunWithOutput([]string{"get", "crd", MCPServerCRDName}, os.Stdout, os.Stderr); err != nil {
		Warn(fmt.Sprintf("Failed to get MCP CRD: %v", err))
	}

	// Check operator
	Section("Operator")
	// #nosec G204 -- fixed kubectl command with hardcoded namespace.
	if err := m.kubectl.RunWithOutput([]string{"get", "pods", "-n", NamespaceMCPRuntime}, os.Stdout, os.Stderr); err != nil {
		Warn(fmt.Sprintf("Failed to get operator pods: %v", err))
	}

	return nil
}

// ConfigureCluster configures cluster settings like ingress.
func (m *ClusterManager) ConfigureCluster(ingress ingressOptions) error {
	m.logger.Info("Configuring cluster", zap.String("ingress", ingress.mode))

	mode := strings.ToLower(ingress.mode)
	switch mode {
	case "none":
		m.logger.Info("Skipping ingress controller install (ingress=none)")
		return nil
	case "traefik":
	default:
		err := newWithSentinel(ErrUnsupportedIngressController, fmt.Sprintf("unsupported ingress controller: %s", ingress.mode))
		Error("Unsupported ingress controller")
		logStructuredError(m.logger, err, "Unsupported ingress controller")
		return err
	}

	// Detect existing ingress classes to avoid double-install unless forced.
	hasIngress := false
	// #nosec G204 -- fixed kubectl command.
	out, err := m.kubectl.CombinedOutput([]string{"get", "ingressclass", "-o", "name"})
	if err == nil && strings.TrimSpace(string(out)) != "" {
		hasIngress = true
	}
	if hasIngress && !ingress.force {
		m.logger.Info("Ingress controller already present; skipping install", zap.String("ingress", ingress.mode))
		return nil
	}

	manifest := ingress.manifest
	if manifest == "" {
		manifest = "config/ingress/overlays/prod"
	}

	m.logger.Info("Installing ingress controller", zap.String("ingress", ingress.mode), zap.String("manifest", manifest))
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
		wrappedErr := wrapWithSentinelAndContext(
			ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to install ingress controller (%s): %v", ingress.mode, err),
			map[string]any{"ingress_mode": ingress.mode, "manifest": manifest, "component": "cluster"},
		)
		Error("Failed to install ingress controller")
		logStructuredError(m.logger, wrappedErr, "Failed to install ingress controller")
		return wrappedErr
	}

	m.logger.Info("Ingress controller installed successfully", zap.String("ingress", ingress.mode))
	m.logger.Info("Cluster configuration complete")
	return nil
}

// ProvisionCluster provisions a new Kubernetes cluster.
func (m *ClusterManager) ProvisionCluster(provider, region string, nodeCount int, clusterName string) error {
	m.logger.Info("Provisioning cluster", zap.String("provider", provider), zap.String("region", region), zap.String("name", clusterName))

	switch provider {
	case "kind":
		return m.provisionKindCluster(nodeCount, clusterName)
	case "gke":
		return provisionGKECluster(m.logger, region, nodeCount, clusterName)
	case "eks":
		return provisionEKSCluster(m.logger, m.exec, region, nodeCount, clusterName)
	case "aks":
		return provisionAKSCluster(m.logger, region, nodeCount, clusterName)
	default:
		err := newWithSentinel(ErrUnsupportedProvider, fmt.Sprintf("unsupported provider: %s", provider))
		Error("Unsupported provider")
		logStructuredError(m.logger, err, "Unsupported provider")
		return err
	}
}

func (m *ClusterManager) provisionKindCluster(nodeCount int, name string) error {
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

	// Write config to temp file
	tmp, err := os.CreateTemp("", "mcp-kind-config-*.yaml")
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrCreateKindConfigFailed, err, fmt.Sprintf("failed to create temp kind config: %v", err))
		Error("Failed to create kind config")
		logStructuredError(m.logger, wrappedErr, "Failed to create kind config")
		return wrappedErr
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(config); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			wrappedErr := wrapWithSentinel(ErrCloseKindConfigFailed, errors.Join(err, closeErr), fmt.Sprintf("failed to close kind config after write error: %v", closeErr))
			Error("Failed to close kind config")
			logStructuredError(m.logger, wrappedErr, "Failed to close kind config")
			return wrappedErr
		}
		wrappedErr := wrapWithSentinel(ErrWriteKindConfigFailed, err, fmt.Sprintf("failed to write kind config: %v", err))
		Error("Failed to write kind config")
		logStructuredError(m.logger, wrappedErr, "Failed to write kind config")
		return wrappedErr
	}
	if err := tmp.Close(); err != nil {
		wrappedErr := wrapWithSentinel(ErrCloseKindConfigFailed, err, fmt.Sprintf("failed to close kind config: %v", err))
		Error("Failed to close kind config")
		logStructuredError(m.logger, wrappedErr, "Failed to close kind config")
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
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateKindClusterFailed,
			err,
			fmt.Sprintf("failed to create kind cluster: %v", err),
			map[string]any{"cluster_name": clusterName, "node_count": nodeCount, "component": "cluster"},
		)
		Error("Failed to create kind cluster")
		logStructuredError(m.logger, wrappedErr, "Failed to create kind cluster")
		return wrappedErr
	}

	m.logger.Info("Kind cluster provisioned successfully")
	return nil
}

func provisionGKECluster(logger *zap.Logger, region string, nodeCount int, clusterName string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	err := newWithSentinel(ErrGKEProvisioningNotImplemented, fmt.Sprintf("GKE provisioning not yet implemented; create the cluster with gcloud, e.g. `gcloud container clusters create %s --region %s --num-nodes %d`", clusterName, region, nodeCount))
	Error("GKE provisioning not implemented")
	logStructuredError(logger, err, "GKE provisioning not implemented")
	return err
}

func provisionEKSCluster(logger *zap.Logger, exec Executor, region string, nodeCount int, clusterName string) error {
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
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := exec.Command("eksctl", args, AllowlistBins("eksctl"), NoShellMeta(), NoControlChars())
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	logger.Info("Provisioning EKS cluster with eksctl", zap.String("name", clusterName), zap.String("region", region), zap.Int("nodes", nodeCount))
	if err := cmd.Run(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrProvisionEKSFailed,
			err,
			fmt.Sprintf("failed to provision EKS cluster: %v", err),
			map[string]any{"cluster_name": clusterName, "region": region, "node_count": nodeCount, "component": "cluster"},
		)
		Error("Failed to provision EKS cluster")
		logStructuredError(logger, wrappedErr, "Failed to provision EKS cluster")
		return wrappedErr
	}
	logger.Info("EKS cluster provisioned successfully", zap.String("name", clusterName))
	return nil
}

func provisionAKSCluster(logger *zap.Logger, region string, nodeCount int, clusterName string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	err := newWithSentinel(ErrAKSProvisioningNotImplemented, fmt.Sprintf("AKS provisioning not yet implemented; create the cluster with az, e.g. `az aks create --name %s --resource-group <rg> --location %s --node-count %d`", clusterName, region, nodeCount))
	Error("AKS provisioning not implemented")
	logStructuredError(logger, err, "AKS provisioning not implemented")
	return err
}

// EnsureNamespace applies/creates a namespace idempotently.
func (m *ClusterManager) EnsureNamespace(name string) error {
	nsYAML := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, name)
	// #nosec G204 -- fixed kubectl command, input via stdin; name from internal code.
	cmd, err := m.kubectl.CommandArgs([]string{"apply", "-f", "-"})
	if err != nil {
		return err
	}
	cmd.SetStdin(strings.NewReader(nsYAML))
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

// ensureNamespace is a package-level helper that uses the default kubectl client.
// Used by other modules that don't have a ClusterManager instance.
func ensureNamespace(name string) error {
	mgr := DefaultClusterManager(zap.NewNop())
	return mgr.EnsureNamespace(name)
}
