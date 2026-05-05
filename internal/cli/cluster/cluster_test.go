package cluster

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func TestClusterManager_CheckClusterStatus(t *testing.T) {
	t.Run("calls kubectl cluster-info", func(t *testing.T) {
		mock := &core.MockExecutor{
			DefaultOutput: []byte("Kubernetes control plane is running"),
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.CheckClusterStatus()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}

		// Should call cluster-info
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "cluster-info") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl cluster-info to be called")
		}
	})
}

func TestClusterConfigRunE_WithProviderAndContext(t *testing.T) {
	mockExec := &core.MockExecutor{}
	mockKubectl := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mockKubectl)
	mgr := NewClusterManager(kubectl, mockExec, zap.NewNop())

	configCmd := findClusterSubcommand(t, newTestClusterCommand(mgr), "config")

	tempDir := t.TempDir()
	kubeconfigPath := filepath.Join(tempDir, "config")
	if err := os.WriteFile(kubeconfigPath, []byte("kubeconfig"), 0o644); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	manifestPath := filepath.Join(tempDir, "ingress.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: List\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := configCmd.Flags().Set("provider", "eks"); err != nil {
		t.Fatalf("set provider: %v", err)
	}
	if err := configCmd.Flags().Set("region", "us-east-1"); err != nil {
		t.Fatalf("set region: %v", err)
	}
	if err := configCmd.Flags().Set("name", "cluster-1"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	if err := configCmd.Flags().Set("kubeconfig", kubeconfigPath); err != nil {
		t.Fatalf("set kubeconfig: %v", err)
	}
	if err := configCmd.Flags().Set("context", "dev"); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if err := configCmd.Flags().Set("ingress-manifest", manifestPath); err != nil {
		t.Fatalf("set ingress-manifest: %v", err)
	}

	if err := configCmd.RunE(configCmd, nil); err != nil {
		t.Fatalf("config RunE error: %v", err)
	}

	if !hasCommand(mockExec.Commands, "aws", "eks", "update-kubeconfig", "--name", "cluster-1", "--region", "us-east-1", "--kubeconfig", kubeconfigPath) {
		t.Fatalf("expected aws update-kubeconfig call, got: %#v", mockExec.Commands)
	}
	if !hasCommand(mockKubectl.Commands, "kubectl", "config", "use-context", "dev") {
		t.Fatalf("expected kubectl config use-context call, got: %#v", mockKubectl.Commands)
	}
	if !hasCommand(mockKubectl.Commands, "kubectl", "get", "ingressclass", "-o", "name") {
		t.Fatalf("expected kubectl get ingressclass call, got: %#v", mockKubectl.Commands)
	}
	if !hasCommand(mockKubectl.Commands, "kubectl", "apply", "-f", manifestPath) {
		t.Fatalf("expected kubectl apply call, got: %#v", mockKubectl.Commands)
	}
}

func TestClusterConfigRunE_UnsupportedProvider(t *testing.T) {
	mockExec := &core.MockExecutor{}
	mockKubectl := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mockKubectl)
	mgr := NewClusterManager(kubectl, mockExec, zap.NewNop())

	configCmd := findClusterSubcommand(t, newTestClusterCommand(mgr), "config")
	if err := configCmd.Flags().Set("provider", "unknown"); err != nil {
		t.Fatalf("set provider: %v", err)
	}

	if err := configCmd.RunE(configCmd, nil); err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if len(mockExec.Commands) > 0 || len(mockKubectl.Commands) > 0 {
		t.Fatalf("expected no commands to be executed, got exec=%v kubectl=%v", mockExec.Commands, mockKubectl.Commands)
	}
}

func findClusterSubcommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, cmd := range root.Commands() {
		if cmd.Use == name {
			return cmd
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

func newTestClusterCommand(mgr *ClusterManager) *cobra.Command {
	cmd := &cobra.Command{Use: "cluster"}

	var ingressMode string
	var ingressManifest string
	var forceIngressInstall bool
	var configKubeconfig string
	var configContext string
	var provider string
	var region string
	var clusterName string
	var resourceGroup string
	var project string
	var zone string
	configCmd := &cobra.Command{
		Use: "config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if provider != "" {
				if err := mgr.ConfigureKubeconfigFromProvider(provider, region, clusterName, resourceGroup, project, zone, configKubeconfig); err != nil {
					return err
				}
			}
			if configKubeconfig != "" || configContext != "" || provider != "" {
				if err := mgr.ConfigureKubeconfig(configKubeconfig, configContext); err != nil {
					return err
				}
			}
			return mgr.ConfigureClusterWithValues(ingressMode, ingressManifest, forceIngressInstall)
		},
	}
	configCmd.Flags().StringVar(&ingressMode, "ingress", "traefik", "")
	configCmd.Flags().StringVar(&ingressManifest, "ingress-manifest", "config/ingress/overlays/prod", "")
	configCmd.Flags().BoolVar(&forceIngressInstall, "force-ingress-install", false, "")
	configCmd.Flags().StringVar(&configKubeconfig, "kubeconfig", "", "")
	configCmd.Flags().StringVar(&configContext, "context", "", "")
	configCmd.Flags().StringVar(&provider, "provider", "", "")
	configCmd.Flags().StringVar(&region, "region", "us-west-1", "")
	configCmd.Flags().StringVar(&clusterName, "name", "mcp-runtime", "")
	configCmd.Flags().StringVar(&resourceGroup, "resource-group", "", "")
	configCmd.Flags().StringVar(&project, "project", "", "")
	configCmd.Flags().StringVar(&zone, "zone", "", "")

	cmd.AddCommand(configCmd)
	return cmd
}

func hasCommand(cmds []core.ExecSpec, name string, args ...string) bool {
	for _, cmd := range cmds {
		if cmd.Name != name {
			continue
		}
		if containsAll(cmd.Args, args) {
			return true
		}
	}
	return false
}

func containsAll(slice []string, vals []string) bool {
	for _, val := range vals {
		if !contains(slice, val) {
			return false
		}
	}
	return true
}

func TestClusterManager_EnsureNamespace(t *testing.T) {
	t.Run("calls kubectl apply with namespace yaml via stdin", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.EnsureNamespace("test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kubectl" {
			t.Errorf("expected kubectl, got %s", cmd.Name)
		}
		// Uses apply -f - (stdin)
		if !contains(cmd.Args, "apply") || !contains(cmd.Args, "-f") || !contains(cmd.Args, "-") {
			t.Errorf("expected 'apply -f -' in args, got %v", cmd.Args)
		}
	})
}

func TestClusterManager_InitCluster(t *testing.T) {
	t.Run("sets kubeconfig context when provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfig := filepath.Join(tmpDir, "config")
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatalf("failed to write kubeconfig: %v", err)
		}

		mock := &core.MockExecutor{
			DefaultOutput: []byte("Switched to context"),
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.InitCluster(kubeconfig, "my-context")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should call use-context
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "use-context") && contains(cmd.Args, "my-context") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl config use-context my-context to be called")
		}
	})
}

