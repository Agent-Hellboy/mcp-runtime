package k8sclient

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
)

func TestChooseRegistryPublicHostPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		in         RegistryHostSources
		wantHost   string
		wantSource string
	}{
		{"explicit public wins", RegistryHostSources{Explicit: "registry.new.org", IngressTLSHosts: []string{"registry.old.org"}}, "registry.new.org", RegistryHostSourceExplicit},
		{"placeholder explicit falls back to ingress tls", RegistryHostSources{Explicit: "registry.local", IngressTLSHosts: []string{"registry.mcpruntime.org"}, IngressRuleHosts: []string{"registry.local"}}, "registry.mcpruntime.org", RegistryHostSourceIngressTLS},
		{"rule host when no tls", RegistryHostSources{IngressRuleHosts: []string{"registry.example.com"}}, "registry.example.com", RegistryHostSourceIngressRule},
		{"config ingress host", RegistryHostSources{IngressRuleHosts: []string{"registry.local"}, ConfigIngressHost: "registry.cfg.org"}, "registry.cfg.org", RegistryHostSourceConfigHost},
		{"config platform domain", RegistryHostSources{ConfigIngressHost: "registry.local", ConfigPlatformDomain: "Mcpruntime.org."}, "registry.mcpruntime.org", RegistryHostSourceConfigDomain},
		{"nothing public keeps placeholder", RegistryHostSources{Explicit: "", IngressRuleHosts: []string{"registry.local"}, ConfigIngressHost: "registry.local"}, RegistryPlaceholderHost, RegistryHostSourcePlaceholder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, source := ChooseRegistryPublicHost(tc.in)
			if host != tc.wantHost || source != tc.wantSource {
				t.Fatalf("got (%q, %q), want (%q, %q)", host, source, tc.wantHost, tc.wantSource)
			}
		})
	}
}

func TestResolveRegistryPublicHostReadsClusterState(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: RegistryIngressName, Namespace: RegistryIngressNamespace},
			Spec: networkingv1.IngressSpec{
				TLS:   []networkingv1.IngressTLS{{Hosts: []string{"registry.mcpruntime.org"}, SecretName: "registry-tls"}},
				Rules: []networkingv1.IngressRule{{Host: "registry.local"}},
			},
		},
	)
	host, source := ResolveRegistryPublicHost(context.Background(), &Clients{Clientset: cs}, "registry.local")
	if host != "registry.mcpruntime.org" || source != RegistryHostSourceIngressTLS {
		t.Fatalf("got (%q, %q)", host, source)
	}

	cs = fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platformConfigName, Namespace: platformConfigNamespace},
		Data:       map[string]string{"MCP_REGISTRY_INGRESS_HOST": "registry.local", "MCP_PLATFORM_DOMAIN": "e2e.example.org"},
	})
	host, _ = ResolveRegistryPublicHost(context.Background(), &Clients{Clientset: cs}, "")
	if host != "registry.e2e.example.org" {
		t.Fatalf("got %q, want registry.e2e.example.org", host)
	}

	host, source = ResolveRegistryPublicHost(context.Background(), &Clients{Clientset: fake.NewSimpleClientset()}, "")
	if host != RegistryPlaceholderHost || source != RegistryHostSourcePlaceholder {
		t.Fatalf("fresh cluster: got (%q, %q)", host, source)
	}
}

func TestCheckRegistryIngressDowngrade(t *testing.T) {
	pub := []string{"registry.mcpruntime.org"}
	local := []string{"registry.local"}
	if err := CheckRegistryIngressDowngrade(local, nil, local, pub); err == nil || !strings.Contains(err.Error(), "registry.mcpruntime.org") {
		t.Fatalf("expected refusal naming public host when live TLS is public, got %v", err)
	}
	if err := CheckRegistryIngressDowngrade(local, pub, nil, nil); err == nil {
		t.Fatal("expected refusal when desired TLS host is public")
	}
	if err := CheckRegistryIngressDowngrade(local, nil, pub, nil); err == nil {
		t.Fatal("expected refusal when live rule host is public")
	}
	if err := CheckRegistryIngressDowngrade(local, local, local, nil); err != nil {
		t.Fatalf("dev install must pass: %v", err)
	}
	if err := CheckRegistryIngressDowngrade(pub, pub, local, local); err != nil {
		t.Fatalf("upgrade to a public host must pass: %v", err)
	}
}

func TestGuardRegistryIngressApplyRejectsBaseManifestOverPublicTLS(t *testing.T) {
	desired := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
		"metadata": map[string]any{"name": "registry", "namespace": "registry"},
		"spec":     map[string]any{"rules": []any{map[string]any{"host": "registry.local"}}},
	}}
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
		"metadata": map[string]any{"name": "registry", "namespace": "registry"},
		"spec": map[string]any{
			"rules": []any{map[string]any{"host": "registry.mcpruntime.org"}},
			"tls":   []any{map[string]any{"hosts": []any{"registry.mcpruntime.org"}, "secretName": "registry-tls"}},
		},
	}}
	if err := guardRegistryIngressApply(desired, "registry", live, nil); err == nil {
		t.Fatal("expected guard to refuse registry.local over a public Ingress")
	}
	other := desired.DeepCopy()
	other.SetName("mcp-server")
	if err := guardRegistryIngressApply(other, "registry", live, nil); err != nil {
		t.Fatalf("non-registry Ingress must not be guarded: %v", err)
	}
}

func TestCheckRegistryIngressManifest(t *testing.T) {
	cs := fake.NewSimpleClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: RegistryIngressName, Namespace: RegistryIngressNamespace},
		Spec:       networkingv1.IngressSpec{TLS: []networkingv1.IngressTLS{{Hosts: []string{"registry.mcpruntime.org"}}}},
	})
	manifest := "apiVersion: v1\nkind: Service\nmetadata:\n  name: registry\n---\napiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: registry\n  namespace: registry\nspec:\n  rules:\n  - host: registry.local\n"
	if err := CheckRegistryIngressManifest(context.Background(), &Clients{Clientset: cs}, manifest); err == nil {
		t.Fatal("expected refusal")
	}
	fixed := strings.ReplaceAll(manifest, "registry.local", "registry.mcpruntime.org")
	if err := CheckRegistryIngressManifest(context.Background(), &Clients{Clientset: cs}, fixed); err != nil {
		t.Fatalf("public host manifest must pass: %v", err)
	}
}
