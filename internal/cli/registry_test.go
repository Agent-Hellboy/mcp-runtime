package cli

import (
	"bytes"
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

	t.Run("returns error when get pvc fails", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "get") && contains(spec.Args, "pvc") {
					cmd.RunErr = errors.New("pvc not found")
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		err := ensureRegistryStorageSize(zap.NewNop(), "registry", "10Gi")
		if err == nil {
			t.Fatal("expected error when get pvc fails")
		}
	})

	t.Run("returns error when patch fails", func(t *testing.T) {
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
				} else if contains(spec.Args, "patch") {
					cmd.RunErr = errors.New("patch failed")
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		err := ensureRegistryStorageSize(zap.NewNop(), "registry", "10Gi")
		if err == nil {
			t.Fatal("expected error when patch fails")
		}
	})
}

func TestRegistryStatusCmdRunE(t *testing.T) {
	mock := &MockExecutor{
		DefaultOutput: []byte("1/1"),
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := mgr.newRegistryStatusCmd()
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryInfoCmdRunE(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if contains(spec.Args, "clusterIP") {
				cmd.OutputData = []byte("10.0.0.1")
			} else if contains(spec.Args, "ports") {
				cmd.OutputData = []byte("5000")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := mgr.newRegistryInfoCmd()
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryProvisionCmdRunE(t *testing.T) {
	t.Run("requires url", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{}

		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryProvisionCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected error when url missing")
		}
		if !strings.Contains(err.Error(), "url is required") {
			t.Fatalf("expected url required error, got: %v", err)
		}
	})

	t.Run("saves config and logs in with credentials", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{}

		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryProvisionCmd()
		_ = cmd.Flags().Set("url", "registry.example.com")
		_ = cmd.Flags().Set("username", "user")
		_ = cmd.Flags().Set("password", "pass")

		err := cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have called docker login
		if !mock.HasCommand("docker") {
			t.Error("expected docker login to be called")
		}
	})

	t.Run("handles login error", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{}

		mock := &MockExecutor{DefaultRunErr: errors.New("login failed")}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryProvisionCmd()
		_ = cmd.Flags().Set("url", "registry.example.com")
		_ = cmd.Flags().Set("username", "user")
		_ = cmd.Flags().Set("password", "pass")

		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected error when login fails")
		}
	})
}

func TestRegistryPushCmdRunE(t *testing.T) {
	t.Run("requires image", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryPushCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected error when image missing")
		}
	})

	t.Run("uses external registry config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{}

		if err := saveExternalRegistryConfig(&ExternalRegistryConfig{URL: "registry.example.com"}); err != nil {
			t.Fatal(err)
		}

		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		cmd := mgr.newRegistryPushCmd()
		_ = cmd.Flags().Set("image", "my-image:latest")
		_ = cmd.Flags().Set("mode", "direct")

		err := cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("uses name override", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		cmd := mgr.newRegistryPushCmd()
		_ = cmd.Flags().Set("image", "my-image:latest")
		_ = cmd.Flags().Set("registry", "localhost:5000")
		_ = cmd.Flags().Set("name", "custom-name")
		_ = cmd.Flags().Set("mode", "direct")

		err := cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check that custom name was used
		found := false
		for _, cmd := range mock.Commands {
			if cmd.Name == "docker" && contains(cmd.Args, "tag") {
				for _, arg := range cmd.Args {
					if strings.Contains(arg, "custom-name") {
						found = true
						break
					}
				}
			}
		}
		if !found {
			t.Error("expected custom name in tag command")
		}
	})

	t.Run("rejects unknown mode", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryPushCmd()
		_ = cmd.Flags().Set("image", "my-image:latest")
		_ = cmd.Flags().Set("registry", "localhost:5000")
		_ = cmd.Flags().Set("mode", "unknown")

		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected error for unknown mode")
		}
		if !strings.Contains(err.Error(), "unknown mode") {
			t.Fatalf("expected unknown mode error, got: %v", err)
		}
	})

	t.Run("uses in-cluster mode with namespace error", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				// Fail on namespace check
				if spec.Name == "kubectl" && contains(spec.Args, "get") && contains(spec.Args, "namespace") {
					cmd.RunErr = errors.New("namespace not found")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		cmd := mgr.newRegistryPushCmd()
		_ = cmd.Flags().Set("image", "my-image:latest")
		_ = cmd.Flags().Set("registry", "localhost:5000")
		_ = cmd.Flags().Set("mode", "in-cluster")

		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected error for missing namespace")
		}
	})
}

