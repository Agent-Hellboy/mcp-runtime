package metadata

import (
	"net"
	"net/url"
	"os"
	"strings"
)

const envMCPRegistryEndpoint = "MCP_REGISTRY_ENDPOINT"
const envMCPRegistryHost = "MCP_REGISTRY_HOST"
const envMCPRegistryIngressHost = "MCP_REGISTRY_INGRESS_HOST"
const envMCPPlatformDomain = "MCP_PLATFORM_DOMAIN"
const envMCPMcpIngressHost = "MCP_MCP_INGRESS_HOST"
const envMCPPlatformIngressHost = "MCP_PLATFORM_INGRESS_HOST"

// NormalizePlatformDomain returns a lowercased FQDN suitable for
// "registry." + d and "mcp." + d, or an empty string if the input is unusable.
func NormalizePlatformDomain(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lows := strings.ToLower(s)
	if strings.HasPrefix(lows, "http://") || strings.HasPrefix(lows, "https://") {
		u, err := url.Parse(s)
		if err == nil && u.Host != "" {
			s = u.Host
		} else {
			// #nosec G104 -- if URL parse failed, use trimmed string without scheme heuristics
			s = strings.TrimPrefix(s, "https://")
			s = strings.TrimPrefix(s, "http://")
		}
	} else {
		s = strings.Trim(s, "/")
	}
	// Path-only URLs without scheme: e.g. "mcpruntime.com/something" -> "mcpruntime.com"
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func platformDomainFromEnv() string {
	return NormalizePlatformDomain(os.Getenv(envMCPPlatformDomain))
}

// ResolveRegistryEndpoint returns the registry hostname/endpoint for pulls and
// in-cluster skopeo (MCP_REGISTRY_ENDPOINT, then MCP_REGISTRY_HOST, then
// registry.<MCP_PLATFORM_DOMAIN> when the platform domain is set).
func ResolveRegistryEndpoint() string {
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryEndpoint)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envMCPRegistryHost)); v != "" {
		return v
	}
	if p := platformDomainFromEnv(); p != "" {
		return "registry." + p
	}
	return DefaultRegistryHost
}

// ResolveMcpIngressHost is the public hostname for the MCP / gateway (operator
// default): MCP_MCP_INGRESS_HOST, else mcp.<MCP_PLATFORM_DOMAIN> when the
// platform domain is set, else empty (operator falls back to spec or
// publicPathPrefix).
func ResolveMcpIngressHost() string {
	if h := strings.TrimSpace(os.Getenv(envMCPMcpIngressHost)); h != "" {
		return h
	}
	if p := platformDomainFromEnv(); p != "" {
		return "mcp." + p
	}
	return ""
}

// ResolvePlatformIngressHost is the public hostname for the platform / admin
// dashboard UI: MCP_PLATFORM_INGRESS_HOST, else platform.<MCP_PLATFORM_DOMAIN>
// when the platform domain is set, else empty (path-based dev routing is used).
func ResolvePlatformIngressHost() string {
	if h := strings.TrimSpace(os.Getenv(envMCPPlatformIngressHost)); h != "" {
		return h
	}
	if p := platformDomainFromEnv(); p != "" {
		return "platform." + p
	}
	return ""
}

// ResolveRegistryHost resolves the host used for default image names.
// Precedence: MCP_REGISTRY_INGRESS_HOST, legacy MCP_REGISTRY_HOST, then
// registry.<MCP_PLATFORM_DOMAIN>, else fallback default.
func ResolveRegistryHost() string {
	if host := strings.TrimSpace(os.Getenv(envMCPRegistryIngressHost)); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv(envMCPRegistryHost)); host != "" {
		return host
	}
	if p := platformDomainFromEnv(); p != "" {
		return "registry." + p
	}
	return DefaultRegistryHost
}
