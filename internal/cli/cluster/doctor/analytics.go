package doctor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
)

func checkGatewayAnalyticsCredentials(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "deploy", "-A", "-o", "json"})
	if err != nil {
		return DoctorCheck{
			Name:   "gateway analytics credentials",
			OK:     false,
			Detail: fmt.Sprintf("failed listing deployments: %v", err),
			Remedy: "check Kubernetes API access and RBAC for listing Deployments",
		}
	}

	var deployments doctorDeploymentList
	if err := json.Unmarshal([]byte(out), &deployments); err != nil {
		return DoctorCheck{
			Name:   "gateway analytics credentials",
			OK:     false,
			Detail: fmt.Sprintf("failed parsing deployments JSON: %v", err),
			Remedy: "rerun cluster doctor; if this persists, inspect `kubectl get deploy -A -o json`",
		}
	}

	checked := 0
	failures := make([]string, 0)
	for _, deployment := range deployments.Items {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name != "mcp-gateway" {
				continue
			}
			if envValue(container.Env, "ANALYTICS_INGEST_URL") == "" {
				continue
			}
			checked++
			apiKeyEnv, ok := envByName(container.Env, "ANALYTICS_API_KEY")
			if !ok {
				failures = append(failures, fmt.Sprintf("%s/%s missing ANALYTICS_API_KEY", deployment.Metadata.Namespace, deployment.Metadata.Name))
				continue
			}
			if strings.TrimSpace(apiKeyEnv.Value) != "" {
				continue
			}
			if apiKeyEnv.ValueFrom == nil || apiKeyEnv.ValueFrom.SecretKeyRef == nil {
				failures = append(failures, fmt.Sprintf("%s/%s ANALYTICS_API_KEY has no value or secretKeyRef", deployment.Metadata.Namespace, deployment.Metadata.Name))
				continue
			}
			ref := apiKeyEnv.ValueFrom.SecretKeyRef
			if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Key) == "" {
				failures = append(failures, fmt.Sprintf("%s/%s ANALYTICS_API_KEY secretKeyRef is incomplete", deployment.Metadata.Namespace, deployment.Metadata.Name))
				continue
			}
			if err := checkSecretKeyPopulated(kubectl, deployment.Metadata.Namespace, ref.Name, ref.Key); err != nil {
				failures = append(failures, fmt.Sprintf("%s/%s references unusable Secret %s/%s: %v", deployment.Metadata.Namespace, deployment.Metadata.Name, ref.Name, ref.Key, err))
			}
		}
	}

	if len(failures) > 0 {
		return DoctorCheck{
			Name:   "gateway analytics credentials",
			OK:     false,
			Detail: strings.Join(limitStrings(failures, 4), "; "),
			Remedy: "create a namespace-local ingest-key Secret and set spec.analytics.apiKeySecretRef on affected MCPServers, or redeploy with `mcp-runtime server deploy --metadata-dir .mcp`",
		}
	}
	if checked == 0 {
		return DoctorCheck{
			Name:   "gateway analytics credentials",
			OK:     true,
			Detail: "no gateway sidecars with analytics ingest URLs found",
		}
	}
	return DoctorCheck{
		Name:   "gateway analytics credentials",
		OK:     true,
		Detail: fmt.Sprintf("%d gateway sidecar(s) have usable analytics credentials", checked),
	}
}

type doctorDeploymentList struct {
	Items []doctorDeployment `json:"items"`
}

type doctorDeployment struct {
	Metadata struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []doctorContainer `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

type doctorContainer struct {
	Name string         `json:"name"`
	Env  []doctorEnvVar `json:"env"`
}

type doctorEnvVar struct {
	Name      string              `json:"name"`
	Value     string              `json:"value"`
	ValueFrom *doctorEnvVarSource `json:"valueFrom"`
}

type doctorEnvVarSource struct {
	SecretKeyRef *doctorSecretKeyRef `json:"secretKeyRef"`
}

type doctorSecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func envByName(env []doctorEnvVar, name string) (doctorEnvVar, bool) {
	for _, item := range env {
		if item.Name == name {
			return item, true
		}
	}
	return doctorEnvVar{}, false
}

func envValue(env []doctorEnvVar, name string) string {
	item, ok := envByName(env, name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(item.Value)
}

func checkSecretKeyPopulated(kubectl core.KubectlRunner, namespace, name, key string) error {
	out, err := readKubectlOutput(kubectl, []string{"get", "secret", name, "-n", namespace, "-o", "json"})
	if err != nil {
		return err
	}
	var secret struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &secret); err != nil {
		return err
	}
	encoded, ok := secret.Data[key]
	if !ok {
		return fmt.Errorf("key missing")
	}
	decodedBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return fmt.Errorf("decode secret key %q: %w", key, err)
	}
	if strings.TrimSpace(string(decodedBytes)) == "" {
		return fmt.Errorf("key empty")
	}
	return nil
}

func limitStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	out := append([]string{}, values[:max]...)
	out = append(out, fmt.Sprintf("%d more", len(values)-max))
	return out
}
