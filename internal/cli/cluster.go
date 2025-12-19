package cli

import (
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

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure cluster settings",
		Long:  "Configure cluster settings like ingress, storage, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {
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

	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	if _, err := os.Stat(kubeconfig); err != nil {
		return fmt.Errorf("kubeconfig %q not found or not readable: %w", kubeconfig, err)
	}

	// Set KUBECONFIG environment variable
	if err := os.Setenv("KUBECONFIG", kubeconfig); err != nil {
		return fmt.Errorf("failed to set KUBECONFIG: %w", err)
	}

	if context != "" {
		// Switch to specified context
		// #nosec G204 -- context from CLI flag, kubectl validates context names.
		if err := m.kubectl.Run([]string{"config", "use-context", context}); err != nil {
			return fmt.Errorf("failed to set context: %w", err)
		}
	}

	// Install CRD
	m.logger.Info("Installing CRD")
	// #nosec G204 -- fixed file path from repository.
	if err := m.kubectl.Run([]string{"apply", "--validate=false", "-f", "config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml"}); err != nil {
		return fmt.Errorf("failed to install CRD: %w", err)
	}

	// Create namespace
	m.logger.Info("Creating mcp-runtime namespace")
	if err := m.EnsureNamespace(NamespaceMCPRuntime); err != nil {
		return fmt.Errorf("failed to ensure mcp-runtime namespace: %w", err)
	}

	m.logger.Info("Creating mcp-servers namespace")
	if err := m.EnsureNamespace(NamespaceMCPServers); err != nil {
		return fmt.Errorf("failed to ensure mcp-servers namespace: %w", err)
	}

	m.logger.Info("Cluster initialized successfully")
	return nil
}

// CheckClusterStatus checks and displays cluster status.
func (m *ClusterManager) CheckClusterStatus() error {
	m.logger.Info("Checking cluster status")

	// Check cluster connectivity
	// #nosec G204 -- fixed kubectl command.
	output, err := m.kubectl.CombinedOutput([]string{"cluster-info"})
	if err != nil {
		return fmt.Errorf("cluster not accessible: %w", err)
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
		return fmt.Errorf("unsupported ingress controller: %s", ingress.mode)
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
		return fmt.Errorf("failed to install ingress controller (%s): %w", ingress.mode, err)
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
		return provisionEKSCluster(m.logger, region, nodeCount, clusterName)
	case "aks":
		return provisionAKSCluster(m.logger, region, nodeCount, clusterName)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
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
		return fmt.Errorf("failed to create temp kind config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(config); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("failed to close kind config after write error: %w", closeErr)
		}
		return fmt.Errorf("failed to write kind config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close kind config: %w", err)
	}

	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	cmd, err := m.exec.Command("kind", []string{"create", "cluster", "--config", tmp.Name(), "--name", clusterName})
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create kind cluster: %w", err)
	}

	m.logger.Info("Kind cluster provisioned successfully")
	return nil
}

func provisionGKECluster(logger *zap.Logger, region string, nodeCount int, clusterName string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	return fmt.Errorf("GKE provisioning not yet implemented; create the cluster with gcloud, e.g. `gcloud container clusters create %s --region %s --num-nodes %d`", clusterName, region, nodeCount)
}

func provisionEKSCluster(logger *zap.Logger, region string, nodeCount int, clusterName string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	return fmt.Errorf("EKS provisioning not yet implemented; create the cluster with eksctl, e.g. `eksctl create cluster --name %s --region %s --nodes %d`", clusterName, region, nodeCount)
}

func provisionAKSCluster(logger *zap.Logger, region string, nodeCount int, clusterName string) error {
	if clusterName == "" {
		clusterName = defaultClusterName
	}
	return fmt.Errorf("AKS provisioning not yet implemented; create the cluster with az, e.g. `az aks create --name %s --resource-group <rg> --location %s --node-count %d`", clusterName, region, nodeCount)
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