func TestClusterManager_ConfigureKubeconfig(t *testing.T) {
	t.Run("sets KUBECONFIG and switches context", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfig := filepath.Join(tmpDir, "config")
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatalf("failed to write kubeconfig: %v", err)
		}

		mock := &core.MockExecutor{
			DefaultOutput: []byte("Switched to context"),
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		previous := os.Getenv("KUBECONFIG")
		t.Cleanup(func() {
			if err := os.Setenv("KUBECONFIG", previous); err != nil {
				t.Fatalf("failed to restore KUBECONFIG: %v", err)
			}
		})

		if err := mgr.ConfigureKubeconfig(kubeconfig, "my-context"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := os.Getenv("KUBECONFIG"); got != kubeconfig {
			t.Fatalf("expected KUBECONFIG=%q, got %q", kubeconfig, got)
		}

		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "use-context") && contains(cmd.Args, "my-context") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl config use-context my-context to be called")
		}
	})

	t.Run("uses default kubeconfig when empty path provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		defaultPath := filepath.Join(tmpDir, ".kube", "config")
		if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
			t.Fatalf("failed to create .kube dir: %v", err)
		}
		if err := os.WriteFile(defaultPath, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatalf("failed to write kubeconfig: %v", err)
		}

		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		previous := os.Getenv("KUBECONFIG")
		t.Cleanup(func() {
			if err := os.Setenv("KUBECONFIG", previous); err != nil {
				t.Fatalf("failed to restore KUBECONFIG: %v", err)
			}
		})

		if err := mgr.ConfigureKubeconfig("", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := os.Getenv("KUBECONFIG"); got != defaultPath {
			t.Fatalf("expected KUBECONFIG=%q, got %q", defaultPath, got)
		}
	})

	t.Run("errors when kubeconfig is missing", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		if err := mgr.ConfigureKubeconfig("/path/does/not/exist", ""); err == nil {
			t.Fatal("expected error for missing kubeconfig")
		}
	})
}

