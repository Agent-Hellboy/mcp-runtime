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
const envMCPDefaultIngressHost = "MCP_DEFAULT_INGRESS_HOST"
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

// ResolveRegistryEndpoint returns the registry endpoint used by pulls and
// in-cluster skopeo: MCP_REGISTRY_ENDPOINT, then MCP_REGISTRY_HOST, then
// registry.<MCP_PLATFORM_DOMAIN>, then the local default. It deliberately
// skips MCP_REGISTRY_INGRESS_HOST, the public auth-protected host, so an
// install that only names its ingress still gets the "set
// MCP_REGISTRY_ENDPOINT" guidance instead of pulling through the public edge.
func ResolveRegistryEndpoint() string {
	for _, key := range []string{envMCPRegistryEndpoint, envMCPRegistryHost} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	if p := platformDomainFromEnv(); p != "" {
		return registryHostForDomain(p)
	}
	return DefaultRegistryHost
}

// ResolveMcpIngressHost is the public hostname for the MCP / gateway. All
// consumers use the same precedence: MCP_MCP_INGRESS_HOST,
// MCP_DEFAULT_INGRESS_HOST, then mcp.<MCP_PLATFORM_DOMAIN>.

// registryHostForDomain names the platform registry for a platform domain,
// without doubling a domain that already starts with "registry.".
func registryHostForDomain(domain string) string {
	return "registry." + strings.TrimPrefix(domain, "registry.")
}
func ResolveMcpIngressHost() string {
	for _, key := range []string{envMCPMcpIngressHost, envMCPDefaultIngressHost} {
		if h := normalizeIngressHost(os.Getenv(key)); h != "" {
			return h
		}
	}
	if p := platformDomainFromEnv(); p != "" {
		return "mcp." + p
	}
	return ""
}

func normalizeIngressHost(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		value = parsed.Host
	} else {
		value = strings.Trim(value, "/")
		if idx := strings.IndexByte(value, '/'); idx >= 0 {
			value = value[:idx]
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
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

// ResolveRegistryHost resolves the public host used for default image names,
// ingress, and registry credentials. Precedence is MCP_REGISTRY_INGRESS_HOST,
// MCP_REGISTRY_HOST, registry.<MCP_PLATFORM_DOMAIN>, then the local
// development default. MCP_REGISTRY_ENDPOINT is reserved for internal pulls
// and transfers, so it must not become a public host fallback.
func ResolveRegistryHost() string {
	for _, key := range []string{envMCPRegistryIngressHost, envMCPRegistryHost} {
		if host := strings.TrimSpace(os.Getenv(key)); host != "" {
			return host
		}
	}
	if p := platformDomainFromEnv(); p != "" {
		return registryHostForDomain(p)
	}
	return DefaultRegistryHost
}
