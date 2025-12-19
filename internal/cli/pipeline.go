package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/pkg/metadata"
)

// NewPipelineCmd returns the pipeline subcommand for generate/deploy flows.
func NewPipelineCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline integration commands",
		Long:  "Commands for CI/CD pipeline integration to generate and deploy CRDs",
	}

	cmd.AddCommand(newPipelineGenerateCmd(logger))
	cmd.AddCommand(newPipelineDeployCmd(logger))

	return cmd
}

func newPipelineGenerateCmd(logger *zap.Logger) *cobra.Command {
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
			return generateCRDsFromMetadata(logger, metadataFile, metadataDir, outputDir)
		},
	}

	cmd.Flags().StringVar(&metadataFile, "file", "", "Path to metadata file (YAML)")
	cmd.Flags().StringVar(&metadataDir, "dir", ".mcp", "Directory containing metadata files")
	cmd.Flags().StringVar(&outputDir, "output", "manifests", "Output directory for CRD files")

	return cmd
}

func newPipelineDeployCmd(logger *zap.Logger) *cobra.Command {
	var manifestsDir string
	var namespace string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy CRD files to cluster",
		Long: `Deploy generated CRD files to the Kubernetes cluster.
This applies all CRD manifests to the cluster, which triggers
the operator to create the necessary Kubernetes resources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return deployCRDs(logger, manifestsDir, namespace)
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "dir", "manifests", "Directory containing CRD files")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to deploy to (overrides metadata)")

	return cmd
}

func generateCRDsFromMetadata(logger *zap.Logger, metadataFile, metadataDir, outputDir string) error {
	var registry *metadata.RegistryFile
	var err error

	if metadataFile != "" {
		logger.Info("Loading metadata from file", zap.String("file", metadataFile))
		registry, err = metadata.LoadFromFile(metadataFile)
	} else {
		logger.Info("Loading metadata from directory", zap.String("dir", metadataDir))
		registry, err = metadata.LoadFromDirectory(metadataDir)
	}

	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	if len(registry.Servers) == 0 {
		return fmt.Errorf("no servers found in metadata")
	}

	logger.Info("Generating CRD files", zap.Int("count", len(registry.Servers)), zap.String("output", outputDir))

	if err := metadata.GenerateCRDsFromRegistry(registry, outputDir); err != nil {
		return fmt.Errorf("failed to generate CRDs: %w", err)
	}

	logger.Info("CRD files generated successfully", zap.String("output", outputDir))

	// List generated files
	files, _ := filepath.Glob(filepath.Join(outputDir, "*.yaml"))
	for _, file := range files {
		Success(fmt.Sprintf("Generated: %s", file))
	}

	return nil
}

func deployCRDs(logger *zap.Logger, manifestsDir, namespace string) error {
	logger.Info("Deploying CRD files", zap.String("dir", manifestsDir))

	// Find all YAML files
	files, err := filepath.Glob(filepath.Join(manifestsDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to list manifest files: %w", err)
	}

	ymlFiles, err := filepath.Glob(filepath.Join(manifestsDir, "*.yml"))
	if err != nil {
		return fmt.Errorf("failed to list manifest files: %w", err)
	}

	files = append(files, ymlFiles...)

	if len(files) == 0 {
		return fmt.Errorf("no manifest files found in %s", manifestsDir)
	}

	// Apply each file
	for _, file := range files {
		logger.Info("Applying manifest", zap.String("file", file))

		args := []string{"apply", "-f", file}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}

		cmd := exec.Command("kubectl", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to apply %s: %w", file, err)
		}
	}

	logger.Info("All CRD files deployed successfully")
	return nil
}