func TestClusterManager_ConfigureKubeconfigFromProvider(t *testing.T) {
	t.Run("dispatches to eks config", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureKubeconfigFromProvider("EKS", "us-west-2", "my-eks", "", "", "", "/tmp/kubeconfig")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "aws" {
			t.Fatalf("expected aws command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "eks") || !contains(cmd.Args, "update-kubeconfig") {
			t.Fatalf("expected aws eks update-kubeconfig args, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-eks") {
			t.Fatalf("expected --name my-eks, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--region") || !contains(cmd.Args, "us-west-2") {
			t.Fatalf("expected --region us-west-2, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--kubeconfig") || !contains(cmd.Args, "/tmp/kubeconfig") {
			t.Fatalf("expected --kubeconfig /tmp/kubeconfig, got %v", cmd.Args)
		}
	})

	t.Run("errors on unsupported provider", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		if err := mgr.ConfigureKubeconfigFromProvider("digitalocean", "us-west-2", "cluster", "", "", "", ""); err == nil {
			t.Fatal("expected error for unsupported provider")
		} else if !strings.Contains(err.Error(), "unsupported provider") {
			t.Fatalf("expected unsupported provider error, got %v", err)
		}
	})
}

func TestProvisionEKSCluster(t *testing.T) {
	t.Run("uses eksctl with args", func(t *testing.T) {
		mock := &core.MockExecutor{}
		err := provisionEKSCluster(zap.NewNop(), mock, "us-west-2", 3, "my-eks", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "eksctl" {
			t.Fatalf("expected eksctl command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "create") || !contains(cmd.Args, "cluster") {
			t.Fatalf("expected eksctl create cluster args, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-eks") {
			t.Fatalf("expected --name my-eks, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--region") || !contains(cmd.Args, "us-west-2") {
			t.Fatalf("expected --region us-west-2, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--nodes") || !contains(cmd.Args, "3") {
			t.Fatalf("expected --nodes 3, got %v", cmd.Args)
		}
	})

	t.Run("defaults cluster name when empty", func(t *testing.T) {
		mock := &core.MockExecutor{}
		err := provisionEKSCluster(zap.NewNop(), mock, "us-west-2", 2, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, defaultClusterName) {
			t.Fatalf("expected --name %s, got %v", defaultClusterName, cmd.Args)
		}
	})
}

func TestConfigureEKSKubeconfig(t *testing.T) {
	t.Run("uses aws eks update-kubeconfig", func(t *testing.T) {
		mock := &core.MockExecutor{}
		err := configureEKSKubeconfig(mock, "us-west-2", "my-eks", "/tmp/kubeconfig")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "aws" {
			t.Fatalf("expected aws command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "eks") || !contains(cmd.Args, "update-kubeconfig") {
			t.Fatalf("expected aws eks update-kubeconfig args, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-eks") {
			t.Fatalf("expected --name my-eks, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--region") || !contains(cmd.Args, "us-west-2") {
			t.Fatalf("expected --region us-west-2, got %v", cmd.Args)
		}
		if !contains(cmd.Args, "--kubeconfig") || !contains(cmd.Args, "/tmp/kubeconfig") {
			t.Fatalf("expected --kubeconfig /tmp/kubeconfig, got %v", cmd.Args)
		}
	})

	t.Run("defaults cluster name when empty", func(t *testing.T) {
		mock := &core.MockExecutor{}
		err := configureEKSKubeconfig(mock, "us-west-2", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, defaultClusterName) {
			t.Fatalf("expected --name %s, got %v", defaultClusterName, cmd.Args)
		}
	})

	t.Run("returns error when aws command fails", func(t *testing.T) {
		mock := &core.MockExecutor{DefaultRunErr: errors.New("aws failed")}
		err := configureEKSKubeconfig(mock, "us-west-2", "my-eks", "")
		if err == nil {
			t.Fatal("expected error when aws fails")
		}
	})
}

func TestProvisionCluster(t *testing.T) {
	t.Run("dispatches to kind", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ProvisionCluster("kind", "us-west-2", 3, "test-cluster", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.HasCommand("kind") {
			t.Error("expected kind command")
		}
	})

	t.Run("returns gke not implemented error", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ProvisionCluster("gke", "us-west-2", 3, "test-cluster", false)
		if err == nil {
			t.Fatal("expected error for gke")
		}
		if !strings.Contains(err.Error(), "not yet implemented") {
			t.Fatalf("expected not implemented error, got: %v", err)
		}
	})

	t.Run("returns aks not implemented error", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ProvisionCluster("aks", "us-west-2", 3, "test-cluster", false)
		if err == nil {
			t.Fatal("expected error for aks")
		}
		if !strings.Contains(err.Error(), "not yet implemented") {
			t.Fatalf("expected not implemented error, got: %v", err)
		}
	})

	t.Run("returns error for unsupported provider", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ProvisionCluster("unknown", "us-west-2", 3, "test-cluster", false)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
		if !strings.Contains(err.Error(), "unsupported provider") {
			t.Fatalf("expected unsupported provider error, got: %v", err)
		}
	})

	t.Run("eks provisioning", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ProvisionCluster("eks", "us-west-2", 3, "test-cluster", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.HasCommand("eksctl") {
			t.Error("expected eksctl command")
		}
	})
}

func TestProvisionKindCluster(t *testing.T) {
	t.Run("creates cluster with default name", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.provisionKindCluster(3, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if cmd.Name != "kind" {
			t.Fatalf("expected kind command, got %q", cmd.Name)
		}
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, defaultClusterName) {
			t.Fatalf("expected --name %s, got %v", defaultClusterName, cmd.Args)
		}
	})

	t.Run("creates cluster with custom name", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.provisionKindCluster(2, "my-cluster", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cmd := mock.LastCommand()
		if !contains(cmd.Args, "--name") || !contains(cmd.Args, "my-cluster") {
			t.Fatalf("expected --name my-cluster, got %v", cmd.Args)
		}
	})

	t.Run("returns error when kind fails", func(t *testing.T) {
		mock := &core.MockExecutor{DefaultRunErr: errors.New("kind failed")}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.provisionKindCluster(1, "test", false)
		if err == nil {
			t.Fatal("expected error when kind fails")
		}
	})
}

func TestProvisionGKECluster(t *testing.T) {
	t.Run("defaults cluster name", func(t *testing.T) {
		err := provisionGKECluster(zap.NewNop(), "us-west-2", 3, "", false)
		if err == nil {
			t.Fatal("expected not implemented error")
		}
		if !strings.Contains(err.Error(), defaultClusterName) {
			t.Fatalf("expected default cluster name in error, got: %v", err)
		}
	})
}

func TestProvisionAKSCluster(t *testing.T) {
	t.Run("defaults cluster name", func(t *testing.T) {
		err := provisionAKSCluster(zap.NewNop(), "us-west-2", 3, "", false)
		if err == nil {
			t.Fatal("expected not implemented error")
		}
		if !strings.Contains(err.Error(), defaultClusterName) {
			t.Fatalf("expected default cluster name in error, got: %v", err)
		}
	})
}

func TestProvisionEKSClusterError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("eksctl failed")}
	err := provisionEKSCluster(zap.NewNop(), mock, "us-west-2", 3, "test", false)
	if err == nil {
		t.Fatal("expected error when eksctl fails")
	}
}

func TestCheckClusterStatusError(t *testing.T) {
	mock := &core.MockExecutor{
		DefaultErr: errors.New("cluster not accessible"),
	}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewClusterManager(kubectl, mock, zap.NewNop())

	err := mgr.CheckClusterStatus()
	if err == nil {
		t.Fatal("expected error when cluster not accessible")
	}
}

func TestConfigureCluster(t *testing.T) {
	t.Run("skips ingress when mode is none", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "none"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not have called kubectl for ingress
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "ingressclass") {
				t.Error("should not check ingress class when mode is none")
			}
		}
	})

	t.Run("returns error for unsupported ingress", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "nginx"})
		if err == nil {
			t.Fatal("expected error for unsupported ingress")
		}
		if !strings.Contains(err.Error(), "unsupported ingress") {
			t.Fatalf("expected unsupported ingress error, got: %v", err)
		}
	})

	t.Run("skips install when ingress already present", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "ingressclass") {
					cmd.OutputData = []byte("ingressclass.networking.k8s.io/traefik\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not have called apply
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "apply") {
				t.Error("should not apply when ingress already present")
			}
		}
	})

	t.Run("forces install when flag set", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestPath := filepath.Join(tmpDir, "ingress.yaml")
		if err := os.WriteFile(manifestPath, []byte("kind: List\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "ingressclass") {
					cmd.OutputData = []byte("ingressclass.networking.k8s.io/traefik\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik", Force: true, Manifest: manifestPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have called apply despite ingress present
		found := false
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "apply") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected apply when force is true")
		}
	})

	t.Run("skips repo install when external traefik exists", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "traefik") && contains(spec.Args, "-n") && contains(spec.Args, "kube-system") {
					cmd.OutputData = []byte("traefik")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "apply") {
				t.Fatalf("should not apply repo-managed ingress when external traefik exists: %v", mock.Commands)
			}
		}
	})

	t.Run("force refuses external traefik", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "traefik") && contains(spec.Args, "-n") && contains(spec.Args, "kube-system") {
					cmd.OutputData = []byte("traefik")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik", Force: true})
		if err == nil {
			t.Fatal("expected force to refuse external traefik")
		}
		if !strings.Contains(err.Error(), "external Traefik exists") {
			t.Fatalf("expected external Traefik error, got: %v", err)
		}
	})

	t.Run("fails when duplicate traefik installs exist", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "traefik") && contains(spec.Args, "-n") &&
					(contains(spec.Args, "kube-system") || contains(spec.Args, "traefik")) {
					cmd.OutputData = []byte("traefik")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik"})
		if err == nil {
			t.Fatal("expected duplicate traefik error")
		}
		if !strings.Contains(err.Error(), "multiple Traefik installs detected") {
			t.Fatalf("expected duplicate Traefik error, got: %v", err)
		}
	})

	t.Run("uses kustomize for directory manifest", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "ingress")
		if err := os.MkdirAll(manifestDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestDir, "kustomization.yaml"), []byte("resources: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik", Manifest: manifestDir})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have called apply with -k
		found := false
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "apply") && contains(cmd.Args, "-k") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected apply -k for directory manifest")
		}
	})

	t.Run("uses kustomize for kustomization.yaml file", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "ingress")
		if err := os.MkdirAll(manifestDir, 0o755); err != nil {
			t.Fatal(err)
		}
		kustomizePath := filepath.Join(manifestDir, "kustomization.yaml")
		if err := os.WriteFile(kustomizePath, []byte("resources: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik", Manifest: kustomizePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have called apply with -k pointing to directory
		found := false
		for _, cmd := range mock.Commands {
			if contains(cmd.Args, "apply") && contains(cmd.Args, "-k") && contains(cmd.Args, manifestDir) {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected apply -k with directory for kustomization.yaml")
		}
	})

	t.Run("returns error when apply fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestPath := filepath.Join(tmpDir, "ingress.yaml")
		if err := os.WriteFile(manifestPath, []byte("kind: List\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "apply") {
					cmd.RunErr = errors.New("apply failed")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.ConfigureCluster(IngressOptions{Mode: "traefik", Manifest: manifestPath})
		if err == nil {
			t.Fatal("expected error when apply fails")
		}
	})
}

func TestInitClusterErrors(t *testing.T) {
	t.Run("returns error when CRD install fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfig := filepath.Join(tmpDir, "config")
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatal(err)
		}

		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if contains(spec.Args, "apply") &&
					contains(spec.Args, "-f") &&
					contains(spec.Args, "config/crd/bases/mcpruntime.org_mcpservers.yaml") {
					cmd.RunErr = errors.New("crd install failed")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.InitCluster(kubeconfig, "")
		if err == nil {
			t.Fatal("expected error when CRD install fails")
		}
	})

	t.Run("returns error when namespace creation fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfig := filepath.Join(tmpDir, "config")
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
			t.Fatal(err)
		}

		callCount := 0
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				callCount++
				cmd := &core.MockCommand{Args: spec.Args}
				// Fail on second apply (namespace creation)
				if contains(spec.Args, "apply") && callCount > 1 {
					cmd.RunErr = errors.New("namespace creation failed")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := NewClusterManager(kubectl, mock, zap.NewNop())

		err := mgr.InitCluster(kubeconfig, "")
		if err == nil {
			t.Fatal("expected error when namespace creation fails")
		}
	})
}

