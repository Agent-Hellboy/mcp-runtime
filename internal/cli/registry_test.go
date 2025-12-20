package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewRegistryCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewRegistryCmd(logger)

	t.Run("command-created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewRegistryCmd should not return nil")
		}
		if cmd.Use != "registry" {
			t.Errorf("expected Use='registry', got %q", cmd.Use)
		}
	})

	t.Run("has-subcommands", func(t *testing.T) {
		subcommands := cmd.Commands()
		expectedSubs := []string{"status", "info", "provision", "push"}
		if len(subcommands) < len(expectedSubs) {
			t.Errorf("expected at least %d subcommands, got %d", len(expectedSubs), len(subcommands))
		}
	})
}

func TestRegistryManager_CheckRegistryStatus(t *testing.T) {
	t.Run("returns error when deployment not found", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultErr: errors.New("not found"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.CheckRegistryStatus("registry")
		if err == nil {
			t.Fatal("expected error when registry not found")
		}
	})

	t.Run("calls kubectl get deployment", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultOutput: []byte("1"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		_ = mgr.CheckRegistryStatus("registry")

		if !mock.HasCommand("kubectl") {
			t.Error("expected kubectl to be called")
		}

		// Should query deployment status
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "get") && contains(cmd.Args, "deployment") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected kubectl get deployment to be called")
		}
	})
}

func TestRegistryManager_LoginRegistry(t *testing.T) {
	t.Run("calls docker login", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.LoginRegistry("localhost:5000", "user", "pass")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("docker") {
			t.Error("expected docker to be called")
		}

		// Check docker login args
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" && contains(cmd.Args, "login") {
				found = true
				if !contains(cmd.Args, "localhost:5000") {
					t.Errorf("expected registry URL in args, got %v", cmd.Args)
				}
				break
			}
		}
		if !found {
			t.Error("expected docker login to be called")
		}
	})
}

func TestRegistryManager_PushDirect(t *testing.T) {
	t.Run("calls docker tag and push", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushDirect("source:tag", "target:tag")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mock.HasCommand("docker") {
			t.Error("expected docker to be called")
		}

		// Should call docker tag first, then docker push
		tagFound := false
		pushFound := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" && contains(cmd.Args, "tag") {
				tagFound = true
			}
			if cmd.Name == "docker" && contains(cmd.Args, "push") {
				pushFound = true
			}
		}
		if !tagFound {
			t.Error("expected docker tag to be called")
		}
		if !pushFound {
			t.Error("expected docker push to be called")
		}
	})
}

// Helper functions for image parsing
func TestSplitImage(t *testing.T) {
	tests := []struct {
		image string
		want  string
		tag   string
	}{
		{"registry.example.com/example-mcp-server:latest", "registry.example.com/example-mcp-server", "latest"},
		{"registry.example.com/example-mcp-server", "registry.example.com/example-mcp-server", ""},
		{"example-mcp-server:latest", "example-mcp-server", "latest"},
		{"example-mcp-server", "example-mcp-server", ""},
	}
	for _, test := range tests {
		image, tag := splitImage(test.image)
		if image != test.want {
			t.Errorf("SplitImage(%q) = %q, want %q", test.image, image, test.want)
		}
		if tag != test.tag {
			t.Errorf("SplitImage(%q) tag = %q, want %q", test.image, tag, test.tag)
		}
	}
}

func TestDropRegistryPrefix(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"registry.example.com/example-mcp-server", "example-mcp-server"},
		{"example-mcp-server", "example-mcp-server"},
		{"localhost:5000/my-image", "my-image"},
		{"192.168.1.1:5000/my-image", "my-image"},
		{"my-image", "my-image"},
	}
	for _, test := range tests {
		repo := dropRegistryPrefix(test.repo)
		if repo != test.want {
			t.Errorf("dropRegistryPrefix(%q) = %q, want %q", test.repo, repo, test.want)
		}
	}
}

func TestRegistryConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := registryConfigPath()
	if err != nil {
		t.Fatalf("registryConfigPath returned error: %v", err)
	}
	expectedSuffix := filepath.Join(".mcp-runtime", "registry.yaml")
	if !strings.HasSuffix(path, expectedSuffix) {
		t.Fatalf("expected path to end with %q, got %q", expectedSuffix, path)
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("expected path to start with home %q, got %q", home, path)
	}
}

func TestSaveAndLoadExternalRegistryConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &ExternalRegistryConfig{
		URL:      "registry.example.com",
		Username: "user",
		Password: "pass",
	}
	if err := saveExternalRegistryConfig(cfg); err != nil {
		t.Fatalf("saveExternalRegistryConfig returned error: %v", err)
	}

	loaded, err := loadExternalRegistryConfig()
	if err != nil {
		t.Fatalf("loadExternalRegistryConfig returned error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected config to be loaded")
	}
	if loaded.URL != cfg.URL || loaded.Username != cfg.Username || loaded.Password != cfg.Password {
		t.Fatalf("loaded config mismatch: %#v", loaded)
	}
}

func TestLoadExternalRegistryConfigMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := loadExternalRegistryConfig()
	if err != nil {
		t.Fatalf("loadExternalRegistryConfig returned error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when file missing, got %#v", cfg)
	}
}

func TestLoadExternalRegistryConfigInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := registryConfigPath()
	if err != nil {
		t.Fatalf("registryConfigPath returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("username: user\n"), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if _, err := loadExternalRegistryConfig(); err == nil {
		t.Fatal("expected error for config missing url")
	}
}

func TestResolveExternalRegistryConfigPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	origConfig := DefaultCLIConfig
	t.Cleanup(func() { DefaultCLIConfig = origConfig })

	if err := saveExternalRegistryConfig(&ExternalRegistryConfig{URL: "file.example.com"}); err != nil {
		t.Fatalf("failed to save file config: %v", err)
	}

	t.Run("uses file config when no overrides", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{}
		cfg, err := resolveExternalRegistryConfig(nil)
		if err != nil {
			t.Fatalf("resolveExternalRegistryConfig returned error: %v", err)
		}
		if cfg == nil || cfg.URL != "file.example.com" {
			t.Fatalf("expected file config, got %#v", cfg)
		}
	})

	t.Run("env config overrides file", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{
			ProvisionedRegistryURL:      "env.example.com",
			ProvisionedRegistryUsername: "env-user",
			ProvisionedRegistryPassword: "env-pass",
		}
		cfg, err := resolveExternalRegistryConfig(nil)
		if err != nil {
			t.Fatalf("resolveExternalRegistryConfig returned error: %v", err)
		}
		if cfg == nil || cfg.URL != "env.example.com" || cfg.Username != "env-user" || cfg.Password != "env-pass" {
			t.Fatalf("expected env config, got %#v", cfg)
		}
	})

	t.Run("flag config overrides env", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{
			ProvisionedRegistryURL:      "env.example.com",
			ProvisionedRegistryUsername: "env-user",
			ProvisionedRegistryPassword: "env-pass",
		}
		cfg, err := resolveExternalRegistryConfig(&ExternalRegistryConfig{
			URL:      "flag.example.com",
			Username: "flag-user",
			Password: "flag-pass",
		})
		if err != nil {
			t.Fatalf("resolveExternalRegistryConfig returned error: %v", err)
		}
		if cfg == nil || cfg.URL != "flag.example.com" || cfg.Username != "flag-user" || cfg.Password != "flag-pass" {
			t.Fatalf("expected flag config, got %#v", cfg)
		}
	})
}

func TestEnsureRegistryStorageSize(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	t.Run("skips when size empty", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		if err := ensureRegistryStorageSize(zap.NewNop(), "registry", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 0 {
			t.Fatalf("expected no kubectl calls, got %v", mock.Commands)
		}
	})

	t.Run("no-op when size matches", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "get") && contains(spec.Args, "pvc") {
					cmd.RunFunc = func() error {
						if cmd.StdoutW != nil {
							_, _ = cmd.StdoutW.Write([]byte("10Gi"))
						}
						return nil
					}
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		if err := ensureRegistryStorageSize(zap.NewNop(), "registry", "10Gi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 1 {
			t.Fatalf("expected 1 kubectl call, got %d", len(mock.Commands))
		}
		if contains(mock.Commands[0].Args, "patch") {
			t.Fatalf("did not expect patch call")
		}
	})

	t.Run("patches when size differs", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "get") && contains(spec.Args, "pvc") {
					cmd.RunFunc = func() error {
						if cmd.StdoutW != nil {
							_, _ = cmd.StdoutW.Write([]byte("5Gi"))
						}
						return nil
					}
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		if err := ensureRegistryStorageSize(zap.NewNop(), "registry", "10Gi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 2 {
			t.Fatalf("expected 2 kubectl calls, got %d", len(mock.Commands))
		}
		foundPatch := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "patch") {
				foundPatch = true
				break
			}
		}
		if !foundPatch {
			t.Fatalf("expected patch command, got %v", mock.Commands)
		}
	})
}
