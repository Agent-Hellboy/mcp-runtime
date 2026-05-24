package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/metadata"
)

func newGenerateCmd(mgr *manager) *cobra.Command {
	var metadataFile string
	var metadataDir string
	var outputDir string
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CRD files from metadata",
		Long: `Generate Kubernetes CRD files from metadata/registry files.
This command reads server definitions and creates CRD YAML files that
the operator will use to deploy MCP servers.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.GenerateCRDsFromMetadata(metadataFile, metadataDir, outputDir)
		},
	}
	cmd.Flags().StringVar(&metadataFile, "file", "", "Path to metadata file (YAML)")
	cmd.Flags().StringVar(&metadataDir, "dir", ".mcp", "Directory containing metadata files")
	cmd.Flags().StringVar(&outputDir, "output", "manifests", "Output directory for CRD files")
	return cmd
}

func (m *manager) GenerateCRDsFromMetadata(metadataFile, metadataDir, outputDir string) error {
	var registry *metadata.RegistryFile
	var err error

	if metadataFile != "" {
		m.logger.Info("Loading metadata from file", zap.String("file", metadataFile))
		registry, err = metadata.LoadFromFile(metadataFile)
	} else {
		m.logger.Info("Loading metadata from directory", zap.String("dir", metadataDir))
		registry, err = metadata.LoadFromDirectory(metadataDir)
	}

	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrLoadMetadataFailed, err, fmt.Sprintf("failed to load metadata: %v", err))
		core.Error("Failed to load metadata")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to load metadata")
		return wrappedErr
	}

	if len(registry.Servers) == 0 {
		err := core.ErrNoServersInMetadata
		core.Error("No servers found in metadata")
		core.LogStructuredError(m.logger, err, "No servers found in metadata")
		return err
	}

	if metadata.ResolveRegistryHost() == metadata.DefaultRegistryHost {
		m.logger.Warn("Using default image host registry.local for generated MCPServer image refs. If cluster pulls fail, set MCP_REGISTRY_PULL_HOST or MCP_REGISTRY_ENDPOINT to the in-cluster registry Service (for example registry.registry.svc.cluster.local:5000) before pipeline generate, or configure containerd/k3s for your public registry host and imagePullSecrets.")
	}

	m.logger.Info("Generating CRD files", zap.Int("count", len(registry.Servers)), zap.String("output", outputDir))

	if err := metadata.GenerateCRDsFromRegistry(registry, outputDir); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrGenerateCRDsFailed,
			err,
			fmt.Sprintf("failed to generate CRDs: %v", err),
			map[string]any{"output_dir": outputDir, "server_count": len(registry.Servers), "component": "pipeline"},
		)
		core.Error("Failed to generate CRDs")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to generate CRDs")
		return wrappedErr
	}

	m.logger.Info("CRD files generated successfully", zap.String("output", outputDir))

	files, _ := filepath.Glob(filepath.Join(outputDir, "*.yaml"))
	for _, file := range files {
		core.Success(fmt.Sprintf("Generated: %s", file))
	}

	return nil
}
