package doctor

import (
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
)

const doctorMCPAuthDeployment = "mcp-auth-server"

// checkMCPAuthDeployment reports the optional authorization server separately
// from Sentinel. An install without the opt-in deployment is healthy and is
// explicitly skipped; an enabled deployment must have its rollout ready.
func checkMCPAuthDeployment(kubectl core.KubectlRunner) DoctorCheck {
	if _, err := readKubectlOutput(kubectl, []string{"get", "deployment", doctorMCPAuthDeployment, "-n", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{Name: "mcp-auth deployment", OK: true, Detail: "optional mcp-auth authorization server is not installed; skipping"}
	}
	ready, err := readKubectlOutput(kubectl, []string{"get", "deployment", doctorMCPAuthDeployment, "-n", doctorSentinelNamespace, "-o", "jsonpath={.status.readyReplicas}"})
	if err != nil || strings.TrimSpace(ready) != "1" {
		return DoctorCheck{
			Name:   "mcp-auth deployment",
			OK:     false,
			Detail: fmt.Sprintf("deployment %s is not ready (readyReplicas=%q)", doctorMCPAuthDeployment, strings.TrimSpace(ready)),
			Remedy: "kubectl rollout status deployment/mcp-auth-server -n mcp-sentinel and inspect its pod events/logs",
		}
	}
	return DoctorCheck{Name: "mcp-auth deployment", OK: true, Detail: "optional authorization server deployment is ready"}
}

func checkMCPAuthSecrets(kubectl core.KubectlRunner) DoctorCheck {
	if _, err := readKubectlOutput(kubectl, []string{"get", "deployment", doctorMCPAuthDeployment, "-n", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{Name: "mcp-auth secrets", OK: true, Detail: "optional mcp-auth authorization server is not installed; skipping"}
	}
	for _, secret := range []struct {
		name string
		key  string
	}{
		{name: "mcp-auth-signing-key", key: "private-key.pem"},
		{name: "mcp-auth-server-tls", key: "tls.crt"},
		{name: "mcp-auth-server-tls", key: "tls.key"},
	} {
		path := "{.data." + strings.ReplaceAll(secret.key, ".", `\.`) + "}"
		value, err := readKubectlOutput(kubectl, []string{"get", "secret", secret.name, "-n", doctorSentinelNamespace, "-o", "jsonpath=" + path})
		if err != nil || strings.TrimSpace(value) == "" {
			return DoctorCheck{
				Name:   "mcp-auth secrets",
				OK:     false,
				Detail: fmt.Sprintf("Secret %s is missing non-empty key %s", secret.name, secret.key),
				Remedy: "create the mcp-auth signing-key Secret and cert-manager-managed mcp-auth-server-tls Secret before enabling the authorization server",
			}
		}
	}
	return DoctorCheck{Name: "mcp-auth secrets", OK: true, Detail: "signing key and TLS Secret contain the required keys"}
}
