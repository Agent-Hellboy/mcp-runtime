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
	checks := []struct {
		name string
		key  string
	}{}
	// Test mode deliberately uses an ephemeral signing key and HTTP ingress.
	// Production manifests expose the mounted key path and TLS ingress, so use
	// those rendered resources to discover custom Secret names instead of
	// assuming the setup defaults.
	signingKeySecret, _ := readKubectlOutput(kubectl, []string{"get", "deployment", doctorMCPAuthDeployment, "-n", doctorSentinelNamespace, "-o", "jsonpath={.spec.template.spec.volumes[?(@.name==\"signing-key\")].secret.secretName}"})
	if strings.TrimSpace(signingKeySecret) != "" {
		checks = append(checks, struct {
			name string
			key  string
		}{name: strings.TrimSpace(signingKeySecret), key: "private-key.pem"})
	}
	tlsSecret, _ := readKubectlOutput(kubectl, []string{"get", "ingress", doctorMCPAuthDeployment, "-n", doctorSentinelNamespace, "-o", "jsonpath={.spec.tls[0].secretName}"})
	if strings.TrimSpace(tlsSecret) == "" && strings.TrimSpace(signingKeySecret) != "" {
		// Older production manifests used the default name without exposing it
		// through a custom Ingress query; retain a useful check for those installs.
		tlsSecret = "mcp-auth-server-tls"
	}
	if strings.TrimSpace(tlsSecret) != "" {
		checks = append(checks,
			struct {
				name string
				key  string
			}{name: strings.TrimSpace(tlsSecret), key: "tls.crt"},
			struct {
				name string
				key  string
			}{name: strings.TrimSpace(tlsSecret), key: "tls.key"},
		)
	}
	if len(checks) == 0 {
		return DoctorCheck{Name: "mcp-auth secrets", OK: true, Detail: "test-mode mcp-auth uses ephemeral signing and ingress credentials; skipping Secret checks"}
	}
	for _, secret := range checks {
		path := "{.data." + strings.ReplaceAll(secret.key, ".", `\.`) + "}"
		value, err := readKubectlOutput(kubectl, []string{"get", "secret", secret.name, "-n", doctorSentinelNamespace, "-o", "jsonpath=" + path})
		if err != nil || strings.TrimSpace(value) == "" {
			return DoctorCheck{
				Name:   "mcp-auth secrets",
				OK:     false,
				Detail: fmt.Sprintf("Secret %s is missing non-empty key %s", secret.name, secret.key),
				Remedy: "create the mcp-auth signing-key Secret or wait for the managed mcp-auth TLS Certificate to become ready",
			}
		}
	}
	return DoctorCheck{Name: "mcp-auth secrets", OK: true, Detail: "configured signing key and TLS Secret contain the required keys"}
}
