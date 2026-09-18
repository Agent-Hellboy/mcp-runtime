package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
)

// checkSentinelTelemetryPipeline verifies the bundled telemetry path instead
// of treating a healthy application deployment as proof that traces can be
// exported. It remains distribution-neutral by discovering the Service and
// checking the deployed workload rather than assuming a node address.
func checkSentinelTelemetryPipeline(kubectl core.KubectlRunner) DoctorCheck {
	if _, err := readKubectlOutput(kubectl, []string{"get", "namespace", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: true, Detail: "namespace mcp-sentinel not found; skipping telemetry check"}
	}

	collectorPair, collectorReady, err := doctorDeploymentReplicaStatus(kubectl, doctorSentinelNamespace, "otel-collector")
	if err != nil {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: false, Detail: err.Error(), Remedy: "inspect the otel-collector deployment and its ConfigMap"}
	}
	if !collectorReady {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: false, Detail: fmt.Sprintf("otel-collector %s replicas ready", collectorPair), Remedy: "inspect `kubectl -n mcp-sentinel logs deploy/otel-collector` and the collector ConfigMap"}
	}

	if _, err := readKubectlOutput(kubectl, []string{"get", "service", "otel-collector", "-n", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: false, Detail: fmt.Sprintf("otel-collector Service is unavailable: %v", err), Remedy: "restore Service/otel-collector and its selector"}
	}
	addresses, err := readKubectlOutput(kubectl, []string{"get", "endpoints", "otel-collector", "-n", doctorSentinelNamespace, "-o", "jsonpath={.subsets[*].addresses[*].ip}"})
	if err != nil || strings.TrimSpace(addresses) == "" {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: false, Detail: "otel-collector Service has no ready endpoints", Remedy: "check the collector pod labels, readiness probe, and Service selector"}
	}

	if _, err := readKubectlOutput(kubectl, []string{"get", "configmap", "otel-collector-config", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.otel-collector-config.yaml}"}); err != nil {
		return DoctorCheck{Name: "sentinel telemetry pipeline", OK: false, Detail: fmt.Sprintf("otel-collector ConfigMap is unavailable: %v", err), Remedy: "restore ConfigMap/otel-collector-config and its trace pipeline"}
	}
	return DoctorCheck{Name: "sentinel telemetry pipeline", OK: true, Detail: fmt.Sprintf("otel-collector %s ready with a Service endpoint", collectorPair)}
}

func checkPersistentVolumeClaims(kubectl core.KubectlRunner) DoctorCheck {
	raw, err := readKubectlOutput(kubectl, []string{"get", "pvc", "-A", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "persistent volume claims", OK: false, Detail: fmt.Sprintf("failed reading PVC status: %v", err), Remedy: "check Kubernetes storage permissions and PVC status"}
	}
	var payload struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return DoctorCheck{Name: "persistent volume claims", OK: false, Detail: fmt.Sprintf("failed parsing PVC status: %v", err), Remedy: "inspect PVC objects and storage provisioner events"}
	}
	var waiting []string
	for _, item := range payload.Items {
		phase := strings.TrimSpace(item.Status.Phase)
		if phase != "Bound" {
			waiting = append(waiting, fmt.Sprintf("%s/%s (%s)", item.Metadata.Namespace, item.Metadata.Name, phase))
		}
	}
	if len(waiting) > 0 {
		return DoctorCheck{Name: "persistent volume claims", OK: false, Detail: fmt.Sprintf("PVCs are not Bound: %s", strings.Join(waiting, ", ")), Remedy: "inspect PVC events, StorageClass provisioning, capacity, and node affinity"}
	}
	if len(payload.Items) == 0 {
		return DoctorCheck{Name: "persistent volume claims", OK: true, Detail: "no PVCs are present; skipping persistence validation"}
	}
	return DoctorCheck{Name: "persistent volume claims", OK: true, Detail: fmt.Sprintf("%d PVC(s) are Bound", len(payload.Items))}
}
