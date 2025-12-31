package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

type helperFakeClusterManager struct{}

func (f *helperFakeClusterManager) InitCluster(_, _ string) error           { return nil }
func (f *helperFakeClusterManager) ConfigureCluster(_ ingressOptions) error { return nil }

type helperFakeRegistryManager struct{}

func (f *helperFakeRegistryManager) ShowRegistryInfo() error { return nil }
func (f *helperFakeRegistryManager) PushInCluster(_, _, _ string) error {
	return nil
}

func TestGetOperatorImage(t *testing.T) {
	origOverride := DefaultCLIConfig.OperatorImage
	origKubectl := kubectlClient
	t.Cleanup(func() {
		DefaultCLIConfig.OperatorImage = origOverride
		kubectlClient = origKubectl
	})

	t.Run("uses override when set", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = "override/operator:v1"
		got := getOperatorImage(nil)
		if got != "override/operator:v1" {
			t.Fatalf("expected override image, got %q", got)
		}
	})

	t.Run("uses external registry URL", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = ""
		ext := &ExternalRegistryConfig{URL: "registry.example.com/"}
		got := getOperatorImage(ext)
		if got != "registry.example.com/mcp-runtime-operator:latest" {
			t.Fatalf("unexpected external registry image: %q", got)
		}
	})

	t.Run("uses platform registry URL when external not set", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = ""
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if contains(spec.Args, "jsonpath={.spec.clusterIP}") {
					return &MockCommand{OutputData: []byte("10.0.0.1")}
				}
				if contains(spec.Args, "jsonpath={.spec.ports[0].port}") {
					return &MockCommand{OutputData: []byte("5000")}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}
		got := getOperatorImage(nil)
		if got != "10.0.0.1:5000/mcp-runtime-operator:latest" {
			t.Fatalf("unexpected platform registry image: %q", got)
		}
	})
}

func TestConfigureProvisionedRegistryEnv(t *testing.T) {
	t.Run("returns nil when registry not set", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}

		if err := configureProvisionedRegistryEnvWithKubectl(kubectl, nil, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) > 0 {
			t.Fatalf("expected no kubectl calls, got %v", mock.Commands)
		}
	})

	t.Run("sets URL only when no credentials", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		ext := &ExternalRegistryConfig{URL: "registry.example.com"}

		if err := configureProvisionedRegistryEnvWithKubectl(kubectl, ext, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 1 {
			t.Fatalf("expected 1 kubectl call, got %d", len(mock.Commands))
		}
		cmd := mock.LastCommand()
		if !contains(cmd.Args, "set") || !contains(cmd.Args, "env") || !contains(cmd.Args, "deployment/mcp-runtime-operator-controller-manager") {
			t.Fatalf("unexpected args: %v", cmd.Args)
		}
		if !contains(cmd.Args, "PROVISIONED_REGISTRY_URL=registry.example.com") {
			t.Fatalf("expected URL env in args: %v", cmd.Args)
		}
		if contains(cmd.Args, "PROVISIONED_REGISTRY_SECRET_NAME="+defaultRegistrySecretName) {
			t.Fatalf("did not expect secret name when no creds: %v", cmd.Args)
		}
	})

	t.Run("creates secrets and sets secret env when credentials provided", func(t *testing.T) {
		var envData string
		var applyInputs []string
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "create") && contains(spec.Args, "secret") {
					cmd.RunFunc = func() error {
						if cmd.StdinR != nil {
							data, _ := io.ReadAll(cmd.StdinR)
							envData = string(data)
						}
						if cmd.StdoutW != nil {
							_, _ = cmd.StdoutW.Write([]byte("apiVersion: v1\nkind: Secret\n"))
						}
						return nil
					}
				}
				if contains(spec.Args, "apply") && contains(spec.Args, "-f") && contains(spec.Args, "-") {
					cmd.RunFunc = func() error {
						if cmd.StdinR != nil {
							data, _ := io.ReadAll(cmd.StdinR)
							applyInputs = append(applyInputs, string(data))
						}
						return nil
					}
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		ext := &ExternalRegistryConfig{
			URL:      "registry.example.com",
			Username: "user",
			Password: "pass",
		}

		if err := configureProvisionedRegistryEnvWithKubectl(kubectl, ext, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 4 {
			t.Fatalf("expected 4 kubectl calls, got %d", len(mock.Commands))
		}
		if !strings.Contains(envData, "PROVISIONED_REGISTRY_USERNAME=user") || !strings.Contains(envData, "PROVISIONED_REGISTRY_PASSWORD=pass") {
			t.Fatalf("unexpected env data: %q", envData)
		}
		foundDockerConfig := false
		for _, input := range applyInputs {
			if strings.Contains(input, "kubernetes.io/dockerconfigjson") {
				foundDockerConfig = true
				break
			}
		}
		if !foundDockerConfig {
			t.Fatalf("expected dockerconfigjson secret manifest in apply inputs")
		}

		setEnv := mock.Commands[len(mock.Commands)-1]
		if !contains(setEnv.Args, "PROVISIONED_REGISTRY_SECRET_NAME="+defaultRegistrySecretName) {
			t.Fatalf("expected secret name env, got %v", setEnv.Args)
		}
		if !contains(setEnv.Args, "--from=secret/"+defaultRegistrySecretName) {
			t.Fatalf("expected from=secret arg, got %v", setEnv.Args)
		}
	})
}

