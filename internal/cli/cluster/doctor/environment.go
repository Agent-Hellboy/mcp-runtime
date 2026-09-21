package doctor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mcp-runtime/internal/cli/core"
)

// checkNodeRuntimeHealth validates the kubelet-facing signals that are
// available through the Kubernetes API. It deliberately does not require
// access to a node's CRI socket, which would make Doctor unusable remotely and
// distribution-specific.
func checkNodeRuntimeHealth(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "nodes", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "node kubelet/runtime health", OK: false, Detail: fmt.Sprintf("failed listing nodes: %v", err), Remedy: "check kubeconfig access and Kubernetes API availability"}
	}
	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				NodeInfo struct {
					KubeletVersion          string `json:"kubeletVersion"`
					ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
				} `json:"nodeInfo"`
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return DoctorCheck{Name: "node kubelet/runtime health", OK: false, Detail: fmt.Sprintf("failed parsing node status: %v", err), Remedy: "rerun Doctor and inspect `kubectl get nodes -o json`"}
	}
	if len(payload.Items) == 0 {
		return DoctorCheck{Name: "node kubelet/runtime health", OK: false, Detail: "the Kubernetes API returned no nodes", Remedy: "check cluster bootstrap and node registration"}
	}
	problems := make([]string, 0)
	runtimes := make([]string, 0, len(payload.Items))
	for _, node := range payload.Items {
		name := strings.TrimSpace(node.Metadata.Name)
		ready := false
		for _, condition := range node.Status.Conditions {
			switch condition.Type {
			case "Ready":
				ready = condition.Status == "True"
				if !ready {
					problems = append(problems, fmt.Sprintf("%s Ready=%s (%s)", name, condition.Status, condition.Reason))
				}
			case "MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable":
				if condition.Status == "True" {
					problems = append(problems, fmt.Sprintf("%s %s", name, condition.Type))
				}
			}
		}
		if !ready && len(node.Status.Conditions) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no node conditions", name))
		}
		runtime := strings.TrimSpace(node.Status.NodeInfo.ContainerRuntimeVersion)
		if runtime == "" {
			problems = append(problems, fmt.Sprintf("%s has no container runtime version", name))
		} else {
			runtimes = append(runtimes, name+"="+runtime)
		}
	}
	if len(problems) > 0 {
		return DoctorCheck{Name: "node kubelet/runtime health", OK: false, Detail: strings.Join(limitStrings(problems, 6), "; "), Remedy: "inspect `kubectl describe node <name>` and kubelet/container-runtime logs on the affected node"}
	}
	return DoctorCheck{Name: "node kubelet/runtime health", OK: true, Detail: fmt.Sprintf("%d node(s) Ready; container runtimes: %s", len(payload.Items), strings.Join(runtimes, ", "))}
}

