package cli

import (
	"fmt"
	"os"
	"os/exec"
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

// NewClusterCmd returns the root cluster subcommand (status/init/provision).
func NewClusterCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Kubernetes cluster",
		Long:  "Commands for managing the Kubernetes cluster",
	}

	cmd.AddCommand(newClusterInitCmd(logger))
	cmd.AddCommand(newClusterStatusCmd(logger))
	cmd.AddCommand(newClusterConfigCmd(logger))
	cmd.AddCommand(newClusterProvisionCmd(logger))

	return cmd
}

func newClusterInitCmd(logger *zap.Logger) *cobra.Command {
	var kubeconfig string
	var context string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize cluster configuration",
		Long:  "Initialize and configure the Kubernetes cluster for MCP platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return initCluster(logger, kubeconfig, context)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.Flags().StringVar(&context, "context", "", "Kubernetes context to use")

	return cmd
}

func newClusterStatusCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check cluster status",
		Long:  "Check the status of the Kubernetes cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkClusterStatus(logger)
		},
	}

	return cmd
}

func newClusterConfigCmd(logger *zap.Logger) *cobra.Command {
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
			return configureCluster(logger, opts)
		},
	}

	cmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "Ingress controller to install (traefik|none)")
	cmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/prod", "Manifest to apply when installing the ingress controller")
	cmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "Force ingress install even if an ingress class already exists")

	return cmd
}

func newClusterProvisionCmd(logger *zap.Logger) *cobra.Command {
	var provider string
	var region string
	var nodeCount int
	var clusterName string

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Provision a new cluster",
		Long:  "Provision a new Kubernetes cluster (requires cloud provider credentials)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return provisionCluster(logger, provider, region, nodeCount, clusterName)
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "kind", "Cloud provider (kind, gke, eks, aks)")
	cmd.Flags().StringVar(&region, "region", "us-west-1", "Region for cluster")
	cmd.Flags().IntVar(&nodeCount, "nodes", 3, "Number of nodes")
	cmd.Flags().StringVar(&clusterName, "name", defaultClusterName, "Cluster name (used by supported providers)")

	return cmd
}

func initCluster(logger *zap.Logger, kubeconfig, context string) error {
	logger.Info("Initializing cluster configuration")

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
	os.Setenv("KUBECONFIG", kubeconfig)

	if context != "" {
		// Switch to specified context
		if err := exec.Command("kubectl", "config", "use-context", context).Run(); err != nil {
			return fmt.Errorf("failed to set context: %w", err)
		}
	}

	// Install CRD
	logger.Info("Installing CRD")
	if err := exec.Command("kubectl", "apply", "--validate=false", "-f", "config/crd/bases/mcp.agent-hellboy.io_mcpservers.yaml").Run(); err != nil {
		return fmt.Errorf("failed to install CRD: %w", err)
	}

	// Create namespace
	logger.Info("Creating mcp-runtime namespace")
	if err := ensureNamespace("mcp-runtime"); err != nil {
		return fmt.Errorf("failed to ensure mcp-runtime namespace: %w", err)
	}

	logger.Info("Creating mcp-servers namespace")
	if err := ensureNamespace("mcp-servers"); err != nil {
		return fmt.Errorf("failed to ensure mcp-servers namespace: %w", err)
	}

	logger.Info("Cluster initialized successfully")
	return nil
}

func checkClusterStatus(logger *zap.Logger) error {
	logger.Info("Checking cluster status")

	// Check cluster connectivity
	cmd := execCommand("kubectl", "cluster-info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cluster not accessible: %w", err)
	}
	DefaultPrinter.Println(string(output))

	// Check nodes
	Section("Nodes")
	cmd = execCommand("kubectl", "get", "nodes")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Check CRD
	Section("MCP CRD")
	cmd = execCommand("kubectl", "get", "crd", "mcpservers.mcp.agent-hellboy.io")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Check operator
	Section("Operator")
	cmd = execCommand("kubectl", "get", "pods", "-n", "mcp-runtime")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	return nil
}

func configureCluster(logger *zap.Logger, ingress ingressOptions) error {
	logger.Info("Configuring cluster", zap.String("ingress", ingress.mode))

	mode := strings.ToLower(ingress.mode)
	switch mode {
	case "none":
		logger.Info("Skipping ingress controller install (ingress=none)")
		return nil
	case "traefik":
	default:
		return fmt.Errorf("unsupported ingress controller: %s", ingress.mode)
	}

	// Detect existing ingress classes to avoid double-install unless forced.
	hasIngress := false
	checkCmd := exec.Command("kubectl", "get", "ingressclass", "-o", "name")
	out, err := checkCmd.CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		hasIngress = true
	}
	if hasIngress && !ingress.force {
		logger.Info("Ingress controller already present; skipping install", zap.String("ingress", ingress.mode))
		return nil
	}

	manifest := ingress.manifest
	if manifest == "" {
		manifest = "config/ingress/overlays/prod"
	}

	logger.Info("Installing ingress controller", zap.String("ingress", ingress.mode), zap.String("manifest", manifest))
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

	applyCmd := exec.Command("kubectl", args...)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("failed to install ingress controller (%s): %w", ingress.mode, err)
	}

	logger.Info("Ingress controller installed successfully", zap.String("ingress", ingress.mode))
	logger.Info("Cluster configuration complete")
	return nil
}
func provisionCluster(logger *zap.Logger, provider, region string, nodeCount int, clusterName string) error {
	logger.Info("Provisioning cluster", zap.String("provider", provider), zap.String("region", region), zap.String("name", clusterName))

	switch provider {
	case "kind":
		return provisionKindCluster(logger, nodeCount, clusterName)
	case "gke":
		return provisionGKECluster(logger, region, nodeCount, clusterName)
	case "eks":
		return provisionEKSCluster(logger, region, nodeCount, clusterName)
	case "aks":
		return provisionAKSCluster(logger, region, nodeCount, clusterName)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}
}

func provisionKindCluster(logger *zap.Logger, nodeCount int, name string) error {
	logger.Info("Provisioning Kind cluster")

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
		tmp.Close()
		return fmt.Errorf("failed to write kind config: %w", err)
	}
	tmp.Close()

	cmd := exec.Command("kind", "create", "cluster", "--config", tmp.Name(), "--name", clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create kind cluster: %w", err)
	}

	logger.Info("Kind cluster provisioned successfully")
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

// ensureNamespace applies/creates a namespace idempotently.
func ensureNamespace(name string) error {
	nsYAML := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, name)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(nsYAML)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