func TestEnsureProvisionedRegistrySecret(t *testing.T) {
	t.Run("returns nil when no credentials", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}

		if err := ensureProvisionedRegistrySecretWithKubectl(kubectl, "name", "", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) > 0 {
			t.Fatalf("expected no kubectl calls, got %v", mock.Commands)
		}
	})

	t.Run("creates and applies secret with env data", func(t *testing.T) {
		var envData string
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "create") && contains(spec.Args, "secret") {
					cmd.RunFunc = func() error {
						if cmd.StdinR != nil {
							data, _ := io.ReadAll(cmd.StdinR)
							envData = string(data)
						}
						if cmd.StdoutW != nil {
							_, _ = cmd.StdoutW.Write([]byte("apiVersion: v1\nkind: Secret\n"))
						}
						return nil
					}
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}

		if err := ensureProvisionedRegistrySecretWithKubectl(kubectl, "custom-secret", "user", "pass"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 2 {
			t.Fatalf("expected 2 kubectl calls, got %d", len(mock.Commands))
		}
		if !strings.Contains(envData, "PROVISIONED_REGISTRY_USERNAME=user") || !strings.Contains(envData, "PROVISIONED_REGISTRY_PASSWORD=pass") {
			t.Fatalf("unexpected env data: %q", envData)
		}
	})
}

func TestEnsureImagePullSecret(t *testing.T) {
	t.Run("returns nil when no credentials", func(t *testing.T) {
		mock := &MockExecutor{}
		kubectl := &KubectlClient{exec: mock, validators: nil}

		if err := ensureImagePullSecretWithKubectl(kubectl, "ns", "name", "registry.example.com", "", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) > 0 {
			t.Fatalf("expected no kubectl calls, got %v", mock.Commands)
		}
	})

	t.Run("applies dockerconfigjson secret manifest", func(t *testing.T) {
		var manifest string
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				cmd := &MockCommand{Args: spec.Args}
				if contains(spec.Args, "apply") && contains(spec.Args, "-f") && contains(spec.Args, "-") {
					cmd.RunFunc = func() error {
						if cmd.StdinR != nil {
							data, _ := io.ReadAll(cmd.StdinR)
							manifest = string(data)
						}
						return nil
					}
				}
				return cmd
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}

		if err := ensureImagePullSecretWithKubectl(kubectl, "ns", "name", "registry.example.com", "user", "pass"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(manifest, "kubernetes.io/dockerconfigjson") || !strings.Contains(manifest, ".dockerconfigjson:") {
			t.Fatalf("unexpected secret manifest: %q", manifest)
		}

		var encoded string
		for _, line := range strings.Split(manifest, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, ".dockerconfigjson:") {
				encoded = strings.TrimSpace(strings.TrimPrefix(line, ".dockerconfigjson:"))
				break
			}
		}
		if encoded == "" {
			t.Fatalf("missing dockerconfigjson payload")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("failed to decode dockerconfigjson: %v", err)
		}
		if !strings.Contains(string(decoded), "registry.example.com") {
			t.Fatalf("decoded docker config missing registry: %s", string(decoded))
		}
	})
}

