package doctor

import (
	"strings"
	"testing"
)

func TestEvaluateRegistryIngressHosts(t *testing.T) {
	// Production symptom: rule host downgraded to registry.local, TLS still public.
	got := evaluateRegistryIngressHosts([]string{"registry.local"}, []string{"registry.mcpruntime.org"}, "registry.mcpruntime.org", "mcpruntime.org")
	if got.OK {
		t.Fatalf("expected failure, got %+v", got)
	}
	if !strings.Contains(got.Remedy, `"value":"registry.mcpruntime.org"`) || !strings.Contains(got.Remedy, "kubectl patch ingress registry -n registry") {
		t.Fatalf("remedy must print the exact patch command, got %q", got.Remedy)
	}

	if got := evaluateRegistryIngressHosts([]string{"registry.mcpruntime.org"}, []string{"registry.mcpruntime.org"}, "registry.mcpruntime.org", "mcpruntime.org"); !got.OK {
		t.Fatalf("consistent public install must pass: %+v", got)
	}
	// Kind/test-mode: placeholder everywhere, no TLS.
	if got := evaluateRegistryIngressHosts([]string{"registry.local"}, nil, "registry.local", ""); !got.OK {
		t.Fatalf("dev install must pass: %+v", got)
	}
	// Platform config says public but the Ingress has no TLS and a placeholder rule.
	got = evaluateRegistryIngressHosts([]string{"registry.local"}, nil, "", "example.org")
	if got.OK || !strings.Contains(got.Remedy, `"value":"registry.example.org"`) {
		t.Fatalf("expected failure with platform host remedy, got %+v", got)
	}
}
