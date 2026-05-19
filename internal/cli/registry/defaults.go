package registry

import (
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/registry/resolve"
)

func resolvePlatformRegistryURL(logger *zap.Logger) string {
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
