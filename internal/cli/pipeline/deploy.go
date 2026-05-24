package pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	sigsyaml "sigs.k8s.io/yaml"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
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
When platform API credentials are configured (mcp-runtime auth login), manifests
are applied through POST /api/runtime/servers without kubectl. Otherwise kubectl
apply is used and requires a working kubeconfig.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.DeployCRDs(manifestsDir, namespace)
		},
	}
	cmd.Flags().StringVar(&manifestsDir, "dir", "manifests", "Directory containing CRD files")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to deploy to (overrides metadata)")
	return cmd
}

func (m *manager) DeployCRDs(manifestsDir, namespace string) error {
	if platformapi.HasPlatformClient() {
		m.logger.Info("Deploying CRD files via platform API", zap.String("dir", manifestsDir))
		return m.deployCRDsViaPlatformAPI(manifestsDir, namespace)
	}

	_, kerr := m.kubectl.CombinedOutput([]string{"version", "--request-timeout=5s"})
	if kerr != nil {
		return core.NewWithSentinel(core.ErrApplyManifestFailed, "pipeline deploy applies YAML with kubectl and needs a working kubeconfig. mcp-runtime auth is for the platform API only, not for applying manifests. Run deploy from a host with cluster access, or fix KUBECONFIG, then retry.")
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

func (m *manager) deployCRDsViaPlatformAPI(manifestsDir, namespace string) error {
	files, err := filepathGlob(filepath.Join(manifestsDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("glob yaml: %w", err)
	}
	yml, err := filepathGlob(filepath.Join(manifestsDir, "*.yml"))
	if err != nil {
		return fmt.Errorf("glob yml: %w", err)
	}
	files = append(files, yml...)
	if len(files) == 0 {
		return core.NewWithSentinel(core.ErrNoManifestFilesFound, fmt.Sprintf("no manifest files found in %s", manifestsDir))
	}
	plat, err := platformapi.NewPlatformClient()
	if err != nil {
		return platformapi.AuthRequiredError(err)
	}
	for _, file := range files {
		absPath, err := kube.ResolveRegularFilePath(file)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", file, err)
		}
		b, err := kube.ReadFileAtPath(absPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", absPath, err)
		}
		var server mcpv1alpha1.MCPServer
		if err := sigsyaml.Unmarshal(b, &server); err != nil {
			return fmt.Errorf("parse %s: %w", file, err)
		}
		if server.Kind != "MCPServer" || server.Name == "" {
			continue
		}
		ns := firstNonEmpty(namespace, server.Namespace)
		scope := server.Labels["mcpruntime.org/scope"]
		if _, err := plat.ApplyRuntimeServerWithScope(context.Background(), server.Name, ns, scope, server.Spec); err != nil {
			return fmt.Errorf("apply %s: %w", server.Name, err)
		}
		m.logger.Info("Applied server via platform API", zap.String("name", server.Name), zap.String("namespace", ns))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
