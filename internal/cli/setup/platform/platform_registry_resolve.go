package platform

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"mcp-runtime/internal/cli/core"
)

const registryServiceDNS = "registry.registry.svc.cluster.local"

func resolveInternalPlatformRegistryURLClientGo(logger *zap.Logger) string {
	cfg := core.DefaultCLIConfig
	registryEndpoint := ""
	registryIngressHost := ""
	registryPort := 5000
	if cfg != nil {
		registryEndpoint = cfg.RegistryEndpoint
		registryIngressHost = cfg.RegistryIngressHost
		registryPort = cfg.RegistryPort
	}
	if endpoint := strings.TrimSpace(registryEndpoint); endpoint != "" &&
		(registryEndpointExplicitlyConfiguredForPlatform() ||
			(endpoint != core.DefaultRegistryEndpoint && endpoint != strings.TrimSpace(registryIngressHost))) {
		return endpoint
	}

	portValue, portErr := registryServicePortClientGo()
	if os.Getenv("MCP_RUNTIME_TEST_MODE") == "1" {
		if portErr == nil && portValue != "" {
			return fmt.Sprintf("%s:%s", registryServiceDNS, portValue)
		}
		if logger != nil {
			logger.Warn("Could not detect registry service port in test mode, using default service DNS:port")
		}
		return fmt.Sprintf("%s:%d", registryServiceDNS, registryPort)
	}

	ip, ipErr := registryServiceClusterIPClientGo()
	if ipErr == nil && ip != "" && portErr == nil && portValue != "" {
		return fmt.Sprintf("%s:%s", ip, portValue)
	}
	if portErr == nil && portValue != "" {
		return fmt.Sprintf("%s:%s", registryServiceDNS, portValue)
	}

	if logger != nil {
		logger.Warn("Could not detect internal registry service port, using default service DNS:port")
	}
	return fmt.Sprintf("%s:%d", registryServiceDNS, registryPort)
}

func registryServiceClusterIPClientGo() (string, error) {
	clients, err := platformKubernetesClients()
	if err != nil {
		return "", err
	}
	service, err := clients.Clientset.CoreV1().Services(core.NamespaceRegistry).Get(context.Background(), core.RegistryServiceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(service.Spec.ClusterIP), nil
}

func registryServicePortClientGo() (string, error) {
	clients, err := platformKubernetesClients()
	if err != nil {
		return "", err
	}
	service, err := clients.Clientset.CoreV1().Services(core.NamespaceRegistry).Get(context.Background(), core.RegistryServiceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if len(service.Spec.Ports) == 0 {
		return "", fmt.Errorf("registry service has no ports")
	}
	return fmt.Sprint(service.Spec.Ports[0].Port), nil
}

func registryEndpointExplicitlyConfiguredForPlatform() bool {
	// Only MCP_REGISTRY_ENDPOINT explicitly names an external/override registry
	// that platform pods should use for image pulls. MCP_REGISTRY_HOST is the
	// public ingress hostname for external access; using it here would route
	// internal pod image pulls through the auth-protected public ingress, causing
	// a bootstrap deadlock where pods can't pull images until the auth service is
	// running, which can't start until it pulls its own image.
	value, ok := os.LookupEnv("MCP_REGISTRY_ENDPOINT")
	return ok && strings.TrimSpace(value) != ""
}
