package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/setup/assetpath"
)

func ensureRepoManagedTraefikMiddlewareResources(kubectl core.KubectlRunner, logger *zap.Logger) error {
	namespaces, err := activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl)
	if err != nil {
		return err
	}
	for _, namespace := range namespaces {
		if logger != nil {
			logger.Info("Reconciling Traefik file-provider resources", zap.String("namespace", namespace))
		}
		if err := applyTraefikSupportManifest(kubectl, "config/ingress/overlays/http/dynamic-config.yaml", namespace); err != nil {
			return err
		}
		if err := applyTraefikSupportManifest(kubectl, "config/ingress/overlays/http/plugin-source.yaml", namespace); err != nil {
			return err
		}
		if err := patchTraefikDeploymentForFileMiddlewareSupport(kubectl, namespace); err != nil {
			return err
		}
	}
	return nil
}

func activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl core.KubectlRunner) ([]string, error) {
	cmd, err := kubectl.CommandArgs([]string{
		"get", "deployment", "-A", "--no-headers",
		"-o", "custom-columns=NS:.metadata.namespace,NAME:.metadata.name",
	})
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list traefik deployments: %w", err)
	}
	seen := map[string]struct{}{}
	var namespaces []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "traefik" {
			continue
		}
		ns := strings.TrimSpace(fields[0])
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		namespaces = append(namespaces, ns)
	}
	slices.Sort(namespaces)
	return namespaces, nil
}

func applyTraefikSupportManifest(kubectl core.KubectlRunner, relPath, namespace string) error {
	resolvedPath, err := assetpath.ResolveRepoAssetPath(relPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadIngressManifestFailed, err, fmt.Sprintf("failed to resolve Traefik manifest %s: %v", relPath, err))
	}
	manifestBytes, err := kube.ReadFileAtPath(resolvedPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadIngressManifestFailed, err, fmt.Sprintf("failed to read Traefik manifest %s: %v", relPath, err))
	}
	manifestContent := strings.ReplaceAll(string(manifestBytes), "namespace: traefik", "namespace: "+namespace)
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, manifestContent); err != nil {
		return core.WrapWithSentinel(
			core.ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to reconcile Traefik manifest %s in namespace %s: %v", relPath, namespace, err),
		)
	}
	return nil
}

