package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/platformapi"
)

func newDeployCmd(mgr *manager) *cobra.Command {
	var manifestsDir string
	var namespace string
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy CRD files to cluster",
		Long: `Deploy generated CRD files to the Kubernetes cluster.
This applies all CRD manifests to the cluster, which triggers
the operator to create the necessary Kubernetes resources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.DeployCRDs(manifestsDir, namespace)
		},
	}
	cmd.Flags().StringVar(&manifestsDir, "dir", "manifests", "Directory containing CRD files")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to deploy to (overrides metadata)")
	return cmd
}

func (m *manager) DeployCRDs(manifestsDir, namespace string) error {
	if _, kerr := m.kubectl.CombinedOutput([]string{"version", "--request-timeout=5s"}); kerr != nil {
		if platformapi.HasPlatformClient() {
			return core.NewWithSentinel(core.ErrApplyManifestFailed, "pipeline deploy applies YAML with kubectl and needs a working kubeconfig. mcp-runtime auth is for the platform API only, not for applying manifests. Run deploy from a host with cluster access, or fix KUBECONFIG, then retry.")
		}
	}
	m.logger.Info("Deploying CRD files", zap.String("dir", manifestsDir))

	files, err := filepathGlob(filepath.Join(manifestsDir, "*.yaml"))
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrListManifestFilesFailed,
			err,
			fmt.Sprintf("failed to list manifest files: %v", err),
			map[string]any{"manifest_dir": manifestsDir, "component": "pipeline"},
		)
		core.Error("Failed to list manifest files")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to list manifest files")
		return wrappedErr
	}

	ymlFiles, err := filepathGlob(filepath.Join(manifestsDir, "*.yml"))
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrListManifestFilesFailed,
			err,
			fmt.Sprintf("failed to list manifest files: %v", err),
			map[string]any{"manifest_dir": manifestsDir, "component": "pipeline"},
		)
		core.Error("Failed to list manifest files")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to list manifest files")
		return wrappedErr
	}

	files = append(files, ymlFiles...)
	if len(files) == 0 {
		err := core.NewWithSentinel(core.ErrNoManifestFilesFound, fmt.Sprintf("no manifest files found in %s", manifestsDir))
		core.Error("No manifest files found")
		core.LogStructuredError(m.logger, err, "No manifest files found")
		return err
	}

	for _, file := range files {
		m.logger.Info("Applying manifest", zap.String("file", file))

		absPath, err := kube.ResolveRegularFilePath(file)
		if err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrApplyManifestFailed,
				err,
				fmt.Sprintf("failed to resolve %s: %v", file, err),
				map[string]any{"file": file, "namespace": namespace, "component": "pipeline"},
			)
			core.Error("Failed to resolve manifest file")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to resolve manifest file")
			return wrappedErr
		}

		manifestBytes, err := kube.ReadFileAtPath(absPath)
		if err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrApplyManifestFailed,
				err,
				fmt.Sprintf("failed to read %s: %v", absPath, err),
				map[string]any{"file": file, "namespace": namespace, "component": "pipeline"},
			)
			core.Error("Failed to read manifest file")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to read manifest file")
			return wrappedErr
		}

		manifestBytes, err = m.prepareManifestForDeploy(manifestBytes, namespace)
		if err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrApplyManifestFailed,
				err,
				fmt.Sprintf("failed to prepare %s: %v", absPath, err),
				map[string]any{"file": file, "namespace": namespace, "component": "pipeline"},
			)
			core.Error("Failed to prepare manifest")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to prepare manifest")
			return wrappedErr
		}

		if err := kube.ApplyManifestContentWithNamespace(m.kubectl.CommandArgs, string(manifestBytes), namespace); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrApplyManifestFailed,
				err,
				fmt.Sprintf("failed to apply %s: %v", file, err),
				map[string]any{"file": file, "namespace": namespace, "component": "pipeline"},
			)
			core.Error("Failed to apply manifest")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to apply manifest")
			return wrappedErr
		}
	}

	m.logger.Info("All CRD files deployed successfully")
	return nil
}
