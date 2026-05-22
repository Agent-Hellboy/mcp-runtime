package doctor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mcp-runtime/internal/cli/core"
)

func checkPlatformAPILiveInventoryNetworkPolicy(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "mcpservers", "-A", "-o", buildMCPServerNamespaceJSONPath()})
	if err != nil {
		return DoctorCheck{
			Name:   "platform API live inventory ingress",
			OK:     false,
			Detail: fmt.Sprintf("failed listing MCPServer namespaces: %v", err),
			Remedy: "check MCPServer CRD availability and RBAC for listing MCPServers across namespaces",
		}
	}
	namespaces := teamNamespacesWithMCPServers(out)
	if len(namespaces) == 0 {
		return DoctorCheck{
			Name:   "platform API live inventory ingress",
			OK:     true,
			Detail: "no team MCPServer namespaces found",
		}
	}

	checked := 0
	for _, namespace := range namespaces {
		policyJSON, err := readKubectlOutput(kubectl, []string{"get", "networkpolicy", "platform-default-deny", "-n", namespace, "-o", "json"})
		if err != nil {
			continue
		}
		checked++
		if !networkPolicyAllowsPlatformAPI(policyJSON) {
			return DoctorCheck{
				Name:   "platform API live inventory ingress",
				OK:     false,
				Detail: fmt.Sprintf("networkpolicy %s/platform-default-deny blocks mcp-sentinel from probing MCPServer Services", namespace),
				Remedy: "rerun team provisioning or patch platform-default-deny to allow ingress from namespace mcp-sentinel",
			}
		}
	}
	if checked == 0 {
		return DoctorCheck{
			Name:   "platform API live inventory ingress",
			OK:     true,
			Detail: fmt.Sprintf("%d team MCPServer namespace(s) found; no platform-default-deny NetworkPolicy present", len(namespaces)),
		}
	}
	return DoctorCheck{
		Name:   "platform API live inventory ingress",
		OK:     true,
		Detail: fmt.Sprintf("%d team NetworkPolicy object(s) allow platform API live inventory probes", checked),
	}
}

func buildMCPServerNamespaceJSONPath() string {
	return `jsonpath={range .items[*]}{.metadata.namespace}{"\n"}{end}`
}

func teamNamespacesWithMCPServers(out string) []string {
	seen := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		namespace := strings.TrimSpace(line)
		if !strings.HasPrefix(namespace, "mcp-team-") {
			continue
		}
		seen[namespace] = struct{}{}
	}
	namespaces := make([]string, 0, len(seen))
	for namespace := range seen {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return namespaces
}

type doctorNetworkPolicy struct {
	Spec struct {
		Ingress []struct {
			From []struct {
				NamespaceSelector *struct {
					MatchLabels map[string]string `json:"matchLabels"`
				} `json:"namespaceSelector"`
				PodSelector *struct {
					MatchLabels map[string]string `json:"matchLabels"`
				} `json:"podSelector"`
			} `json:"from"`
		} `json:"ingress"`
	} `json:"spec"`
}

func networkPolicyAllowsPlatformAPI(raw string) bool {
	var policy doctorNetworkPolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return false
	}
	for _, rule := range policy.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.NamespaceSelector == nil {
				continue
			}
			if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != doctorSentinelNamespace {
				continue
			}
			if peer.PodSelector == nil {
				return true
			}
			labels := peer.PodSelector.MatchLabels
			if len(labels) == 0 || labels["app"] == doctorSentinelAPIService {
				return true
			}
		}
	}
	return false
}
