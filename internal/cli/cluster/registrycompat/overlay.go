package registrycompat

import (
	"path/filepath"
	"strings"

	"mcp-runtime/internal/cli/cluster/doctor"
	"mcp-runtime/internal/cli/core"
)

const (
	// K3sOverlaySubPath is the kustomize subpath for k3s registry NetworkPolicy compatibility.
	K3sOverlaySubPath = "overlays/compatibility/k3s"
)

// OverlayPath returns a registry compatibility overlay subpath when the live cluster
// needs distribution-specific NetworkPolicy rules. Returns empty when base manifests suffice.
func OverlayPath(kubectl core.KubectlRunner) string {
	if kubectl == nil {
		return ""
	}
	switch doctor.DetectDistribution(kubectl) {
	case doctor.DistroK3s:
		return K3sOverlaySubPath
	}
	if traefikRunsInKubeSystem(kubectl) {
		return K3sOverlaySubPath
	}
	return ""
}

// ResolveOverlayPath resolves an overlay subpath from the active manifest root.
// Example: config/registry/overlays/tls + overlays/compatibility/k3s ->
// config/registry/overlays/compatibility/k3s.
func ResolveOverlayPath(manifestPath, overlaySubPath string) string {
	manifestPath = filepath.Clean(strings.TrimSpace(manifestPath))
	overlaySubPath = filepath.Clean(strings.TrimSpace(overlaySubPath))
	if overlaySubPath == "" || overlaySubPath == "." {
		return ""
	}
	if manifestPath == "" || manifestPath == "." {
		return filepath.Clean(filepath.Join("config/registry", overlaySubPath))
	}
	needle := string(filepath.Separator) + "overlays" + string(filepath.Separator)
	if i := strings.Index(manifestPath, needle); i >= 0 {
		return filepath.Clean(filepath.Join(manifestPath[:i], overlaySubPath))
	}
	if filepath.Base(manifestPath) == "base" {
		return filepath.Clean(filepath.Join(filepath.Dir(manifestPath), overlaySubPath))
	}
	return filepath.Clean(filepath.Join(manifestPath, overlaySubPath))
}

func traefikRunsInKubeSystem(kubectl core.KubectlRunner) bool {
	cmd, err := kubectl.CommandArgs([]string{
		"get", "deploy", "-n", "kube-system",
		"-l", "app.kubernetes.io/name=traefik",
		"-o", "jsonpath={.items[*].metadata.name}",
	})
	if err != nil {
		return false
	}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}
