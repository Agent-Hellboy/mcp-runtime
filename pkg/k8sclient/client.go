package k8sclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Config provides Kubernetes client configuration options.
type Config struct {
	KubeconfigPath string
}

// Clients holds all Kubernetes clients.
type Clients struct {
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	Config    *rest.Config
}

// New creates Kubernetes clients with in-cluster config or kubeconfig fallback.
func New() (*Clients, error) {
	return NewWithConfig(Config{})
}

// NewWithConfig creates Kubernetes clients with the provided configuration.
func NewWithConfig(cfg Config) (*Clients, error) {
	var restConfig *rest.Config
	var err error

	// Try in-cluster config first
	restConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		restConfig, err = buildKubeconfig(cfg.KubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes dynamic client: %w", err)
	}

	return &Clients{
		Clientset: clientset,
		Dynamic:   dynamicClient,
		Config:    restConfig,
	}, nil
}

// NewFromConfig creates clients from an existing rest.Config.
func NewFromConfig(restConfig *rest.Config) (*Clients, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes dynamic client: %w", err)
	}

	return &Clients{
		Clientset: clientset,
		Dynamic:   dynamicClient,
		Config:    restConfig,
	}, nil
}

func buildKubeconfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		kubeconfigPath = getDefaultKubeconfigPath()
	}

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig not found at %s", kubeconfigPath)
	}

	// Build from kubeconfig
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return config, nil
}

func getDefaultKubeconfigPath() string {
	// Check KUBECONFIG env var first
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		return envPath
	}

	// Default to ~/.kube/config
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}

// IsInCluster returns true if running inside a Kubernetes cluster.
func IsInCluster() bool {
	// Check for service account token
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return true
	}
	return false
}

// GetNamespace returns the current namespace or "default".
func GetNamespace() string {
	// Try to read from service account namespace file
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if namespace := strings.TrimSpace(string(data)); namespace != "" {
			return namespace
		}
	}

	// Fall back to env var
	if ns := os.Getenv("NAMESPACE"); ns != "" {
		return ns
	}

	return "default"
}
