package server

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func newBuildImageCmd(logger *zap.Logger) *cobra.Command {
	var dockerfile string
	var metadataFile string
	var metadataDir string
	var registryURL string
	var tag string
	var contextDir string

	cmd := &cobra.Command{
		Use:   "image <server-name>",
		Short: "Build Docker image for an MCP server",
		Long:  "Build a Docker image from Dockerfile and update metadata file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return BuildImage(logger, args[0], dockerfile, metadataFile, metadataDir, registryURL, tag, contextDir)
		},
	}

	cmd.Flags().StringVar(&dockerfile, "dockerfile", "Dockerfile", "Path to Dockerfile")
	cmd.Flags().StringVar(&metadataFile, "metadata-file", "", "Path to metadata file")
	cmd.Flags().StringVar(&metadataDir, "metadata-dir", ".mcp", "Directory containing metadata files")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Registry URL (defaults to platform registry)")
	cmd.Flags().StringVar(&tag, "tag", "", "Image tag (defaults to git SHA or 'latest')")
	cmd.Flags().StringVar(&contextDir, "context", ".", "Build context directory")

	return cmd
}
