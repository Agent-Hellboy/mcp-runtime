package setup

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry/config"
	setupplan "mcp-runtime/internal/cli/setup/plan"
)

// printPlatformEntrypoints prints the public URLs derived from
// MCP_PLATFORM_DOMAIN / MCP_*_INGRESS_HOST so the operator knows which
// hostnames must resolve in DNS and what the dashboard URL is.
func printPlatformEntrypoints(tlsEnabled bool) {
	scheme := "http://"
	if tlsEnabled {
		scheme = "https://"
	}
	registry := strings.TrimSpace(core.GetRegistryIngressHost())
	mcp := strings.TrimSpace(core.GetMcpIngressHost())
	platform := strings.TrimSpace(core.GetPlatformIngressHost())
	if registry == "" && mcp == "" && platform == "" {
		return
	}
	fmt.Println()
	fmt.Println("Public entrypoints:")
	if platform != "" {
		fmt.Printf("  Dashboard:  %s%s/\n", scheme, platform)
	}
	if registry != "" {
		fmt.Printf("  Registry:   %s%s/v2/\n", scheme, registry)
	}
	if mcp != "" {
		fmt.Printf("  MCP:        %s%s/<server-name>/mcp\n", scheme, mcp)
	}
	if platform != "" {
		fmt.Println("  (Make sure DNS A/AAAA records point platform./registry./mcp.<domain> at the cluster ingress.)")
	}
}

func resolveRegistrySetup(logger *zap.Logger, deps SetupDeps) (*config.ExternalRegistryConfig, bool, string) {
	extRegistry, err := deps.ResolveExternalRegistryConfig(nil)
	if err != nil {
		core.Warn(fmt.Sprintf("Could not load external registry config: %v", err))
	}
	usingExternalRegistry := extRegistry != nil
	return extRegistry, usingExternalRegistry, defaultRegistrySecretName
}

func validateNonTestSetup(plan setupplan.Plan, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry bool) error {
	if plan.TestMode {
		return nil
	}
	if !plan.StrictProd {
		return nil
	}
	if !plan.TLSEnabled {
		return core.NewWithSentinel(
			core.ErrSetupStepFailed,
			"strict production setup requires --with-tls; use normal setup for local HTTP/internal registry flows",
		)
	}
	if usingExternalRegistry && extRegistry != nil && strings.TrimSpace(extRegistry.URL) != "" {
		if isDevRegistryURL(extRegistry.URL) {
			return core.NewWithSentinel(
				core.ErrSetupStepFailed,
				fmt.Sprintf("strict production setup requires a stable production registry, got dev-only registry URL %q", extRegistry.URL),
			)
		}
		return nil
	}
	if isDevRegistryURL(core.GetRegistryEndpoint()) {
		return core.NewWithSentinel(
			core.ErrSetupStepFailed,
			fmt.Sprintf("strict production setup requires a stable internal registry endpoint; set MCP_REGISTRY_ENDPOINT (current %q)", core.GetRegistryEndpoint()),
		)
	}
	return nil
}

func setupWarnings(plan setupplan.Plan, extRegistry *config.ExternalRegistryConfig, usingExternalRegistry bool) []string {
	if plan.TestMode {
		return nil
	}

	var warnings []string
	if !plan.TLSEnabled {
		warnings = append(warnings, "Non-test setup is running without TLS. This is fine for local/internal registries but not recommended for production.")
	}

	if usingExternalRegistry && extRegistry != nil && strings.TrimSpace(extRegistry.URL) != "" {
		registryURL := strings.TrimSpace(extRegistry.URL)
		if strings.HasPrefix(strings.ToLower(registryURL), "http://") {
			warnings = append(warnings, fmt.Sprintf("External registry %q is using HTTP. This is acceptable for local environments but not recommended for production.", registryURL))
		}
		if isDevRegistryURL(registryURL) {
			warnings = append(warnings, fmt.Sprintf("External registry %q looks local/internal. Normal setup allows this, but use --strict-prod to enforce production-style validation.", registryURL))
		}
		return warnings
	}

	registryEndpoint := strings.TrimSpace(core.GetRegistryEndpoint())
	if registryEndpoint == "" {
		warnings = append(warnings, "Internal registry host is empty; setup will fall back to service DNS. This is fine for local clusters but not recommended for production.")
		return warnings
	}
	if isDevRegistryURL(registryEndpoint) {
		warnings = append(warnings, fmt.Sprintf("Internal registry endpoint %q looks local/internal. Normal setup allows this for local clusters, but use --strict-prod to enforce production-style validation.", registryEndpoint))
	}
	return warnings
}

func isDevRegistryURL(raw string) bool {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") {
		return true
	}

	host := trimmed
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
	}
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if idx := strings.LastIndex(host, ":"); idx >= 0 && strings.Count(host, ":") == 1 {
		host = host[:idx]
	}

	host = strings.ToLower(strings.Trim(host, "[]"))
	switch host {
	case "", "localhost", "registry.local":
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".svc.cluster.local") {
		return true
	}
	return net.ParseIP(host) != nil
}