func TestSetupDepsWithDefaultsSetsNil(t *testing.T) {
	deps := SetupDeps{}.withDefaults(zap.NewNop())
	if deps.ResolveExternalRegistryConfig == nil {
		t.Fatal("expected ResolveExternalRegistryConfig default")
	}
	if deps.ClusterManager == nil {
		t.Fatal("expected ClusterManager default")
	}
	if deps.RegistryManager == nil {
		t.Fatal("expected RegistryManager default")
	}
	if deps.LoginRegistry == nil {
		t.Fatal("expected LoginRegistry default")
	}
	if deps.DeployRegistry == nil {
		t.Fatal("expected DeployRegistry default")
	}
	if deps.WaitForDeploymentAvailable == nil {
		t.Fatal("expected WaitForDeploymentAvailable default")
	}
	if deps.PrintDeploymentDiagnostics == nil {
		t.Fatal("expected PrintDeploymentDiagnostics default")
	}
	if deps.SetupTLS == nil {
		t.Fatal("expected SetupTLS default")
	}
	if deps.BuildOperatorImage == nil {
		t.Fatal("expected BuildOperatorImage default")
	}
	if deps.PushOperatorImage == nil {
		t.Fatal("expected PushOperatorImage default")
	}
	if deps.EnsureNamespace == nil {
		t.Fatal("expected EnsureNamespace default")
	}
	if deps.GetPlatformRegistryURL == nil {
		t.Fatal("expected GetPlatformRegistryURL default")
	}
	if deps.PushOperatorImageToInternal == nil {
		t.Fatal("expected PushOperatorImageToInternal default")
	}
	if deps.DeployOperatorManifests == nil {
		t.Fatal("expected DeployOperatorManifests default")
	}
	if deps.ConfigureProvisionedRegistryEnv == nil {
		t.Fatal("expected ConfigureProvisionedRegistryEnv default")
	}
	if deps.RestartDeployment == nil {
		t.Fatal("expected RestartDeployment default")
	}
	if deps.CheckCRDInstalled == nil {
		t.Fatal("expected CheckCRDInstalled default")
	}
	if deps.GetDeploymentTimeout == nil {
		t.Fatal("expected GetDeploymentTimeout default")
	}
	if deps.GetRegistryPort == nil {
		t.Fatal("expected GetRegistryPort default")
	}
	if deps.OperatorImageFor == nil {
		t.Fatal("expected OperatorImageFor default")
	}
}

func TestSetupDepsWithDefaultsPreservesNonNil(t *testing.T) {
	cluster := &helperFakeClusterManager{}
	registry := &helperFakeRegistryManager{}
	deps := SetupDeps{
		ClusterManager:  cluster,
		RegistryManager: registry,
		GetRegistryPort: func() int { return 123 },
		OperatorImageFor: func(_ *ExternalRegistryConfig) string {
			return "custom-image"
		},
	}

	got := deps.withDefaults(zap.NewNop())
	if got.ClusterManager != cluster {
		t.Fatal("expected ClusterManager to be preserved")
	}
	if got.RegistryManager != registry {
		t.Fatal("expected RegistryManager to be preserved")
	}
	if got.GetRegistryPort() != 123 {
		t.Fatal("expected GetRegistryPort to be preserved")
	}
	if got.OperatorImageFor(nil) != "custom-image" {
		t.Fatal("expected OperatorImageFor to be preserved")
	}
}

func TestCheckCRDInstalledWithKubectl(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := checkCRDInstalledWithKubectl(kubectl, "example.crd.io"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "crd", "example.crd.io") {
		t.Fatalf("unexpected command args: %v", mock.Commands[0].Args)
	}
}

func TestCheckCRDInstalledUsesDefaultKubectl(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{}
	kubectlClient = &KubectlClient{exec: mock, validators: nil}

	if err := checkCRDInstalled("example.crd.io"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "crd", "example.crd.io") {
		t.Fatalf("unexpected command args: %v", mock.Commands[0].Args)
	}
}

