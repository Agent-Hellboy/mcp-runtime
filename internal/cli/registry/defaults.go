package registry

import (
	"net/url"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry/resolve"
	"mcp-runtime/pkg/authfile"
)

const registryServiceDNSWithPort = "registry.registry.svc.cluster.local:5000"

func resolvePlatformRegistryURL(logger *zap.Logger) string {
	if host := strings.TrimSpace(authfile.CurrentRegistryHost()); host != "" {
		return strings.TrimSuffix(host, "/")
	}
	if host := platformRegistryHostFromSavedLogin(); host != "" {
		return host
	}
	return resolve.PlatformURL(logger, func(args []string) (resolve.OutputCommand, error) {
		return core.DefaultKubectlClient().CommandArgs(args)
	}, registryResolveConfig())
}

func ResolvePlatformRegistryURL(logger *zap.Logger) string {
	return resolvePlatformRegistryURL(logger)
}

func resolveInternalPlatformRegistryURL(logger *zap.Logger) string {
	return resolve.InternalPlatformURL(logger, func(args []string) (resolve.OutputCommand, error) {
		return core.DefaultKubectlClient().CommandArgs(args)
	}, registryResolveConfig())
}

func ResolveInternalPlatformRegistryURL(logger *zap.Logger) string {
	return resolveInternalPlatformRegistryURL(logger)
}

func resolveInClusterPushRegistryURL(logger *zap.Logger) string {
	target := strings.TrimSpace(resolveInternalPlatformRegistryURL(logger))
	registryEndpoint := ""
	if core.DefaultCLIConfig != nil {
		registryEndpoint = strings.TrimSpace(core.DefaultCLIConfig.RegistryEndpoint)
	}
	if strings.TrimSpace(authfile.CurrentRegistryHost()) == "" && registryEndpoint == "" {
		if _, port, found := strings.Cut(target, ":"); found && strings.TrimSpace(port) != "" {
			return "registry.registry.svc.cluster.local:" + strings.TrimSpace(port)
		}
		return registryServiceDNSWithPort
	}
	return target
}

func registryResolveConfig() resolve.Config {
	return resolve.Config{
		RegistryEndpoint:        core.DefaultCLIConfig.RegistryEndpoint,
		DefaultRegistryEndpoint: core.DefaultRegistryEndpoint,
		RegistryIngressHost:     core.DefaultCLIConfig.RegistryIngressHost,
		DefaultRegistryHost:     core.DefaultRegistryIngressHost,
		RegistryPort:            core.DefaultCLIConfig.RegistryPort,
	}
}

func defaultGitTag() string {
	return resolve.GitTag(func(name string, args []string) (resolve.OutputCommand, error) {
		return core.ExecCommandWithValidators(name, args)
	})
}

func DefaultGitTag() string {
	return defaultGitTag()
}

func platformRegistryHostFromSavedLogin() string {
	_, apiBaseURL, _, err := authfile.ResolveToken()
	if err != nil {
		return ""
	}
	return registryHostFromAPIBaseURL(apiBaseURL)
}

func registryHostFromAPIBaseURL(apiBaseURL string) string {
	apiBaseURL = strings.TrimSpace(apiBaseURL)
	if apiBaseURL == "" {
		return ""
	}
	u, err := url.Parse(apiBaseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" || host == "localhost" || host == "127.0.0.1" {
		return ""
	}
	if strings.HasPrefix(host, "platform.") {
		host = "registry." + strings.TrimPrefix(host, "platform.")
	}
	if port := strings.TrimSpace(u.Port()); port != "" {
		return host + ":" + port
	}
	return host
}
