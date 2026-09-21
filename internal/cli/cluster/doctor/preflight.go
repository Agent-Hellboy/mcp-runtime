package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
)

func checkClusterNodesReady(kubectl core.KubectlRunner) DoctorCheck {
	raw, err := readKubectlOutput(kubectl, []string{"get", "nodes", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "Kubernetes nodes ready", OK: false, Detail: fmt.Sprintf("cannot read cluster nodes: %v", err), Remedy: "check kubeconfig, RBAC, and Kubernetes API availability"}
	}
	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return DoctorCheck{Name: "Kubernetes nodes ready", OK: false, Detail: fmt.Sprintf("cannot parse node status: %v", err), Remedy: "inspect `kubectl get nodes` and node controller events"}
	}
	if len(payload.Items) == 0 {
		return DoctorCheck{Name: "Kubernetes nodes ready", OK: false, Detail: "cluster has no nodes", Remedy: "join or restore at least one Ready Kubernetes node"}
	}
	var notReady []string
	for _, node := range payload.Items {
		ready := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		if !ready {
			notReady = append(notReady, node.Metadata.Name)
		}
	}
	if len(notReady) > 0 {
		return DoctorCheck{Name: "Kubernetes nodes ready", OK: false, Detail: fmt.Sprintf("not-ready node(s): %s", strings.Join(notReady, ", ")), Remedy: "inspect `kubectl describe node` and restore node Ready status before setup"}
	}
	return DoctorCheck{Name: "Kubernetes nodes ready", OK: true, Detail: fmt.Sprintf("%d node(s) are Ready", len(payload.Items))}
}
