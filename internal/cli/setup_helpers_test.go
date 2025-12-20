package cli

import (
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestGetOperatorImage(t *testing.T) {
	origOverride := DefaultCLIConfig.OperatorImage
	origKubectl := kubectlClient
	t.Cleanup(func() {
		DefaultCLIConfig.OperatorImage = origOverride
		kubectlClient = origKubectl
	})

	t.Run("uses override when set", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = "override/operator:v1"
		got := getOperatorImage(nil, false)
		if got != "override/operator:v1" {
			t.Fatalf("expected override image, got %q", got)
		}
	})

	t.Run("uses test mode image", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = ""
		got := getOperatorImage(nil, true)
		if got != "docker.io/library/mcp-runtime-operator:latest" {
			t.Fatalf("unexpected test mode image: %q", got)
		}
	})

	t.Run("uses external registry URL", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = ""
		ext := &ExternalRegistryConfig{URL: "registry.example.com/"}
		got := getOperatorImage(ext, false)
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
		got := getOperatorImage(nil, false)
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
