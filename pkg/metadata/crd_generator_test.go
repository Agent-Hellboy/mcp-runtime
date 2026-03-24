package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCRD(t *testing.T) {
	t.Run("generates valid CRD YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputPath := filepath.Join(tmpDir, "test-server.yaml")

		replicas := int32(2)
		server := &ServerMetadata{
			Name:      "test-server",
			Image:     "my-image",
			ImageTag:  "v1.0.0",
			Route:     "/test/mcp",
			Port:      9000,
			Replicas:  &replicas,
			Namespace: "custom-ns",
		}

		err := GenerateCRD(server, outputPath)
		if err != nil {
			t.Fatalf("GenerateCRD failed: %v", err)
		}

		// Verify file exists
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)

		// Verify YAML content (yaml.v3 uses lowercase keys)
		assertContains(t, content, "apiversion: mcpruntime.org/v1alpha1")
		assertContains(t, content, "kind: MCPServer")
		assertContains(t, content, "name: test-server")
		assertContains(t, content, "namespace: custom-ns")
		assertContains(t, content, "image: my-image")
		assertContains(t, content, "imagetag: v1.0.0")
		assertContains(t, content, "port: 9000")
		assertContains(t, content, "replicas: 2")
		assertContains(t, content, "ingresspath: /test/mcp")
	})

	t.Run("generates CRD with resources", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputPath := filepath.Join(tmpDir, "resource-server.yaml")

		server := &ServerMetadata{
			Name:      "resource-server",
			Image:     "my-image",
			Namespace: "default",
			Resources: &ResourceRequirements{
				Limits: &ResourceList{
					CPU:    "500m",
					Memory: "512Mi",
				},
				Requests: &ResourceList{
					CPU:    "100m",
					Memory: "128Mi",
				},
			},
		}

		err := GenerateCRD(server, outputPath)
		if err != nil {
			t.Fatalf("GenerateCRD failed: %v", err)
		}

		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)
		assertContains(t, content, "cpu: 500m")
		assertContains(t, content, "memory: 512Mi")
		assertContains(t, content, "cpu: 100m")
		assertContains(t, content, "memory: 128Mi")
	})

	t.Run("generates CRD with environment variables", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputPath := filepath.Join(tmpDir, "env-server.yaml")

		server := &ServerMetadata{
			Name:      "env-server",
			Image:     "my-image",
			Namespace: "default",
			EnvVars: []EnvVar{
				{Name: "DATABASE_URL", Value: "postgres://localhost"},
				{Name: "LOG_LEVEL", Value: "debug"},
			},
		}

		err := GenerateCRD(server, outputPath)
		if err != nil {
			t.Fatalf("GenerateCRD failed: %v", err)
		}

		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)
		assertContains(t, content, "name: DATABASE_URL")
		assertContains(t, content, "value: postgres://localhost")
		assertContains(t, content, "name: LOG_LEVEL")
		assertContains(t, content, "value: debug")
	})

	t.Run("creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputPath := filepath.Join(tmpDir, "nested", "dirs", "server.yaml")

		server := &ServerMetadata{
			Name:      "nested-server",
			Image:     "my-image",
			Namespace: "default",
		}

		err := GenerateCRD(server, outputPath)
		if err != nil {
			t.Fatalf("GenerateCRD failed: %v", err)
		}

		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Error("expected file to be created in nested directory")
		}
	})
}

func TestGenerateCRDsFromRegistry(t *testing.T) {
	t.Run("generates CRDs for all servers", func(t *testing.T) {
		tmpDir := t.TempDir()

		replicas := int32(1)
		registry := &RegistryFile{
			Version: "v1",
			Servers: []ServerMetadata{
				{
					Name:      "server-one",
					Image:     "image-one",
					Namespace: "ns1",
					Replicas:  &replicas,
				},
				{
					Name:      "server-two",
					Image:     "image-two",
					Namespace: "ns2",
					Replicas:  &replicas,
				},
			},
		}

		err := GenerateCRDsFromRegistry(registry, tmpDir)
		if err != nil {
			t.Fatalf("GenerateCRDsFromRegistry failed: %v", err)
		}

		// Verify both files exist
		for _, name := range []string{"server-one.yaml", "server-two.yaml"} {
			path := filepath.Join(tmpDir, name)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("expected file %s to exist", name)
			}
		}

		// Verify content of first file
		data, err := os.ReadFile(filepath.Join(tmpDir, "server-one.yaml"))
		if err != nil {
			t.Fatalf("failed to read server-one.yaml: %v", err)
		}
		assertContains(t, string(data), "name: server-one")
		assertContains(t, string(data), "image: image-one")
	})

	t.Run("creates output directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "new-dir")

		registry := &RegistryFile{
			Version: "v1",
			Servers: []ServerMetadata{
				{Name: "test", Image: "test", Namespace: "default"},
			},
		}

		err := GenerateCRDsFromRegistry(registry, outputDir)
		if err != nil {
			t.Fatalf("GenerateCRDsFromRegistry failed: %v", err)
		}

		if _, err := os.Stat(outputDir); os.IsNotExist(err) {
			t.Error("expected output directory to be created")
		}
	})

	t.Run("handles empty registry", func(t *testing.T) {
		tmpDir := t.TempDir()

		registry := &RegistryFile{
			Version: "v1",
			Servers: []ServerMetadata{},
		}

		err := GenerateCRDsFromRegistry(registry, tmpDir)
		if err != nil {
			t.Fatalf("GenerateCRDsFromRegistry failed: %v", err)
		}

		// Verify no files created
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("failed to read directory: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected empty directory, got %d entries", len(entries))
		}
	})
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("expected content to contain %q, got:\n%s", substr, content)
	}
}