func TestShowRegistryInfo(t *testing.T) {
	t.Run("displays registry info when available", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "jsonpath={.spec.clusterIP}") {
					cmd.OutputData = []byte("10.0.0.1")
				} else if contains(spec.Args, "jsonpath={.spec.ports[0].port}") {
					cmd.OutputData = []byte("5000")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ShowRegistryInfo()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "10.0.0.1") {
			t.Errorf("expected IP in output, got: %s", buf.String())
		}
	})

	t.Run("shows warning when registry not found", func(t *testing.T) {
		mock := &MockExecutor{
			DefaultErr: errors.New("not found"),
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.ShowRegistryInfo()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "Registry not found") {
			t.Errorf("expected warning message, got: %s", buf.String())
		}
	})
}

func TestLoginRegistryWrapper(t *testing.T) {
	origExecutor := execExecutor
	origKubectl := kubectlClient
	t.Cleanup(func() {
		execExecutor = origExecutor
		kubectlClient = origKubectl
	})

	mock := &MockExecutor{}
	execExecutor = mock
	kubectlClient = &KubectlClient{exec: mock, validators: nil}

	err := loginRegistry(zap.NewNop(), "localhost:5000", "user", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.HasCommand("docker") {
		t.Error("expected docker login to be called")
	}
}

func TestLoginRegistryError(t *testing.T) {
	mock := &MockExecutor{DefaultRunErr: errors.New("login failed")}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

	err := mgr.LoginRegistry("localhost:5000", "user", "pass")
	if err == nil {
		t.Fatal("expected error when login fails")
	}
}

func TestPushDirectErrors(t *testing.T) {
	t.Run("returns error when tag fails", func(t *testing.T) {
		mock := &MockExecutor{DefaultRunErr: errors.New("tag failed")}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushDirect("source:tag", "target:tag")
		if err == nil {
			t.Fatal("expected error when tag fails")
		}
	})

	t.Run("returns error when push fails", func(t *testing.T) {
		callCount := 0
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				callCount++
				cmd := &MockCommand{Args: spec.Args}
				if callCount > 1 { // First call is tag, second is push
					cmd.RunErr = errors.New("push failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushDirect("source:tag", "target:tag")
		if err == nil {
			t.Fatal("expected error when push fails")
		}
	})
}

func TestPushInCluster(t *testing.T) {
	t.Run("returns error when namespace not found", func(t *testing.T) {
		mock := &MockExecutor{DefaultRunErr: errors.New("namespace not found")}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "missing-ns")
		if err == nil {
			t.Fatal("expected error when namespace not found")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected namespace not found error, got: %v", err)
		}
	})

	t.Run("returns error when docker save fails", func(t *testing.T) {
		callCount := 0
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				callCount++
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "docker" && contains(spec.Args, "save") {
					cmd.RunErr = errors.New("save failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err == nil {
			t.Fatal("expected error when save fails")
		}
	})

	t.Run("returns error when run helper pod fails", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "run") {
					cmd.RunErr = errors.New("run failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err == nil {
			t.Fatal("expected error when run helper fails")
		}
	})

	t.Run("returns error when wait fails", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "wait") {
					cmd.RunErr = errors.New("wait failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err == nil {
			t.Fatal("expected error when wait fails")
		}
	})

	t.Run("returns error when cp fails", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "cp") {
					cmd.RunErr = errors.New("cp failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err == nil {
			t.Fatal("expected error when cp fails")
		}
	})

	t.Run("returns error when exec skopeo fails", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "exec") {
					cmd.RunErr = errors.New("exec failed")
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err == nil {
			t.Fatal("expected error when exec fails")
		}
	})

	t.Run("succeeds and cleans up helper pod", func(t *testing.T) {
		deleteCalled := false
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "delete") {
					deleteCalled = true
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

		var buf bytes.Buffer
		setDefaultPrinterWriter(t, &buf)

		err := mgr.PushInCluster("source:tag", "target:tag", "registry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !deleteCalled {
			t.Error("expected delete to be called for cleanup")
		}
	})
}

