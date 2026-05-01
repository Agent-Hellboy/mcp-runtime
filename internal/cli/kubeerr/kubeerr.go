package kubeerr

import "strings"

// CommandDetail extracts a single-line error detail from kubectl output or the exec error.
func CommandDetail(output string, fallback error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	if fallback != nil {
		return fallback.Error()
	}
	return "Unknown error"
}

// SetupHint returns a friendlier message when the cluster has not been provisioned yet.
func SetupHint(detail string) (string, bool) {
	lower := strings.ToLower(detail)

	switch {
	case strings.Contains(lower, "executable file not found"),
		strings.Contains(lower, "kubectl: not found"):
		return "kubectl is missing. Install kubectl and re-run the command.", true
	case strings.Contains(lower, "kubeconfig"),
		strings.Contains(lower, "no configuration has been provided"):
		return "kubeconfig is missing or not readable. Either copy your cluster kubeconfig to ~/.kube/config, or re-run with `./bin/mcp-runtime setup --kubeconfig /etc/rancher/k3s/k3s.yaml` (for k3s) and optionally `--context <name>`.", true
	case strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "unable to connect to the server"),
		strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "the connection to the server"):
		return "no Kubernetes API reachable. Verify your kubeconfig/context (or pass `--kubeconfig`/`--context` to setup) and ensure the cluster control plane is reachable.", true
	default:
		return "", false
	}
}
