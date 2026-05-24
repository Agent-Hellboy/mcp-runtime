package metadata

import (
	"fmt"
	"os"
	"strings"
)

const (
	envMCPRegistryPullHost  = "MCP_REGISTRY_PULL_HOST"
	defaultRegistryPullHost = "registry.registry.svc.cluster.local:5000"
)

// ResolveRegistryPullHost returns the registry host kubelet should use for in-cluster
// image pulls. Precedence: MCP_REGISTRY_PULL_HOST, MCP_REGISTRY_ENDPOINT,
// MCP_REGISTRY_INGRESS_HOST, registry.<MCP_PLATFORM_DOMAIN>, then bundled cluster DNS.
//
// When a public TLS registry is configured via MCP_REGISTRY_INGRESS_HOST or
// MCP_PLATFORM_DOMAIN without an explicit pull host, that public hostname is used for
// kubelet pulls too — nodes can reach the TLS ingress directly, so no in-cluster
// service DNS rewrite is needed. imageRefForClusterPull skips the rewrite whenever
// pullHost equals the push host resolved by ResolveRegistryHost.
func ResolveRegistryPullHost() string {
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryPullHost)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryEndpoint)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryIngressHost)); v != "" {
		return v
	}
	if p := platformDomainFromEnv(); p != "" {
		return "registry." + p
	}
	return defaultRegistryPullHost
}

// RewriteImageRegistryHost replaces the registry portion of an image reference.
func RewriteImageRegistryHost(image, registry string) (string, bool) {
	image = strings.TrimSpace(image)
	registry = strings.TrimSpace(registry)
	if image == "" || registry == "" {
		return image, false
	}
	parts := strings.Split(image, "/")
	if len(parts) == 1 {
		return fmt.Sprintf("%s/%s", registry, image), true
	}
	first := parts[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		parts = parts[1:]
	}
	return fmt.Sprintf("%s/%s", registry, strings.Join(parts, "/")), true
}
