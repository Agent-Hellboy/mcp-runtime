package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/pkg/metadata"
)

// filepathGlob is a test seam for filepath.Glob.
var filepathGlob = filepath.Glob

// PipelineManager handles pipeline operations with injected dependencies.
type PipelineManager struct {
	kubectl *KubectlClient
	logger  *zap.Logger
}

// NewPipelineManager creates a PipelineManager with the given dependencies.
func NewPipelineManager(kubectl *KubectlClient, logger *zap.Logger) *PipelineManager {
	return &PipelineManager{
		kubectl: kubectl,
		logger:  logger,
	}
}

// DefaultPipelineManager returns a PipelineManager using default clients.
func DefaultPipelineManager(logger *zap.Logger) *PipelineManager {
	return NewPipelineManager(kubectlClient, logger)
}

// NewPipelineCmd returns the pipeline subcommand for generate/deploy flows.
func NewPipelineCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultPipelineManager(logger)
	return NewPipelineCmdWithManager(mgr)
}

// NewPipelineCmdWithManager returns the pipeline subcommand using the provided manager.
func NewPipelineCmdWithManager(mgr *PipelineManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline integration commands",
		Long:  "Commands for CI/CD pipeline integration to generate and deploy CRDs",
	}

	cmd.AddCommand(mgr.newPipelineGenerateCmd())
	cmd.AddCommand(mgr.newPipelineDeployCmd())

	return cmd
}

func (m *PipelineManager) newPipelineGenerateCmd() *cobra.Command {
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
			return m.GenerateCRDsFromMetadata(metadataFile, metadataDir, outputDir)
		},
	}

	cmd.Flags().StringVar(&metadataFile, "file", "", "Path to metadata file (YAML)")
	cmd.Flags().StringVar(&metadataDir, "dir", ".mcp", "Directory containing metadata files")
	cmd.Flags().StringVar(&outputDir, "output", "manifests", "Output directory for CRD files")

	return cmd
}

func (m *PipelineManager) newPipelineDeployCmd() *cobra.Command {
	var manifestsDir string
	var namespace string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy CRD files to cluster",
		Long: `Deploy generated CRD files to the Kubernetes cluster.
This applies all CRD manifests to the cluster, which triggers
the operator to create the necessary Kubernetes resources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.DeployCRDs(manifestsDir, namespace)
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "dir", "manifests", "Directory containing CRD files")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to deploy to (overrides metadata)")

	return cmd
}

// GenerateCRDsFromMetadata generates CRD files from metadata.
func (m *PipelineManager) GenerateCRDsFromMetadata(metadataFile, metadataDir, outputDir string) error {
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
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	if len(registry.Servers) == 0 {
		return fmt.Errorf("no servers found in metadata")
	}

	m.logger.Info("Generating CRD files", zap.Int("count", len(registry.Servers)), zap.String("output", outputDir))

	if err := metadata.GenerateCRDsFromRegistry(registry, outputDir); err != nil {
		return fmt.Errorf("failed to generate CRDs: %w", err)
	}

	m.logger.Info("CRD files generated successfully", zap.String("output", outputDir))

	// List generated files
	files, _ := filepath.Glob(filepath.Join(outputDir, "*.yaml"))
	for _, file := range files {
		Success(fmt.Sprintf("Generated: %s", file))
	}

	return nil
}

// DeployCRDs deploys CRD files to the cluster.
func (m *PipelineManager) DeployCRDs(manifestsDir, namespace string) error {
	m.logger.Info("Deploying CRD files", zap.String("dir", manifestsDir))

	// Find all YAML files
	files, err := filepathGlob(filepath.Join(manifestsDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to list manifest files: %w", err)
	}

	ymlFiles, err := filepathGlob(filepath.Join(manifestsDir, "*.yml"))
	if err != nil {
		return fmt.Errorf("failed to list manifest files: %w", err)
	}

	files = append(files, ymlFiles...)

	if len(files) == 0 {
		return fmt.Errorf("no manifest files found in %s", manifestsDir)
	}

	// Apply each file
	for _, file := range files {
		m.logger.Info("Applying manifest", zap.String("file", file))

		args := []string{"apply", "-f", file}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}

		// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
		if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
			return fmt.Errorf("failed to apply %s: %w", file, err)
		}
	}

	m.logger.Info("All CRD files deployed successfully")
	return nil
}
