package doctor

import (
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/sentinel"
)

func checkSessionLocalDeploymentScaling(kubectl core.KubectlRunner) DoctorCheck {
	const checkName = "sentinel session-local deployment scaling"
	if _, err := readKubectlOutput(kubectl, []string{"get", "namespace", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{
			Name:   checkName,
			OK:     true,
			Detail: fmt.Sprintf("namespace %s not installed; skipping session-local replica check", doctorSentinelNamespace),
		}
	}

	failures := make([]string, 0)
	for _, name := range sentinel.SessionLocalDeploymentNames {
		out, err := readKubectlOutput(kubectl, []string{
			"get", "deployment", name,
			"-n", doctorSentinelNamespace,
			"-o", "jsonpath={.spec.replicas}",
		})
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		replicas, err := parseDoctorReplicaCount(out)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s/%s: %v", doctorSentinelNamespace, name, err))
			continue
		}
		if replicas > sentinel.SessionLocalMaxReplicas {
			failures = append(failures, fmt.Sprintf("%s/%s has %d replicas; max %d until shared UI session storage exists",
				doctorSentinelNamespace, name, replicas, sentinel.SessionLocalMaxReplicas))
		}
	}

	if len(failures) > 0 {
		return DoctorCheck{
			Name:   checkName,
			OK:     false,
			Detail: strings.Join(failures, "; "),
			Remedy: "scale mcp-sentinel-ui and mcp-sentinel-gateway back to 1 replica, or implement shared session storage (see issue #257)",
		}
	}
	return DoctorCheck{
		Name:   checkName,
		OK:     true,
		Detail: fmt.Sprintf("%d session-local deployment(s) at or below %d replica", len(sentinel.SessionLocalDeploymentNames), sentinel.SessionLocalMaxReplicas),
	}
}

func parseDoctorReplicaCount(raw string) (int32, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("empty replica count")
	}
	var replicas int32
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid replica count %q", raw)
		}
		replicas = replicas*10 + int32(ch-'0')
	}
	return replicas, nil
}
