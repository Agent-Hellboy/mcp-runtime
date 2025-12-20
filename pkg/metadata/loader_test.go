package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     *RegistryFile
		wantErr  bool
	}{
		{
			name:     "valid-yaml",
			filePath: "testdata/valid.yaml",
			want: &RegistryFile{
				Version: "v1",
				Servers: []ServerMetadata{
					{
						Name:      "test-server",
						Image:     "registry.registry.svc.cluster.local:5000/test-server",
						ImageTag:  "latest",
						Route:     "/test-server/mcp",
						Port:      8088,
						Replicas:  int32Ptr(1),
						Namespace: "mcp-servers",
					},
					{
						Name:      "custom-server",
						Image:     "custom/image",
						ImageTag:  "v1",
						Route:     "/custom-route",
						Port:      9090,
						Replicas:  int32Ptr(3),
						Namespace: "custom-namespace",
					},
				},
			},
		},
		{
			name:     "invalid-yaml",
			filePath: "testdata/invalid.yaml",
			want:     nil,
			wantErr:  true,
		},
		{
			name:     "missing-file",
			filePath: "testdata/missing.yaml",
			want:     nil,
			wantErr:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := LoadFromFile(test.filePath)
			if test.wantErr {
				if err == nil {
					t.Fatalf("LoadFromFile(%q) expected error, got nil", test.filePath)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFromFile(%q) unexpected error: %v", test.filePath, err)
			}
			if registry.Version != test.want.Version {
				t.Fatalf("LoadFromFile(%q) version = %q, want %q", test.filePath, registry.Version, test.want.Version)
			}
			if len(registry.Servers) != len(test.want.Servers) {
				t.Fatalf("LoadFromFile(%q) servers length = %d, want %d", test.filePath, len(registry.Servers), len(test.want.Servers))
			}
			for i := range registry.Servers {
				got := registry.Servers[i]
				want := test.want.Servers[i]
				if got.Name != want.Name {
					t.Errorf("server[%d].Name = %q, want %q", i, got.Name, want.Name)
				}
				if got.Image != want.Image {
					t.Errorf("server[%d].Image = %q, want %q", i, got.Image, want.Image)
				}
				if got.ImageTag != want.ImageTag {
					t.Errorf("server[%d].ImageTag = %q, want %q", i, got.ImageTag, want.ImageTag)
				}
				if got.Route != want.Route {
					t.Errorf("server[%d].Route = %q, want %q", i, got.Route, want.Route)
				}
				if got.Port != want.Port {
					t.Errorf("server[%d].Port = %d, want %d", i, got.Port, want.Port)
				}
				if !int32PtrEqual(got.Replicas, want.Replicas) {
					t.Errorf("server[%d].Replicas = %v, want %v", i, got.Replicas, want.Replicas)
				}
				if got.Namespace != want.Namespace {
					t.Errorf("server[%d].Namespace = %q, want %q", i, got.Namespace, want.Namespace)
				}
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	tests := []struct {
		name   string
		server *ServerMetadata
		want   *ServerMetadata
	}{
		{
			name: "apply-defaults",
			server: &ServerMetadata{
				Name:  "test-server",
				Image: "test-image",
			},
			want: &ServerMetadata{
				Name:      "test-server",
				Image:     "test-image",
				ImageTag:  "latest",
				Route:     "/test-server/mcp",
				Port:      8088,
				Replicas:  int32Ptr(1),
				Namespace: "mcp-servers",
			},
		},
		{
			name: "test-server",
			server: &ServerMetadata{
				Name:      "test-server",
				Image:     "test-image",
				ImageTag:  "latest",
				Route:     "/test-server/mcp",
				Port:      8088,
				Replicas:  int32Ptr(1),
				Namespace: "mcp-servers",
			},
			want: &ServerMetadata{
				Name:      "test-server",
				Image:     "test-image",
				ImageTag:  "latest",
				Route:     "/test-server/mcp",
				Port:      8088,
				Replicas:  int32Ptr(1),
				Namespace: "mcp-servers",
			},
		},
	}
	for _, test := range tests {
		setDefaults(test.server)
		if test.server.Name != test.want.Name {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.Name, test.server.Name, test.want.Name)
		}
		if test.server.Image != test.want.Image {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.Image, test.server.Image, test.want.Image)
		}
		if test.server.ImageTag != test.want.ImageTag {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.ImageTag, test.server.ImageTag, test.want.ImageTag)
		}
		if test.server.Route != test.want.Route {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.Route, test.server.Route, test.want.Route)
		}
		if test.server.Port != test.want.Port {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.Port, test.server.Port, test.want.Port)
		}
		if !int32PtrEqual(test.server.Replicas, test.want.Replicas) {
			t.Errorf("setDefaults Replicas = %v, want %v", test.server.Replicas, test.want.Replicas)
		}
		if test.server.Namespace != test.want.Namespace {
			t.Errorf("setDefaults(%q) = %q, want %q", test.server.Namespace, test.server.Namespace, test.want.Namespace)
		}
	}
}

