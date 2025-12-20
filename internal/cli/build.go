// Package cli provides CLI commands for the mcp-runtime.
//
// Example usage:
//
//	mcp-runtime server build image my-server --tag v1.0.0
//	mcp-runtime server build image my-server --dockerfile custom.Dockerfile --registry my-registry.com
package cli

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
		return fmt.Errorf("failed to build image for %s: %w", serverName, err)
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
		return fmt.Errorf("metadata file not found for server %s", serverName)
	}

	// Load and update
	registry, err := metadata.LoadFromFile(targetFile)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
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
		return fmt.Errorf("server %s not found in metadata", serverName)
	}

	// Write back
	data, err := yamlMarshal(registry)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(targetFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

func getPlatformRegistryURL(logger *zap.Logger) string {
	// Try to get from kubectl
	// #nosec G204 -- fixed arguments, no user input.
	ipCmd, ipErr := kubectlClient.CommandArgs([]string{"get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.clusterIP}"})
	var clusterIP []byte
	if ipErr == nil {
		clusterIP, ipErr = ipCmd.Output()
	}
	// #nosec G204 -- fixed arguments, no user input.
	portCmd, portErr := kubectlClient.CommandArgs([]string{"get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.ports[0].port}"})
	var port []byte
	if portErr == nil {
		port, portErr = portCmd.Output()
	}
	if ipErr == nil && len(clusterIP) > 0 && portErr == nil && len(port) > 0 {
		return fmt.Sprintf("%s:%s", strings.TrimSpace(string(clusterIP)), strings.TrimSpace(string(port)))
	}

	// Fallback to default
	logger.Warn("Could not detect platform registry, using default host:port")
	return fmt.Sprintf("registry.registry.svc.cluster.local:%d", GetRegistryPort())
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
