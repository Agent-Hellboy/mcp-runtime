package manifest_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTraefikNetworkPolicyAllowsMCPServerGatewayPort(t *testing.T) {
	policies := loadTraefikNetworkPolicies(t)
	egress, ok := policies["traefik-allow-egress"]
	if !ok {
		t.Fatal("traefik-allow-egress policy not found")
	}
	for _, port := range []int{8083, 8088, 8091} {
		if !hasEgressPort(egress, port) {
			t.Fatalf("traefik egress policy must allow TCP port %d for MCP gateway/app targets", port)
		}
	}
}

func loadTraefikNetworkPolicies(t *testing.T) map[string]networkPolicyDoc {
	t.Helper()

	path := filepath.Join("..", "..", "config", "ingress", "base", "networkpolicy.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read traefik networkpolicy: %v", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	policies := make(map[string]networkPolicyDoc)
	for {
		var doc networkPolicyDoc
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode traefik networkpolicy: %v", err)
		}
		if doc.Kind != "NetworkPolicy" || doc.Metadata.Name == "" {
			continue
		}
		policies[doc.Metadata.Name] = doc
	}
	return policies
}

func hasEgressPort(rule networkPolicyDoc, port int) bool {
	for _, egress := range rule.Spec.Egress {
		for _, p := range egress.Ports {
			if p.Protocol == "TCP" && p.Port == port {
				return true
			}
		}
	}
	return false
}
