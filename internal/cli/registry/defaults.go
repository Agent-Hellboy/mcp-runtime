package registry

import (
	"net/url"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry/resolve"
	"mcp-runtime/pkg/authfile"
	"mcp-runtime/pkg/metadata"
)

const registryServiceDNSWithPort = "registry.registry.svc.cluster.local:5000"

// resolvePlatformRegistryURL resolves the registry host for user-facing image
// refs (build tags, push targets). The active platform login wins: a laptop
// user has no MCP_* env and no kubeconfig, so the saved profile is the only
// source that names the public registry. Env overrides and cluster discovery
// follow, then the bundled default.
func resolvePlatformRegistryURL(logger *zap.Logger) string {
	if host := SavedLoginRegistryHost(); host != "" {
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

// SavedLoginRegistryHost returns the registry host tied to the active platform
// login: the registry host saved by auth login, else one derived from the saved
// platform API URL (platform.<domain> -> registry.<domain>). It returns "" for
// logins against localhost or when no login is saved.
func SavedLoginRegistryHost() string {
	if host := strings.TrimSpace(authfile.CurrentRegistryHost()); host != "" {
		return strings.TrimSuffix(host, "/")
	}
	return platformRegistryHostFromSavedLogin()
}

// PublicImageRefForSavedLogin rewrites an image ref whose registry is a local
// placeholder (registry.local) or the bundled in-cluster registry Service DNS
// name onto the registry host of the active platform login. Neither host is
// pullable by a remote platform's nodes, so deploys through the platform API
// must name the registry the user pushed to. It returns false when the ref
// names another registry or no platform login registry is known (e.g. a Kind
// test-mode login against localhost), leaving the ref unchanged.
func PublicImageRefForSavedLogin(image string) (string, bool) {
	image = strings.TrimSpace(image)
	host, _, found := strings.Cut(image, "/")
	if !found || !isLocalOnlyRegistryHost(host) {
		return image, false
	}
	public := SavedLoginRegistryHost()
	if public == "" || strings.EqualFold(public, host) || isLocalOnlyRegistryHost(public) {
		return image, false
	}
	rewritten, ok := metadata.RewriteImageRegistryHost(image, public)
	if !ok {
		return image, false
	}
	return rewritten, true
}

func isLocalOnlyRegistryHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == metadata.DefaultRegistryHost {
		return true
	}
	name, _, _ := strings.Cut(host, ":")
	return name == "registry.registry.svc.cluster.local" || name == "registry.registry.svc"
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
