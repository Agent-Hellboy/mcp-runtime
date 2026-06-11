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

// ResolveRegistryPullHost returns the registry host kubelet should use for
// in-cluster image pulls. Precedence: MCP_REGISTRY_PULL_HOST,
// MCP_REGISTRY_ENDPOINT, then bundled cluster DNS.
//
// Public ingress hostnames are intentionally excluded. Workload pods must pull
// from the internal registry endpoint, not the auth-protected external ingress.
func ResolveRegistryPullHost() string {
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryPullHost)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryEndpoint)); v != "" {
		return v
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

// DisplayImageReference rewrites internal registry image refs for user-facing
// display. It prefers the public registry host when configured, and otherwise
// strips the internal host so cluster-only endpoints do not leak into UI/API
// responses.
func DisplayImageReference(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	currentRegistry, rest, found := strings.Cut(image, "/")
	if !found {
		return image
	}
	if !looksLikeRegistryHost(currentRegistry) {
		return image
	}
	currentRegistry = strings.TrimSpace(currentRegistry)
	if !isInternalRegistryHost(currentRegistry) {
		return image
	}
	if publicRegistry := explicitDisplayRegistryHost(); publicRegistry != "" && publicRegistry != currentRegistry && looksLikeRegistryHost(publicRegistry) {
		if rewritten, ok := RewriteImageRegistryHost(image, publicRegistry); ok {
			return rewritten
		}
	}
	return rest
}

func isInternalRegistryHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if host == defaultRegistryPullHost {
		return true
	}
	for _, candidate := range []string{
		strings.TrimSpace(os.Getenv(envMCPRegistryPullHost)),
		strings.TrimSpace(os.Getenv(envMCPRegistryEndpoint)),
	} {
		if candidate != "" && candidate == host {
			return true
		}
	}
	return false
}

func looksLikeRegistryHost(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, ".") || strings.Contains(value, ":") || value == "localhost"
}

func explicitDisplayRegistryHost() string {
	for _, candidate := range []string{
		strings.TrimSpace(os.Getenv(envMCPRegistryIngressHost)),
		strings.TrimSpace(os.Getenv(envMCPRegistryHost)),
	} {
		if candidate != "" {
			return candidate
		}
	}
	if p := platformDomainFromEnv(); p != "" {
		return "registry." + p
	}
	return ""
}