func checkRuntimeClassCompatibility(kubectl core.KubectlRunner) DoctorCheck {
	classes, err := readKubectlOutput(kubectl, []string{"get", "runtimeclass", "-o", "json"})
	if err != nil {
		// RuntimeClass is optional. Only fail if an MCPServer explicitly asks
		// for one that cannot be resolved.
		classes = `{"items":[]}`
	}
	var classList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(classes), &classList); err != nil {
		return DoctorCheck{Name: "runtime class compatibility", OK: false, Detail: fmt.Sprintf("failed parsing RuntimeClass objects: %v", err), Remedy: "inspect `kubectl get runtimeclass -o json`"}
	}
	available := map[string]bool{}
	for _, item := range classList.Items {
		available[strings.TrimSpace(item.Metadata.Name)] = true
	}
	mcp, err := readKubectlOutput(kubectl, []string{"get", "mcpservers", "-A", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "runtime class compatibility", OK: true, Detail: "MCPServer CRD is not installed yet; setup will validate RuntimeClass references after installation"}
	}
	var servers struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				RuntimeClassName string `json:"runtimeClassName"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(mcp), &servers); err != nil {
		return DoctorCheck{Name: "runtime class compatibility", OK: false, Detail: fmt.Sprintf("failed parsing MCPServers: %v", err), Remedy: "rerun Doctor and inspect MCPServer JSON"}
	}
	missing := make([]string, 0)
	for _, server := range servers.Items {
		class := strings.TrimSpace(server.Spec.RuntimeClassName)
		if class != "" && !available[class] {
			missing = append(missing, fmt.Sprintf("%s/%s requires %s", server.Metadata.Namespace, server.Metadata.Name, class))
		}
	}
	if len(missing) > 0 {
		return DoctorCheck{Name: "runtime class compatibility", OK: false, Detail: strings.Join(limitStrings(missing, 5), "; "), Remedy: "install the requested RuntimeClass or update the MCPServer to use a RuntimeClass available on this cluster"}
	}
	return DoctorCheck{Name: "runtime class compatibility", OK: true, Detail: fmt.Sprintf("%d RuntimeClass object(s); all configured MCPServer runtime classes resolve", len(available))}
}

func checkStorageReadiness(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "storageclass", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "storage readiness", OK: false, Detail: fmt.Sprintf("failed listing StorageClasses: %v", err), Remedy: "install a default StorageClass or configure an explicit persistence class"}
	}
	var classes struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &classes); err != nil {
		return DoctorCheck{Name: "storage readiness", OK: false, Detail: fmt.Sprintf("failed parsing StorageClasses: %v", err), Remedy: "inspect `kubectl get storageclass -o json`"}
	}
	defaults := 0
	for _, item := range classes.Items {
		if item.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || item.Metadata.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			defaults++
		}
	}
	pending, pendingErr := readKubectlOutput(kubectl, []string{"get", "pvc", "-A", "--field-selector=status.phase=Pending", "-o", "custom-columns=NS:.metadata.namespace,NAME:.metadata.name", "--no-headers"})
	if pendingErr != nil {
		return DoctorCheck{Name: "storage readiness", OK: false, Detail: fmt.Sprintf("failed checking Pending PVCs: %v", pendingErr), Remedy: "check PVC list permissions and the cluster storage controller"}
	}
	pendingLines := filterNonEmptyLines(pending)
	if len(classes.Items) == 0 {
		return DoctorCheck{Name: "storage readiness", OK: false, Detail: "no StorageClass is installed", Remedy: "install a StorageClass before enabling Kafka or other persistent components"}
	}
	if len(pendingLines) > 0 {
		return DoctorCheck{Name: "storage readiness", OK: false, Detail: fmt.Sprintf("%d PVC(s) are Pending", len(pendingLines)), Remedy: "inspect `kubectl get pvc -A` and the events for the affected claims; verify provisioner, capacity, and node affinity"}
	}
	return DoctorCheck{Name: "storage readiness", OK: true, Detail: fmt.Sprintf("%d StorageClass(es), %d default, no Pending PVCs", len(classes.Items), defaults)}
}

func checkNodeArchitectureCompatibility(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "nodes", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: "node architecture compatibility", OK: false, Detail: fmt.Sprintf("failed listing node architectures: %v", err), Remedy: "check Kubernetes API access"}
	}
	var nodes struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				NodeInfo struct {
					Architecture string `json:"architecture"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		return DoctorCheck{Name: "node architecture compatibility", OK: false, Detail: fmt.Sprintf("failed parsing node architectures: %v", err), Remedy: "inspect `kubectl get nodes -o json`"}
	}
	arches := map[string]int{}
	missing := 0
	for _, node := range nodes.Items {
		arch := strings.TrimSpace(node.Status.NodeInfo.Architecture)
		if arch == "" {
			missing++
			continue
		}
		arches[arch]++
	}
	if missing > 0 || len(arches) == 0 {
		return DoctorCheck{Name: "node architecture compatibility", OK: false, Detail: fmt.Sprintf("architecture missing on %d node(s)", missing), Remedy: "upgrade/register nodes with valid status.nodeInfo.architecture and build images for the cluster architecture"}
	}
	values := make([]string, 0, len(arches))
	for arch, count := range arches {
		values = append(values, fmt.Sprintf("%s=%d", arch, count))
	}
	sort.Strings(values)
	detail := strings.Join(values, ", ")
	if len(arches) > 1 {
		detail += "; mixed-architecture cluster: publish multi-arch images or constrain workloads with node selectors"
	}
	return DoctorCheck{Name: "node architecture compatibility", OK: true, Detail: detail}
}