func TestLoadFromDirectory(t *testing.T) {
	t.Run("loads_all_yaml_files_from_directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test YAML files
		file1 := filepath.Join(tmpDir, "server1.yaml")
		file2 := filepath.Join(tmpDir, "server2.yml")

		yaml1 := `version: v1
servers:
  - name: server1
    image: img1
`
		yaml2 := `version: v1
servers:
  - name: server2
    image: img2
`
		if err := os.WriteFile(file1, []byte(yaml1), 0644); err != nil {
			t.Fatalf("failed to write file1: %v", err)
		}
		if err := os.WriteFile(file2, []byte(yaml2), 0644); err != nil {
			t.Fatalf("failed to write file2: %v", err)
		}

		registry, err := LoadFromDirectory(tmpDir)
		if err != nil {
			t.Fatalf("LoadFromDirectory() unexpected error: %v", err)
		}

		if len(registry.Servers) != 2 {
			t.Errorf("LoadFromDirectory() servers = %d, want 2", len(registry.Servers))
		}

		// Check both servers are loaded (order may vary)
		names := make(map[string]bool)
		for _, s := range registry.Servers {
			names[s.Name] = true
		}
		if !names["server1"] || !names["server2"] {
			t.Errorf("LoadFromDirectory() missing servers, got names: %v", names)
		}
	})

	t.Run("returns_empty_registry_for_empty_directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		registry, err := LoadFromDirectory(tmpDir)
		if err != nil {
			t.Fatalf("LoadFromDirectory() unexpected error: %v", err)
		}

		if len(registry.Servers) != 0 {
			t.Errorf("LoadFromDirectory() servers = %d, want 0", len(registry.Servers))
		}
		if registry.Version != "v1" {
			t.Errorf("LoadFromDirectory() version = %q, want v1", registry.Version)
		}
	})

	t.Run("returns_error_for_invalid_yaml_in_directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		invalidFile := filepath.Join(tmpDir, "invalid.yaml")
		if err := os.WriteFile(invalidFile, []byte("{{invalid yaml"), 0644); err != nil {
			t.Fatalf("failed to write invalid file: %v", err)
		}

		_, err := LoadFromDirectory(tmpDir)
		if err == nil {
			t.Fatal("LoadFromDirectory() expected error for invalid YAML, got nil")
		}
	})

	t.Run("ignores_non_yaml_files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a YAML file and a non-YAML file
		yamlFile := filepath.Join(tmpDir, "server.yaml")
		txtFile := filepath.Join(tmpDir, "readme.txt")

		yaml := `version: v1
servers:
  - name: test-server
`
		if err := os.WriteFile(yamlFile, []byte(yaml), 0644); err != nil {
			t.Fatalf("failed to write yaml file: %v", err)
		}
		if err := os.WriteFile(txtFile, []byte("not yaml"), 0644); err != nil {
			t.Fatalf("failed to write txt file: %v", err)
		}

		registry, err := LoadFromDirectory(tmpDir)
		if err != nil {
			t.Fatalf("LoadFromDirectory() unexpected error: %v", err)
		}

		if len(registry.Servers) != 1 {
			t.Errorf("LoadFromDirectory() servers = %d, want 1", len(registry.Servers))
		}
	})
}

func int32Ptr(i int32) *int32 {
	return &i
}

func int32PtrEqual(a, b *int32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