func TestSaveExternalRegistryConfigErrors(t *testing.T) {
	t.Run("rejects nil config", func(t *testing.T) {
		err := saveExternalRegistryConfig(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("rejects empty url", func(t *testing.T) {
		err := saveExternalRegistryConfig(&ExternalRegistryConfig{})
		if err == nil {
			t.Fatal("expected error for empty url")
		}
	})
}

func TestLoadExternalRegistryConfigYAMLError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := registryConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	// Write invalid YAML
	if err := os.WriteFile(path, []byte(":::invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = loadExternalRegistryConfig()
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestResolveExternalRegistryConfigErrors(t *testing.T) {
	t.Run("returns nil when no source found", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{}

		cfg, err := resolveExternalRegistryConfig(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg != nil {
			t.Fatalf("expected nil config, got: %#v", cfg)
		}
	})

	t.Run("returns error when source found but no url", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		origConfig := DefaultCLIConfig
		t.Cleanup(func() { DefaultCLIConfig = origConfig })
		DefaultCLIConfig = &CLIConfig{
			ProvisionedRegistryUsername: "user", // Has username but no url
		}

		_, err := resolveExternalRegistryConfig(nil)
		if err == nil {
			t.Fatal("expected error when source found but url missing")
		}
	})
}

func TestDeployRegistry(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	t.Run("defaults to docker registry type", func(t *testing.T) {
		origKubectl := kubectlClient
		t.Cleanup(func() { kubectlClient = origKubectl })

		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "get") &&
					contains(spec.Args, "deployment") &&
					contains(spec.Args, "jsonpath={.status.availableReplicas}") {
					cmd.OutputData = []byte("1")
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		// Create temp manifest dir
		tmpDir := t.TempDir()
		manifestPath := filepath.Join(tmpDir, "registry")
		if err := os.MkdirAll(manifestPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestPath, "kustomization.yaml"), []byte("resources: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := deployRegistry(zap.NewNop(), "registry", 5000, "", "", manifestPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unsupported registry type", func(t *testing.T) {
		origKubectl := kubectlClient
		t.Cleanup(func() { kubectlClient = origKubectl })

		mock := &MockExecutor{}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		err := deployRegistry(zap.NewNop(), "registry", 5000, "harbor", "", "")
		if err == nil {
			t.Fatal("expected error for unsupported registry type")
		}
		if !strings.Contains(err.Error(), "unsupported registry type") {
			t.Fatalf("expected unsupported registry type error, got: %v", err)
		}
	})

	t.Run("returns error when ensure namespace fails", func(t *testing.T) {
		origKubectl := kubectlClient
		t.Cleanup(func() { kubectlClient = origKubectl })

		mock := &MockExecutor{DefaultRunErr: errors.New("namespace failed")}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		err := deployRegistry(zap.NewNop(), "registry", 5000, "docker", "", "config/registry")
		if err == nil {
			t.Fatal("expected error when namespace fails")
		}
	})

	t.Run("returns error when apply fails", func(t *testing.T) {
		origKubectl := kubectlClient
		t.Cleanup(func() { kubectlClient = origKubectl })

		callCount := 0
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				callCount++
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "apply") && contains(spec.Args, "-k") {
					cmd.RunErr = errors.New("apply failed")
				}
				return cmd
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}

		err := deployRegistry(zap.NewNop(), "registry", 5000, "docker", "", "config/registry")
		if err == nil {
			t.Fatal("expected error when apply fails")
		}
	})
}

func TestCheckRegistryStatusStarting(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if contains(spec.Args, "deployment") {
				cmd.OutputData = []byte("0/1") // Starting state
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	err := mgr.CheckRegistryStatus("registry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should show "Starting" status for 0/1 replicas
}

func TestDropRegistryPrefixMoreCases(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"user/repo", "user/repo"}, // user/repo should NOT be stripped
		{"gcr.io/project/image", "project/image"},
		{"docker.io/library/nginx", "library/nginx"},
	}
	for _, test := range tests {
		repo := dropRegistryPrefix(test.repo)
		if repo != test.want {
			t.Errorf("dropRegistryPrefix(%q) = %q, want %q", test.repo, repo, test.want)
		}
	}
}

func TestRegistryProvisionCmdWithOperatorImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	origConfig := DefaultCLIConfig
	origExecutor := execExecutor
	t.Cleanup(func() {
		DefaultCLIConfig = origConfig
		execExecutor = origExecutor
	})
	DefaultCLIConfig = &CLIConfig{}

	// Mock executor that returns error for make command (build step)
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if spec.Name == "make" {
				cmd.RunErr = errors.New("make build failed")
			}
			return cmd
		},
	}
	execExecutor = mock
	kubectl := &KubectlClient{exec: mock, validators: nil}
	mgr := NewRegistryManager(kubectl, mock, zap.NewNop())

	cmd := mgr.newRegistryProvisionCmd()
	_ = cmd.Flags().Set("url", "registry.example.com")
	_ = cmd.Flags().Set("operator-image", "registry.example.com/operator:latest")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when build fails")
	}
	if !strings.Contains(err.Error(), "build") && !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected build error, got: %v", err)
	}
}
