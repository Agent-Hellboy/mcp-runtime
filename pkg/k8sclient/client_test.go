package k8sclient

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildKubeconfigAcceptsExplicitPathList(t *testing.T) {
	dir := t.TempDir()
	first := writeTestKubeconfig(t, dir, "first.yaml", "https://explicit.example.com")
	second := writeTestKubeconfig(t, dir, "second.yaml", "https://explicit.example.com")

	cfg, err := buildKubeconfig(first + string(os.PathListSeparator) + second)
	if err != nil {
		t.Fatalf("buildKubeconfig() error = %v", err)
	}
	if cfg.Host != "https://explicit.example.com" {
		t.Fatalf("config.Host = %q, want %q", cfg.Host, "https://explicit.example.com")
	}
}

func TestBuildKubeconfigUsesKubeconfigEnvPathList(t *testing.T) {
	dir := t.TempDir()
	first := writeTestKubeconfig(t, dir, "first.yaml", "https://env.example.com")
	second := writeTestKubeconfig(t, dir, "second.yaml", "https://env.example.com")
	t.Setenv("KUBECONFIG", first+string(os.PathListSeparator)+second)

	cfg, err := buildKubeconfig("")
	if err != nil {
		t.Fatalf("buildKubeconfig() error = %v", err)
	}
	if cfg.Host != "https://env.example.com" {
		t.Fatalf("config.Host = %q, want %q", cfg.Host, "https://env.example.com")
	}
}

func writeTestKubeconfig(t *testing.T, dir, name, server string) string {
	t.Helper()

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
current-context: test
users:
- name: test
  user:
    token: test-token
`, server)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
