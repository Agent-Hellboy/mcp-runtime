package k8sclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildKubeconfigAcceptsExplicitPathList(t *testing.T) {
	dir := t.TempDir()
	first := writeTestKubeconfig(t, dir, "first.yaml", "https://explicit.example.com")
	second := writeTestKubeconfig(t, dir, "second.yaml", "https://explicit.example.com")

	cfg, namespace, err := buildKubeconfig(first + string(os.PathListSeparator) + second)
	if err != nil {
		t.Fatalf("buildKubeconfig() error = %v", err)
	}
	if cfg.Host != "https://explicit.example.com" {
		t.Fatalf("config.Host = %q, want %q", cfg.Host, "https://explicit.example.com")
	}
	if namespace != "default" {
		t.Fatalf("namespace = %q, want default", namespace)
	}
}

func TestBuildKubeconfigUsesKubeconfigEnvPathList(t *testing.T) {
	dir := t.TempDir()
	first := writeTestKubeconfig(t, dir, "first.yaml", "https://env.example.com")
	second := writeTestKubeconfig(t, dir, "second.yaml", "https://env.example.com")
	t.Setenv("KUBECONFIG", first+string(os.PathListSeparator)+second)

	cfg, namespace, err := buildKubeconfig("")
	if err != nil {
		t.Fatalf("buildKubeconfig() error = %v", err)
	}
	if cfg.Host != "https://env.example.com" {
		t.Fatalf("config.Host = %q, want %q", cfg.Host, "https://env.example.com")
	}
	if namespace != "default" {
		t.Fatalf("namespace = %q, want default", namespace)
	}
}

func TestBuildKubeconfigReturnsCurrentContextNamespace(t *testing.T) {
	dir := t.TempDir()
	path := writeTestKubeconfigWithNamespace(t, dir, "namespaced.yaml", "https://ns.example.com", "mcp-servers")

	_, namespace, err := buildKubeconfig(path)
	if err != nil {
		t.Fatalf("buildKubeconfig() error = %v", err)
	}
	if namespace != "mcp-servers" {
		t.Fatalf("namespace = %q, want mcp-servers", namespace)
	}
}

func TestNewFromConfigRejectsNil(t *testing.T) {
	clients, err := NewFromConfig(nil)
	if err == nil {
		t.Fatal("NewFromConfig(nil) error = nil, want non-nil")
	}
	if clients != nil {
		t.Fatalf("NewFromConfig(nil) clients = %#v, want nil", clients)
	}
	if !strings.Contains(err.Error(), "rest config cannot be nil") {
		t.Fatalf("NewFromConfig(nil) error = %q, want nil-config message", err)
	}
}

func TestEnvNamespaceTrimsWhitespace(t *testing.T) {
	t.Setenv("NAMESPACE", "  mcp-servers  ")
	if got := envNamespace(); got != "mcp-servers" {
		t.Fatalf("envNamespace() = %q, want %q", got, "mcp-servers")
	}
}

func TestEnvNamespaceWhitespaceFallsBackToDefault(t *testing.T) {
	t.Setenv("NAMESPACE", "   ")
	if got := envNamespace(); got != "" {
		t.Fatalf("envNamespace() = %q, want empty string", got)
	}
}

func writeTestKubeconfig(t *testing.T, dir, name, server string) string {
	return writeTestKubeconfigWithNamespace(t, dir, name, server, "")
}

func writeTestKubeconfigWithNamespace(t *testing.T, dir, name, server, namespace string) string {
	t.Helper()

	var namespaceLine string
	if namespace != "" {
		namespaceLine = fmt.Sprintf("    namespace: %s\n", namespace)
	}
	path := filepath.Join(dir, name)
	data := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
%s
current-context: test
users:
- name: test
  user:
    token: test-token
`, server, namespaceLine)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
