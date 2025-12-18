package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/pkg/metadata"

	"gopkg.in/yaml.v3"
)

func newBuildImageCmd(logger *zap.Logger) *cobra.Command {
	var dockerfile string
	var metadataFile string
	var metadataDir string
	var registryURL string
	var tag string
	var context string

	cmd := &cobra.Command{
		Use:   "image [server-name]",
		Short: "Build Docker image for an MCP server",
		Long: `Build a Docker image from Dockerfile and update metadata file.
If server-name is provided, builds that specific server.
Otherwise, builds all servers found in metadata files.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName := ""
			if len(args) > 0 {
				serverName = args[0]
			}
			return buildImage(logger, serverName, dockerfile, metadataFile, metadataDir, registryURL, tag, context)
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
	// Load metadata
	var registry *metadata.RegistryFile
	var err error

	if metadataFile != "" {
		registry, err = metadata.LoadFromFile(metadataFile)
	} else {
		registry, err = metadata.LoadFromDirectory(metadataDir)
	}

	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Get registry URL
	if registryURL == "" {
		registryURL = getPlatformRegistryURL(logger)
	}

	// Get tag
	if tag == "" {
		tag = getGitTag()
	}

	// Filter servers if serverName provided
	servers := registry.Servers
	if serverName != "" {
		found := false
		for _, s := range servers {
			if s.Name == serverName {
				servers = []metadata.ServerMetadata{s}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("server '%s' not found in metadata", serverName)
		}
	}

	// Build images
	for _, server := range servers {
		logger.Info("Building image", zap.String("server", server.Name))

		// Determine image name
		imageName := fmt.Sprintf("%s/%s", registryURL, server.Name)
		fullImage := fmt.Sprintf("%s:%s", imageName, tag)

		// Build Docker image
		buildCmd := exec.Command("docker", "build",
			"-f", dockerfile,
			"-t", fullImage,
			context,
		)
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr

		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("failed to build image for %s: %w", server.Name, err)
		}

		logger.Info("Image built successfully", zap.String("image", fullImage))

		// Update metadata file
		if err := updateMetadataImage(server.Name, imageName, tag, metadataFile, metadataDir); err != nil {
			logger.Warn("Failed to update metadata", zap.Error(err))
		}
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
	data, err := yaml.Marshal(registry)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(targetFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

func getPlatformRegistryURL(logger *zap.Logger) string {
	// Try to get from kubectl
	ipCmd := exec.Command("kubectl", "get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.clusterIP}")
	clusterIP, ipErr := ipCmd.Output()
	portCmd := exec.Command("kubectl", "get", "service", "registry", "-n", "registry", "-o", "jsonpath={.spec.ports[0].port}")
	port, portErr := portCmd.Output()
	if ipErr == nil && len(clusterIP) > 0 && portErr == nil && len(port) > 0 {
		return fmt.Sprintf("%s:%s", strings.TrimSpace(string(clusterIP)), strings.TrimSpace(string(port)))
	}

	// Fallback to default
	logger.Warn("Could not detect platform registry, using default host:port")
	return fmt.Sprintf("registry.registry.svc.cluster.local:%d", getRegistryPort())
}

func getGitTag() string {
	// Try to get git SHA
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	sha, err := cmd.Output()
	if err == nil && len(sha) > 0 {
		return strings.TrimSpace(string(sha))
	}

	// Fallback to latest
	return "latest"
}
