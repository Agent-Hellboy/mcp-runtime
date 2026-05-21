package kubeerr

import "strings"

// DirectModeGuidance explains the boundary for explicit --use-kube operations.
const DirectModeGuidance = "Direct Kubernetes mode requires admin/operator cluster access. Use the platform API for normal CLI operations: `mcp-runtime auth login --api-url <platform-url>`."

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

// DirectModeHint returns guidance for explicit --use-kube failures.
func DirectModeHint(detail string) string {
	lower := strings.ToLower(detail)

	switch {
	case strings.Contains(lower, "forbidden"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "user cannot"),
		strings.Contains(lower, "cannot list resource"),
		strings.Contains(lower, "cannot get resource"),
		strings.Contains(lower, "cannot create resource"),
		strings.Contains(lower, "cannot patch resource"),
		strings.Contains(lower, "cannot delete resource"):
		return "Direct Kubernetes mode requires admin/operator cluster access; the current kubeconfig is not authorized for this operation. Use the platform API for normal CLI operations: `mcp-runtime auth login --api-url <platform-url>`."
	case strings.Contains(lower, "kubeconfig"),
		strings.Contains(lower, "no configuration has been provided"):
		return "Direct Kubernetes mode requires admin/operator cluster access and a readable kubeconfig. Use the platform API for normal CLI operations: `mcp-runtime auth login --api-url <platform-url>`."
	default:
		return DirectModeGuidance
	}
}

// WithDirectModeHint appends explicit --use-kube guidance to a command failure detail.
func WithDirectModeHint(detail string) string {
	detail = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(detail), "."))
	if detail == "" {
		return DirectModeHint(detail)
	}
	return detail + ". " + DirectModeHint(detail)
}

// DirectModeFailureMessage appends shared direct Kubernetes mode guidance to a command failure.
func DirectModeFailureMessage(prefix, detail string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return WithDirectModeHint(detail)
	}
	return prefix + ": " + WithDirectModeHint(detail)
}
