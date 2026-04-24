package cli

// This file implements the "server build" command for building Docker images.
// It handles Docker image building, metadata file updates, and registry integration.
//
// Example usage:
//   mcp-runtime server build image my-server --tag v1.0.0
//   mcp-runtime server build image my-server --dockerfile custom.Dockerfile --registry my-registry.com

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/pkg/metadata"

	"gopkg.in/yaml.v3"
)

// yamlMarshal is a test seam for yaml.Marshal.
var yamlMarshal = yaml.Marshal

func newBuildImageCmd(logger *zap.Logger) *cobra.Command {
	var dockerfile string
	var metadataFile string
	var metadataDir string
	var registryURL string
	var tag string
	var context string

	cmd := &cobra.Command{
		Use:   "image <server-name>",
		Short: "Build Docker image for an MCP server",
		Long:  `Build a Docker image from Dockerfile and update metadata file.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return buildImage(logger, args[0], dockerfile, metadataFile, metadataDir, registryURL, tag, context)
		},
	}

	cmd.Flags().StringVar(&dockerfile, "dockerfile", "Dockerfile", "Path to Dockerfile")
	cmd.Flags().StringVar(&metadataFile, "metadata-file", "", "Path to metadata file")
	cmd.Flags().StringVar(&metadataDir, "metadata-dir", ".mcp", "Directory containing metadata files")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Registry URL (defaults to platform registry)")
	cmd.Flags().StringVar(&tag, "tag", "", "Image tag (defaults to git SHA or 'latest')")
	cmd.Flags().StringVar(&context, "context", ".", "Build context directory")

	return cmd
}

func buildImage(logger *zap.Logger, serverName, dockerfile, metadataFile, metadataDir, registryURL, tag, context string) error {
	// Get registry URL
	if registryURL == "" {
		registryURL = getPlatformRegistryURL(logger)
	}

	// Get tag
	if tag == "" {
		tag = getGitTag()
	}

	logger.Info("Building image", zap.String("server", serverName))

	// Determine image name
	imageName := fmt.Sprintf("%s/%s", registryURL, serverName)
	fullImage := fmt.Sprintf("%s:%s", imageName, tag)

	// Build Docker image
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	buildCmd, err := execCommandWithValidators("docker", []string{
		"build",
		"-f", dockerfile,
		"-t", fullImage,
		context,
	})
	if err != nil {
		return err
	}
	buildCmd.SetStdout(os.Stdout)
	buildCmd.SetStderr(os.Stderr)

	if err := buildCmd.Run(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrBuildImageFailed,
			err,
			fmt.Sprintf("failed to build image for %s: %v", serverName, err),
			map[string]any{"server": serverName, "image": fullImage, "dockerfile": dockerfile, "component": "build"},
		)
		Error("Failed to build image")
		logStructuredError(logger, wrappedErr, "Failed to build image")
		return wrappedErr
	}

	logger.Info("Image built successfully", zap.String("image", fullImage))

	// Update metadata file
	if err := updateMetadataImage(serverName, imageName, tag, metadataFile, metadataDir); err != nil {
		logger.Warn("Failed to update metadata", zap.Error(err))
	}

	return nil
}

func updateMetadataImage(serverName, imageName, tag, metadataFile, metadataDir string) error {
	// Find the metadata file containing this server
	var targetFile string

	if metadataFile != "" {
		targetFile = metadataFile
	} else {
		// Search in metadata directory
		files, _ := filepath.Glob(filepath.Join(metadataDir, "*.yaml"))
		ymlFiles, _ := filepath.Glob(filepath.Join(metadataDir, "*.yml"))
		files = append(files, ymlFiles...)

		for _, file := range files {
			registry, err := metadata.LoadFromFile(file)
			if err != nil {
				continue
			}
			for _, s := range registry.Servers {
				if s.Name == serverName {
					targetFile = file
					break
				}
			}
			if targetFile != "" {
				break
			}
		}
	}

	if targetFile == "" {
		err := newWithSentinel(ErrMetadataFileNotFound, fmt.Sprintf("metadata file not found for server %s", serverName))
		Error("Metadata file not found")
		// Note: No logger available in this helper function
		return err
	}

	// Load and update
	registry, err := metadata.LoadFromFile(targetFile)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrLoadMetadataFailed, err, fmt.Sprintf("failed to load metadata: %v", err))
		Error("Failed to load metadata")
		// Note: No logger available in this helper function
		return wrappedErr
	}

	// Update server image
	updated := false
	for i := range registry.Servers {
		if registry.Servers[i].Name == serverName {
			registry.Servers[i].Image = imageName
			registry.Servers[i].ImageTag = tag
			updated = true
			break
		}
	}

	if !updated {
		err := newWithSentinel(ErrServerNotFoundInMetadata, fmt.Sprintf("server %s not found in metadata", serverName))
		Error("Server not found in metadata")
		// Note: No logger available in this helper function
		return err
	}

	// Write back
	data, err := yamlMarshal(registry)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrMarshalMetadataFailed, err, fmt.Sprintf("failed to marshal metadata: %v", err))
		Error("Failed to marshal metadata")
		// Note: No logger available in this helper function
		return wrappedErr
	}

	fileMode := os.FileMode(0o600)
	if info, statErr := os.Stat(targetFile); statErr == nil {
		fileMode = info.Mode().Perm()
		if fileMode&0o200 == 0 {
			writeErr := fmt.Errorf("file is not writable: %s", targetFile)
			wrappedErr := wrapWithSentinel(ErrWriteMetadataFailed, writeErr, fmt.Sprintf("failed to write metadata: %v", writeErr))
			Error("Failed to write metadata")
			// Note: No logger available in this helper function
			return wrappedErr
		}
	}

	if err := os.WriteFile(targetFile, data, fileMode); err != nil {
		wrappedErr := wrapWithSentinel(ErrWriteMetadataFailed, err, fmt.Sprintf("failed to write metadata: %v", err))
		Error("Failed to write metadata")
		// Note: No logger available in this helper function
		return wrappedErr
	}

	return nil
}

func getPlatformRegistryURL(logger *zap.Logger) string {
	const registryServiceDNS = "registry.registry.svc.cluster.local"

	// Respect an explicitly configured endpoint. For the implicit local default
	// (registry.local), prefer service-discovered endpoints that are known to work
	// in kind-based CI/e2e flows.
	if endpoint := strings.TrimSpace(GetRegistryEndpoint()); endpoint != "" &&
		(endpoint != defaultRegistryEndpoint || registryEndpointExplicitlyConfigured()) {
		return endpoint
	}

	// Otherwise read registry service IP/port and use the concrete service endpoint.
	// This is resolvable from kind/containerd nodes where cluster DNS names may not be.
	// #nosec G204 -- fixed arguments, no user input.
	ipCmd, ipErr := kubectlClient.CommandArgs([]string{"get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.clusterIP}"})
	var clusterIP []byte
	if ipErr == nil {
		clusterIP, ipErr = ipCmd.Output()
	}

	ip := strings.TrimSpace(string(clusterIP))
	// #nosec G204 -- fixed arguments, no user input.
	portCmd, portErr := kubectlClient.CommandArgs([]string{"get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.ports[0].port}"})
	var port []byte
	if portErr == nil {
		port, portErr = portCmd.Output()
	}
	portValue := strings.TrimSpace(string(port))
	if ipErr == nil && ip != "" && portErr == nil && portValue != "" {
		return fmt.Sprintf("%s:%s", ip, portValue)
	}
	if portErr == nil && portValue != "" {
		return fmt.Sprintf("%s:%s", registryServiceDNS, portValue)
	}

	// Fallback to default
	if logger != nil {
		logger.Warn("Could not detect registry ingress host or service port, using default service DNS:port")
	}
	return fmt.Sprintf("%s:%d", registryServiceDNS, GetRegistryPort())
}

func registryEndpointExplicitlyConfigured() bool {
	if value, ok := os.LookupEnv("MCP_REGISTRY_ENDPOINT"); ok && strings.TrimSpace(value) != "" {
		return true
	}
	if value, ok := os.LookupEnv("MCP_REGISTRY_HOST"); ok && strings.TrimSpace(value) != "" {
		return true
	}
	return false
}

func getGitTag() string {
	// Try to get git SHA
	// #nosec G204 -- fixed arguments, no user input.
	cmd, err := execCommandWithValidators("git", []string{"rev-parse", "--short", "HEAD"})
	if err == nil {
		sha, execErr := cmd.Output()
		if execErr == nil && len(sha) > 0 {
			return strings.TrimSpace(string(sha))
		}
	}

	// Fallback to latest
	return "latest"
}
