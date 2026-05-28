package registrycompat

import (
	"strings"

	"mcp-runtime/internal/cli/cluster/doctor"
	"mcp-runtime/internal/cli/core"
)

const (
	// K3sOverlayPath is the kustomize root for k3s registry NetworkPolicy compatibility.
	K3sOverlayPath = "config/registry/overlays/compatibility/k3s"
)

// OverlayPath returns a registry compatibility overlay kustomize path when the live cluster
// needs distribution-specific NetworkPolicy rules. Returns empty when base manifests suffice.
func OverlayPath(kubectl core.KubectlRunner) string {
	if kubectl == nil {
		return ""
	}
	switch doctor.DetectDistribution(kubectl) {
	case doctor.DistroK3s:
		return K3sOverlayPath
	}
	if traefikRunsInKubeSystem(kubectl) {
		return K3sOverlayPath
	}
	return ""
}

func traefikRunsInKubeSystem(kubectl core.KubectlRunner) bool {
	cmd, err := kubectl.CommandArgs([]string{
		"get", "deploy", "-n", "kube-system",
		"-l", "app.kubernetes.io/name=traefik",
		"-o", "jsonpath={.items[0].metadata.name}",
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
