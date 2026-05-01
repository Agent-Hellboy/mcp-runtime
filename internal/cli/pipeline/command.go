// Package pipeline owns routing for the pipeline top-level command.
package pipeline

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

// filepathGlob is a test seam for filepath.Glob.
var filepathGlob = filepath.Glob

type manager struct {
	kubectl *core.KubectlClient
	logger  *zap.Logger
}

func newManager(runtime *core.Runtime) *manager {
	return &manager{
		kubectl: runtime.KubectlClient(),
		logger:  runtime.Logger(),
	}
}

// New returns the pipeline command.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(newManager(runtime))
}

// NewWithManager returns the pipeline command using the provided manager.
func NewWithManager(mgr *manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline integration commands",
		Long:  "Commands for CI/CD pipeline integration to generate and deploy CRDs",
	}
	cmd.AddCommand(newGenerateCmd(mgr), newDeployCmd(mgr))
	return cmd
}
