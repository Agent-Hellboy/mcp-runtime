package platform

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/k8sclient"
)

const (
	defaultRegistryDeploymentImage  = "registry:2.8.3"
	registryImageOverrideEnvForPlan = "MCP_RUNTIME_REGISTRY_IMAGE_OVERRIDE"
)

func deployRegistryClientGo(logger *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	if err := k8sclient.EnsureNamespace(context.Background(), clients, namespace, nil); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrEnsureNamespaceFailed,
			err,
			fmt.Sprintf("failed to ensure namespace: %v", err),
			map[string]any{"namespace": namespace, "component": "registry"},
		)
		core.Error("Failed to ensure namespace")
		core.LogStructuredError(logger, wrappedErr, "Failed to ensure namespace")
		return wrappedErr
	}

	if logger != nil {
		logger.Info("Applying registry manifests")
	}
	manifest, err := renderRegistryKustomizeManifest(manifestPath)
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrDeployRegistryFailed,
			err,
			fmt.Sprintf("failed to render registry manifest %q: %v", manifestPath, err),
			map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
		)
		core.Error("Failed to render registry manifest")
		core.LogStructuredError(logger, wrappedErr, "Failed to render registry manifest")
		return wrappedErr
	}
	manifest = rewriteRegistryManifestHost(manifest, core.GetRegistryIngressHost())
	manifest = stripRegistryManifestClusterIssuerAnnotation(manifest)
	if overrideImage := strings.TrimSpace(os.Getenv(registryImageOverrideEnvForPlan)); overrideImage != "" {
		if logger != nil {
			logger.Info("Applying registry image override", zap.String("image", overrideImage))
		}
		updated := strings.Replace(manifest, "image: "+defaultRegistryDeploymentImage, "image: "+overrideImage, 1)
		if updated == manifest {
			err := fmt.Errorf("registry image reference %q not found in manifest", defaultRegistryDeploymentImage)
			wrappedErr := core.WrapWithSentinelAndContext(
				core.ErrDeployRegistryFailed,
				err,
				err.Error(),
				map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
			)
			core.Error("Failed to rewrite registry image")
			core.LogStructuredError(logger, wrappedErr, "Failed to rewrite registry image")
			return wrappedErr
		}
		manifest = updated
	}
	results, err := k8sclient.ApplyManifestYAML(context.Background(), clients, []byte(manifest), namespace)
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrDeployRegistryFailed,
			err,
			fmt.Sprintf("failed to deploy registry: %v", err),
			map[string]any{"namespace": namespace, "manifest_path": manifestPath, "registry_type": registryType, "component": "registry"},
		)
		core.Error("Failed to deploy registry")
		core.LogStructuredError(logger, wrappedErr, "Failed to deploy registry")
		return wrappedErr
	}
	writeApplyResults(os.Stdout, results)

	if err := ensureRegistryStorageSizeClientGo(logger, clients, namespace, registryStorageSize); err != nil {
		return err
	}

	if logger != nil {
		logger.Info("Waiting for registry to be ready")
	}
	if err := k8sclient.WaitForDeploymentAvailable(context.Background(), clients, namespace, "registry", 5*time.Minute); err != nil {
		if logger != nil {
			logger.Warn("Registry deployment may still be in progress", zap.Error(err))
		}
	}
	if logger != nil {
		logger.Info("Registry deployed successfully")
	}
	_ = port
	return nil
}

func renderRegistryKustomizeManifest(manifestPath string) (string, error) {
	kubectl := core.DefaultKubectlClient()
	renderCmd, err := kubectl.CommandArgs([]string{"kustomize", manifestPath})
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	renderCmd.SetStdout(&stdout)
	renderCmd.SetStderr(&stderr)
	if err := renderCmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl kustomize %s failed: %w (%s)", manifestPath, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func rewriteRegistryManifestHost(manifest, host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "registry.local" {
		return manifest
	}
	return strings.ReplaceAll(manifest, "registry.local", host)
}

func stripRegistryManifestClusterIssuerAnnotation(manifest string) string {
	lines := strings.SplitAfter(manifest, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cert-manager.io/cluster-issuer:") || strings.HasPrefix(trimmed, `"cert-manager.io/cluster-issuer":`) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func ensureRegistryStorageSizeClientGo(logger *zap.Logger, clients *k8sclient.Clients, namespace, registryStorageSize string) error {
	storageSize := strings.TrimSpace(registryStorageSize)
	if storageSize == "" {
		return nil
	}
	currentSize, err := k8sclient.PersistentVolumeClaimStorage(context.Background(), clients, namespace, core.RegistryPVCName)
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrReadRegistryStorageFailed,
			err,
			fmt.Sprintf("failed to read current registry storage size: %v", err),
			map[string]any{"namespace": namespace, "pvc": core.RegistryPVCName, "component": "registry"},
		)
		core.Error("Failed to read registry storage size")
		core.LogStructuredError(logger, wrappedErr, "Failed to read registry storage size")
		return wrappedErr
	}
	if currentSize == storageSize {
		if logger != nil {
			logger.Info("Registry storage size already matches requested value", zap.String("size", storageSize))
		}
		return nil
	}
	if logger != nil {
		logger.Info("Updating registry storage size", zap.String("from", currentSize), zap.String("to", storageSize))
	}
	if err := k8sclient.UpdatePersistentVolumeClaimStorage(context.Background(), clients, namespace, core.RegistryPVCName, storageSize); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrUpdateRegistryStorageFailed,
			err,
			fmt.Sprintf("failed to update registry storage size to %s: %v", storageSize, err),
			map[string]any{"namespace": namespace, "pvc": core.RegistryPVCName, "storage_size": storageSize, "component": "registry"},
		)
		core.Error("Failed to update registry storage size")
		core.LogStructuredError(logger, wrappedErr, "Failed to update registry storage size")
		return wrappedErr
	}
	return nil
}