func TestCheckCRDInstalledWithKubectlError(t *testing.T) {
	mock := &MockExecutor{DefaultRunErr: errors.New("kubectl failed")}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := checkCRDInstalledWithKubectl(kubectl, "example.crd.io"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWaitForDeploymentAvailableWithKubectl(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			return &MockCommand{Args: spec.Args, OutputData: []byte("1")}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := waitForDeploymentAvailableWithKubectl(kubectl, zap.NewNop(), "registry", "registry", "app=registry", time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "deployment", "registry", "-n", "registry", "-o", "jsonpath={.status.availableReplicas}") {
		t.Fatalf("unexpected command args: %v", mock.Commands[0].Args)
	}
}

func TestWaitForDeploymentAvailableWithKubectlTimeout(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			return &MockCommand{Args: spec.Args, OutputData: []byte("0")}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := waitForDeploymentAvailableWithKubectl(kubectl, zap.NewNop(), "registry", "registry", "app=registry", -time.Second); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDeployOperatorManifestsWithKubectl(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	root := repoRootForTest(t)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	var managerManifest string
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if idx := argIndex(spec.Args, "-f"); idx != -1 && idx+1 < len(spec.Args) {
				path := spec.Args[idx+1]
				if strings.Contains(path, "manager-") && strings.HasSuffix(path, ".yaml") {
					cmd.RunFunc = func() error {
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						managerManifest = string(data)
						return nil
					}
				}
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	operatorImage := "registry.example.com/mcp-runtime-operator:dev"
	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), operatorImage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if managerManifest == "" {
		t.Fatal("expected manager manifest to be captured")
	}
	if !strings.Contains(managerManifest, "image: "+operatorImage) {
		t.Fatalf("expected manager manifest to include image %q", operatorImage)
	}

	var (
		hasCRD          bool
		hasRBAC         bool
		hasDelete       bool
		hasManagerApply bool
		hasNamespace    bool
	)
	for _, cmd := range mock.Commands {
		if commandHasArgs(cmd, "apply", "--validate=false", "-f", "config/crd/bases/mcpruntime.org_mcpservers.yaml") {
			hasCRD = true
		}
		if commandHasArgs(cmd, "apply", "-k", "config/rbac/") {
			hasRBAC = true
		}
		if commandHasArgs(cmd, "delete", "deployment/"+OperatorDeploymentName, "-n", NamespaceMCPRuntime, "--ignore-not-found") {
			hasDelete = true
		}
		if idx := argIndex(cmd.Args, "-f"); idx != -1 && idx+1 < len(cmd.Args) {
			path := cmd.Args[idx+1]
			if strings.Contains(path, "manager-") && strings.HasSuffix(path, ".yaml") {
				hasManagerApply = true
			}
			if path == "-" {
				hasNamespace = true
			}
		}
	}
	if !hasCRD || !hasRBAC || !hasDelete || !hasManagerApply || !hasNamespace {
		t.Fatalf("missing expected kubectl commands: crd=%t rbac=%t delete=%t manager=%t namespace=%t", hasCRD, hasRBAC, hasDelete, hasManagerApply, hasNamespace)
	}
}

func TestDeployOperatorManifestsWithKubectlCRDError(t *testing.T) {
	mockErr := errors.New("apply crd failed")
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "--validate=false", "-f", "config/crd/bases/mcpruntime.org_mcpservers.yaml") {
				cmd.RunErr = mockErr
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeployOperatorManifestsWithKubectlRBACError(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mockErr := errors.New("apply rbac failed")
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-k", "config/rbac/") {
				cmd.RunErr = mockErr
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeployOperatorManifestsWithKubectlManagerApplyError(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	root := repoRootForTest(t)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	mockErr := errors.New("apply manager failed")
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if idx := argIndex(spec.Args, "-f"); idx != -1 && idx+1 < len(spec.Args) {
				path := spec.Args[idx+1]
				if strings.Contains(path, "manager-") && strings.HasSuffix(path, ".yaml") {
					cmd.RunErr = mockErr
				}
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetupTLSWithKubectl(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	if err := setupTLSWithKubectl(kubectl, zap.NewNop()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	timeoutArg := fmt.Sprintf("--timeout=%s", GetCertTimeout())
	var (
		hasCRD       bool
		hasSecret    bool
		hasIssuer    bool
		hasNamespace bool
		hasCert      bool
		hasWait      bool
	)
	for _, cmd := range mock.Commands {
		if commandHasArgs(cmd, "get", "crd", CertManagerCRDName) {
			hasCRD = true
		}
		if commandHasArgs(cmd, "get", "secret", "mcp-runtime-ca", "-n", "cert-manager") {
			hasSecret = true
		}
		if commandHasArgs(cmd, "apply", "-f", "config/cert-manager/cluster-issuer.yaml") {
			hasIssuer = true
		}
		if commandHasArgs(cmd, "apply", "-f", "-") {
			hasNamespace = true
		}
		if commandHasArgs(cmd, "apply", "-f", "config/cert-manager/example-registry-certificate.yaml") {
			hasCert = true
		}
		if commandHasArgs(cmd, "wait", "--for=condition=Ready", "certificate/registry-cert", "-n", NamespaceRegistry, timeoutArg) {
			hasWait = true
		}
	}
	if !hasCRD || !hasSecret || !hasIssuer || !hasNamespace || !hasCert || !hasWait {
		t.Fatalf("missing expected kubectl commands: crd=%t secret=%t issuer=%t namespace=%t cert=%t wait=%t", hasCRD, hasSecret, hasIssuer, hasNamespace, hasCert, hasWait)
	}
}

func TestSetupTLSWithKubectlMissingCRD(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "crd", CertManagerCRDName) {
				cmd.RunErr = errors.New("missing crd")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := setupTLSWithKubectl(kubectl, zap.NewNop()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetupTLSWithKubectlMissingSecret(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "secret", "mcp-runtime-ca", "-n", "cert-manager") {
				cmd.RunErr = errors.New("missing secret")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := setupTLSWithKubectl(kubectl, zap.NewNop()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetupTLSWithKubectlWaitError(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "wait", "--for=condition=Ready", "certificate/registry-cert", "-n", NamespaceRegistry) {
				cmd.RunErr = errors.New("wait failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	if err := setupTLSWithKubectl(kubectl, zap.NewNop()); err == nil {
		t.Fatal("expected error")
	}
}

func commandHasArgs(cmd ExecSpec, args ...string) bool {
	for _, arg := range args {
		if !contains(cmd.Args, arg) {
			return false
		}
	}
	return true
}

func argIndex(args []string, target string) int {
	for i, arg := range args {
		if arg == target {
			return i
		}
	}
	return -1
}

func repoRootForTest(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
