package metadata

import (
	"os"
	"strings"
	"testing"
)

func TestResolveRegistryPullHost(t *testing.T) {
	for _, k := range []string{envMCPRegistryPullHost, envMCPRegistryEndpoint, envMCPRegistryHost, envMCPRegistryIngressHost, envMCPPlatformDomain} {
		t.Setenv(k, "")
	}

	if got := ResolveRegistryPullHost(); got != defaultRegistryPullHost {
		t.Fatalf("default pull host = %q, want %q", got, defaultRegistryPullHost)
	}

	t.Setenv(envMCPRegistryEndpoint, "10.43.69.247:5000")
	if got := ResolveRegistryPullHost(); got != "10.43.69.247:5000" {
		t.Fatalf("endpoint pull host = %q", got)
	}

	t.Setenv(envMCPRegistryPullHost, "registry.registry.svc.cluster.local:5000")
	if got := ResolveRegistryPullHost(); got != "registry.registry.svc.cluster.local:5000" {
		t.Fatalf("explicit pull host = %q", got)
	}
}

func TestResolveRegistryPullHostIngressFallback(t *testing.T) {
	for _, k := range []string{envMCPRegistryPullHost, envMCPRegistryEndpoint, envMCPPlatformDomain} {
		t.Setenv(k, "")
	}
	t.Setenv(envMCPRegistryIngressHost, "registry.mcpruntime.org")
	if got := ResolveRegistryPullHost(); got != "registry.mcpruntime.org" {
		t.Fatalf("ingress fallback pull host = %q, want %q", got, "registry.mcpruntime.org")
	}
}

func TestResolveRegistryPullHostPlatformDomainFallback(t *testing.T) {
	for _, k := range []string{envMCPRegistryPullHost, envMCPRegistryEndpoint, envMCPRegistryIngressHost} {
		t.Setenv(k, "")
	}
	t.Setenv(envMCPPlatformDomain, "mcpruntime.org")
	if got := ResolveRegistryPullHost(); got != "registry.mcpruntime.org" {
		t.Fatalf("platform domain fallback pull host = %q, want %q", got, "registry.mcpruntime.org")
	}
}

func TestGenerateCRDNoRewriteWhenIngressHostSet(t *testing.T) {
	for _, k := range []string{envMCPRegistryPullHost, envMCPRegistryEndpoint, envMCPRegistryHost} {
		t.Setenv(k, "")
	}
	t.Setenv(envMCPRegistryIngressHost, "registry.mcpruntime.org")

	server := &ServerMetadata{
		Name:      "acme-tools",
		Namespace: "mcp-team-acme",
		Image:     "registry.mcpruntime.org/acme/acme-tools",
		ImageTag:  "v0.1.0",
		Port:      8088,
	}
	tmp := t.TempDir()
	path := tmp + "/acme-tools.yaml"
	if err := GenerateCRD(server, path); err != nil {
		t.Fatalf("GenerateCRD: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated CRD: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "image: registry.mcpruntime.org/acme/acme-tools") {
		t.Fatalf("expected public registry image (no rewrite), got:\n%s", content)
	}
	if strings.Contains(content, "registry.registry.svc.cluster.local") {
		t.Fatalf("image was incorrectly rewritten to cluster-local pull host:\n%s", content)
	}
}

func TestRewriteImageRegistryHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		image    string
		registry string
		want     string
	}{
		{"registry.mcpruntime.org/acme/acme-tools", "10.43.69.247:5000", "10.43.69.247:5000/acme/acme-tools"},
		{"acme-tools", "registry.registry.svc.cluster.local:5000", "registry.registry.svc.cluster.local:5000/acme-tools"},
		{"localhost:5000/demo", "10.0.0.1:5000", "10.0.0.1:5000/demo"},
	}
	for _, tc := range cases {
		got, ok := RewriteImageRegistryHost(tc.image, tc.registry)
		if !ok {
			t.Fatalf("RewriteImageRegistryHost(%q, %q) returned ok=false", tc.image, tc.registry)
		}
		if got != tc.want {
			t.Fatalf("RewriteImageRegistryHost(%q, %q) = %q, want %q", tc.image, tc.registry, got, tc.want)
		}
	}
}

func TestGenerateCRDRewritesImageToPullHost(t *testing.T) {
	t.Setenv(envMCPRegistryPullHost, "10.43.69.247:5000")
	t.Setenv(envMCPRegistryIngressHost, "registry.mcpruntime.org")

	server := &ServerMetadata{
		Name:      "acme-tools",
		Namespace: "mcp-team-acme",
		Image:     "registry.mcpruntime.org/acme/acme-tools",
		ImageTag:  "v0.1.0",
		Port:      8088,
	}
	tmp := t.TempDir()
	path := tmp + "/acme-tools.yaml"
	if err := GenerateCRD(server, path); err != nil {
		t.Fatalf("GenerateCRD: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated CRD: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "image: 10.43.69.247:5000/acme/acme-tools") {
		t.Fatalf("expected pull-host image rewrite, got:\n%s", content)
	}
}