func patchTraefikDeploymentForFileMiddlewareSupport(kubectl core.KubectlRunner, namespace string) error {
	spec, err := readTraefikDeploymentSpec(kubectl, namespace)
	if err != nil {
		return err
	}
	if len(spec.Spec.Template.Spec.Containers) == 0 {
		return core.NewWithSentinel(core.ErrInstallIngressControllerFailed, fmt.Sprintf("traefik deployment in namespace %s has no containers", namespace))
	}
	containerIndex := -1
	for i, candidate := range spec.Spec.Template.Spec.Containers {
		if candidate.Name == "traefik" {
			containerIndex = i
			break
		}
	}
	if containerIndex == -1 {
		return core.NewWithSentinel(core.ErrInstallIngressControllerFailed, fmt.Sprintf("traefik deployment in namespace %s has no container named traefik", namespace))
	}
	container := spec.Spec.Template.Spec.Containers[containerIndex]

	var ops []jsonPatchOperation
	if !containsString(container.Args, "--providers.file.filename=/etc/traefik/dynamic/dynamic.yml") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--providers.file.filename=/etc/traefik/dynamic/dynamic.yml",
		})
	}
	if !containsString(container.Args, "--providers.file.watch=true") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--providers.file.watch=true",
		})
	}
	if !containsString(container.Args, "--experimental.localplugins.pii-redactor.modulename=github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/args/-", containerIndex),
			Value: "--experimental.localplugins.pii-redactor.modulename=github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor",
		})
	}
	addDynamicMount := !hasVolumeMountPath(container.VolumeMounts, "/etc/traefik/dynamic")
	if addDynamicMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-dynamic", "mountPath": "/etc/traefik/dynamic", "readOnly": true},
		})
	}
	addPluginSourceMount := !hasVolumeMountPath(container.VolumeMounts, "/plugins-local/src/github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor")
	if addPluginSourceMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-plugin-source", "mountPath": "/plugins-local/src/github.com/Agent-Hellboy/mcp-runtime/traefik-plugins/pii-redactor", "readOnly": true},
		})
	}
	addPluginStorageMount := !hasVolumeMountPath(container.VolumeMounts, "/plugins-storage")
	if addPluginStorageMount {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/volumeMounts/-", containerIndex),
			Value: map[string]any{"name": "traefik-plugins", "mountPath": "/plugins-storage"},
		})
	}
	if addDynamicMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-dynamic") {
		ops = append(ops, jsonPatchOperation{
			Op:   "add",
			Path: "/spec/template/spec/volumes/-",
			Value: map[string]any{
				"name": "traefik-dynamic",
				"configMap": map[string]any{
					"name":  "traefik-dynamic",
					"items": []map[string]any{{"key": "dynamic.yml", "path": "dynamic.yml"}},
				},
			},
		})
	}
	if addPluginSourceMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-plugin-source") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  "/spec/template/spec/volumes/-",
			Value: map[string]any{"name": "traefik-plugin-source", "configMap": map[string]any{"name": "traefik-plugin-pii-redactor"}},
		})
	}
	if addPluginStorageMount && !hasVolume(spec.Spec.Template.Spec.Volumes, "traefik-plugins") {
		ops = append(ops, jsonPatchOperation{
			Op:    "add",
			Path:  "/spec/template/spec/volumes/-",
			Value: map[string]any{"name": "traefik-plugins", "emptyDir": map[string]any{}},
		})
	}
	if len(ops) == 0 {
		return nil
	}
	patchBytes, err := json.Marshal(ops)
	if err != nil {
		return fmt.Errorf("marshal traefik deployment patch: %w", err)
	}
	if err := kubectl.RunWithOutput([]string{
		"patch", "deployment", "traefik", "-n", namespace, "--type=json", "-p", string(patchBytes),
	}, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinel(
			core.ErrInstallIngressControllerFailed,
			err,
			fmt.Sprintf("failed to patch traefik deployment in namespace %s for file-provider middleware support: %v", namespace, err),
		)
	}
	return nil
}

func readTraefikDeploymentSpec(kubectl core.KubectlRunner, namespace string) (traefikDeploymentSpec, error) {
	var spec traefikDeploymentSpec
	cmd, err := kubectl.CommandArgs([]string{"get", "deployment", "traefik", "-n", namespace, "-o", "json"})
	if err != nil {
		return spec, err
	}
	out, err := cmd.Output()
	if err != nil {
		return spec, fmt.Errorf("read traefik deployment %s/traefik: %w", namespace, err)
	}
	if err := json.Unmarshal(out, &spec); err != nil {
		return spec, fmt.Errorf("decode traefik deployment %s/traefik: %w", namespace, err)
	}
	return spec, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hasVolumeMountPath(mounts []struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}, mountPath string) bool {
	for _, mount := range mounts {
		if mount.MountPath == mountPath {
			return true
		}
	}
	return false
}

func hasVolume(volumes []struct {
	Name string `json:"name"`
}, name string) bool {
	for _, volume := range volumes {
		if volume.Name == name {
			return true
		}
	}
	return false
}

func activeTraefikNamespaceForPlatform(kubectl core.KubectlRunner) string {
	namespaces, err := activeNamedTraefikDeploymentNamespacesWithKubectl(kubectl)
	if err != nil || len(namespaces) == 0 {
		return ""
	}
	for _, preferred := range []string{"traefik", "kube-system"} {
		if slices.Contains(namespaces, preferred) {
			return preferred
		}
	}
	return namespaces[0]
}