func TestEnsureNamespaceError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("apply failed")}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewClusterManager(kubectl, mock, zap.NewNop())

	err := mgr.EnsureNamespace("test-ns")
	if err == nil {
		t.Fatal("expected error when apply fails")
	}
}

func TestConfigureKubeconfigFromProviderAKS(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewClusterManager(kubectl, mock, zap.NewNop())

	err := mgr.ConfigureKubeconfigFromProvider("aks", "us-west-2", "cluster", "rg", "", "", "")
	if err == nil {
		t.Fatal("expected error for aks")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("expected not implemented error, got: %v", err)
	}
}

func TestConfigureKubeconfigFromProviderGKE(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewClusterManager(kubectl, mock, zap.NewNop())

	err := mgr.ConfigureKubeconfigFromProvider("gke", "us-west-2", "cluster", "", "project", "zone", "")
	if err == nil {
		t.Fatal("expected error for gke")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("expected not implemented error, got: %v", err)
	}
}

func TestResolveKubeconfigPath(t *testing.T) {
	t.Run("returns provided path", func(t *testing.T) {
		path, err := resolveKubeconfigPath("/custom/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != "/custom/path" {
			t.Fatalf("expected /custom/path, got %q", path)
		}
	})

	t.Run("returns default path when empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)

		path, err := resolveKubeconfigPath("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(tmpDir, ".kube", "config")
		if path != expected {
			t.Fatalf("expected %q, got %q", expected, path)
		}
	})
}

func TestConfigureClusterConfigCmdFlags(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	mgr := NewClusterManager(kubectl, mock, zap.NewNop())

	cmd := findClusterSubcommand(t, newTestClusterCommand(mgr), "config")

	// Verify all flags are registered
	flags := []string{"ingress", "ingress-manifest", "force-ingress-install", "kubeconfig", "context", "provider", "region", "name", "resource-group", "project", "zone"}
	for _, flag := range flags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected flag %q to be registered", flag)
		}
	}
}
