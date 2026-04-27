package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type helperFakeClusterManager struct{}

func (f *helperFakeClusterManager) InitCluster(_, _ string) error           { return nil }
func (f *helperFakeClusterManager) ConfigureCluster(_ ingressOptions) error { return nil }

type helperFakeRegistryManager struct{}

func (f *helperFakeRegistryManager) ShowRegistryInfo() error { return nil }
func (f *helperFakeRegistryManager) PushInCluster(_, _, _ string) error {
	return nil
}

func secretStringDataFromManifest(t *testing.T, manifest string) map[string]string {
	t.Helper()
	var payload struct {
		StringData map[string]string `yaml:"stringData"`
	}
	if err := yaml.Unmarshal([]byte(manifest), &payload); err != nil {
		t.Fatalf("unmarshal secret manifest: %v", err)
	}
	if payload.StringData == nil {
		t.Fatalf("secret manifest missing stringData: %q", manifest)
	}
	return payload.StringData
}

func TestGetOperatorImage(t *testing.T) {
	origOverride := DefaultCLIConfig.OperatorImage
	origKubectl := kubectlClient
	origTagResolver := setupImageTagResolver
	t.Cleanup(func() {
		DefaultCLIConfig.OperatorImage = origOverride
		kubectlClient = origKubectl
		setupImageTagResolver = origTagResolver
	})

	t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
	setupImageTagResolver = func() string { return "deadbeef" }

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
				if contains(spec.Args, "jsonpath={.spec.ports[0].port}") {
					return &MockCommand{OutputData: []byte("5000")}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}
		got := getOperatorImage(nil)
		if got != "registry.registry.svc.cluster.local:5000/mcp-runtime-operator:latest" {
			t.Fatalf("unexpected platform registry image: %q", got)
		}
	})

	t.Run("uses versioned tag outside test mode", func(t *testing.T) {
		DefaultCLIConfig.OperatorImage = ""
		t.Setenv("MCP_RUNTIME_TEST_MODE", "")
		ext := &ExternalRegistryConfig{URL: "registry.example.com/"}
		got := getOperatorImage(ext)
		if got != "registry.example.com/mcp-runtime-operator:deadbeef" {
			t.Fatalf("unexpected versioned image: %q", got)
		}
	})
}

func TestGetGatewayProxyImage(t *testing.T) {
	origOverride := DefaultCLIConfig.GatewayProxyImage
	origKubectl := kubectlClient
	origTagResolver := setupImageTagResolver
	t.Cleanup(func() {
		DefaultCLIConfig.GatewayProxyImage = origOverride
		kubectlClient = origKubectl
		setupImageTagResolver = origTagResolver
	})

	t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
	setupImageTagResolver = func() string { return "deadbeef" }

	t.Run("uses override when set", func(t *testing.T) {
		DefaultCLIConfig.GatewayProxyImage = "override/mcp-proxy:v1"
		got := getGatewayProxyImage(nil)
		if got != "override/mcp-proxy:v1" {
			t.Fatalf("expected override image, got %q", got)
		}
	})

	t.Run("uses external registry URL", func(t *testing.T) {
		DefaultCLIConfig.GatewayProxyImage = ""
		ext := &ExternalRegistryConfig{URL: "registry.example.com/"}
		got := getGatewayProxyImage(ext)
		if got != "registry.example.com/mcp-sentinel-mcp-proxy:latest" {
			t.Fatalf("unexpected external registry image: %q", got)
		}
	})

	t.Run("uses platform registry URL when external not set", func(t *testing.T) {
		DefaultCLIConfig.GatewayProxyImage = ""
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if contains(spec.Args, "jsonpath={.spec.ports[0].port}") {
					return &MockCommand{OutputData: []byte("5000")}
				}
				return &MockCommand{}
			},
		}
		kubectlClient = &KubectlClient{exec: mock, validators: nil}
		got := getGatewayProxyImage(nil)
		if got != "registry.registry.svc.cluster.local:5000/mcp-sentinel-mcp-proxy:latest" {
			t.Fatalf("unexpected platform registry image: %q", got)
		}
	})

	t.Run("uses versioned tag outside test mode", func(t *testing.T) {
		DefaultCLIConfig.GatewayProxyImage = ""
		t.Setenv("MCP_RUNTIME_TEST_MODE", "")
		ext := &ExternalRegistryConfig{URL: "registry.example.com/"}
		got := getGatewayProxyImage(ext)
		if got != "registry.example.com/mcp-sentinel-mcp-proxy:deadbeef" {
			t.Fatalf("unexpected versioned image: %q", got)
		}
	})
}

func TestBuildOperatorArgs(t *testing.T) {
	t.Run("omits defaults", func(t *testing.T) {
		if got := buildOperatorArgs("", "", false, false); len(got) != 0 {
			t.Fatalf("expected no operator args, got %v", got)
		}
	})

	t.Run("includes explicit overrides", func(t *testing.T) {
		got := buildOperatorArgs(":9090", ":9091", false, true)
		want := []string{
			"--metrics-bind-address=:9090",
			"--health-probe-bind-address=:9091",
			"--leader-elect=false",
		}
		if len(got) != len(want) {
			t.Fatalf("expected %d args, got %d (%v)", len(want), len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("expected arg %d to be %q, got %q", i, want[i], got[i])
			}
		}
	})
}

func TestOperatorEnvOverrides(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() {
		DefaultCLIConfig = orig
	})

	t.Run("returns empty when no gateway override is set", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{}
		got := operatorEnvOverrides("")
		if len(got) != 1 {
			t.Fatalf("expected default analytics ingest env only, got %v", got)
		}
		if got[0].Name != "MCP_SENTINEL_INGEST_URL" || got[0].Value != defaultAnalyticsIngestURL {
			t.Fatalf("unexpected default env override: %+v", got[0])
		}
	})

	t.Run("returns gateway proxy image override", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{GatewayProxyImage: "example.com/mcp-proxy:latest"}
		got := operatorEnvOverrides("")
		if len(got) != 2 {
			t.Fatalf("expected gateway and analytics env overrides, got %d (%v)", len(got), got)
		}
		if got[0].Name != "MCP_GATEWAY_PROXY_IMAGE" || got[0].Value != "example.com/mcp-proxy:latest" {
			t.Fatalf("unexpected env override: %+v", got[0])
		}
		if got[1].Name != "MCP_SENTINEL_INGEST_URL" || got[1].Value != defaultAnalyticsIngestURL {
			t.Fatalf("unexpected analytics env override: %+v", got[1])
		}
	})

	t.Run("prefers explicit setup image over config override", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{
			GatewayProxyImage:  "example.com/mcp-proxy:config",
			AnalyticsIngestURL: "http://custom-analytics-ingest",
		}
		got := operatorEnvOverrides("example.com/mcp-proxy:setup")
		if len(got) != 2 {
			t.Fatalf("expected gateway and analytics env overrides, got %d (%v)", len(got), got)
		}
		if got[0].Value != "example.com/mcp-proxy:setup" {
			t.Fatalf("expected explicit setup image to win, got %+v", got[0])
		}
		if got[1].Name != "MCP_SENTINEL_INGEST_URL" || got[1].Value != "http://custom-analytics-ingest" {
			t.Fatalf("expected custom analytics env override, got %+v", got[1])
		}
	})

	t.Run("uses analytics ingest override when configured", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{AnalyticsIngestURL: "http://custom-analytics-ingest"}
		got := operatorEnvOverrides("")
		if len(got) != 1 {
			t.Fatalf("expected analytics ingest env only, got %d (%v)", len(got), got)
		}
		if got[0].Value != "http://custom-analytics-ingest" {
			t.Fatalf("expected custom ingest url, got %+v", got[0])
		}
	})

	t.Run("includes registry endpoint and ingress host when configured", func(t *testing.T) {
		DefaultCLIConfig = &CLIConfig{
			RegistryEndpoint:    "10.43.39.164:5000",
			RegistryIngressHost: "registry.local",
		}
		got := operatorEnvOverrides("")
		if len(got) != 3 {
			t.Fatalf("expected analytics plus registry env overrides, got %v", got)
		}
		if got[1].Name != "MCP_REGISTRY_ENDPOINT" || got[1].Value != "10.43.39.164:5000" {
			t.Fatalf("unexpected registry endpoint env override: %+v", got[1])
		}
		if got[2].Name != "MCP_REGISTRY_INGRESS_HOST" || got[2].Value != "registry.local" {
			t.Fatalf("unexpected registry ingress env override: %+v", got[2])
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

func TestEnsureAnalyticsImagePullSecret(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() {
		DefaultCLIConfig = orig
	})

	DefaultCLIConfig = &CLIConfig{
		ProvisionedRegistryURL:      "registry.example.com",
		ProvisionedRegistryUsername: "user",
		ProvisionedRegistryPassword: "pass",
	}

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

	secretName, err := ensureAnalyticsImagePullSecret(kubectl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secretName != defaultRegistrySecretName {
		t.Fatalf("expected secret name %q, got %q", defaultRegistrySecretName, secretName)
	}
	if !strings.Contains(manifest, "namespace: "+defaultAnalyticsNamespace) {
		t.Fatalf("expected analytics namespace in secret manifest, got %q", manifest)
	}
	if !strings.Contains(manifest, "kubernetes.io/dockerconfigjson") {
		t.Fatalf("expected dockerconfigjson secret manifest, got %q", manifest)
	}
}

func TestRenderAnalyticsSecretManifestReusesExistingPassword(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("keep-me"))
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			if contains(spec.Args, "get") && contains(spec.Args, "secret") {
				return &MockCommand{Args: spec.Args, OutputData: []byte(encoded)}
			}
			return &MockCommand{Args: spec.Args}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	manifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := secretStringDataFromManifest(t, manifest)
	if data["GRAFANA_ADMIN_PASSWORD"] != "keep-me" {
		t.Fatalf("expected existing grafana password to be reused, got %q", data["GRAFANA_ADMIN_PASSWORD"])
	}
}

func TestRenderAnalyticsSecretManifestReusesExistingAPIKeys(t *testing.T) {
	apiKeyEncoded := base64.StdEncoding.EncodeToString([]byte("api-key"))
	uiKeyEncoded := base64.StdEncoding.EncodeToString([]byte("ui-key"))
	passwordEncoded := base64.StdEncoding.EncodeToString([]byte("grafana-password"))
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			switch {
			case contains(spec.Args, "jsonpath={.data.API_KEYS}"):
				return &MockCommand{Args: spec.Args, OutputData: []byte(apiKeyEncoded)}
			case contains(spec.Args, "jsonpath={.data.UI_API_KEY}"):
				return &MockCommand{Args: spec.Args, OutputData: []byte(uiKeyEncoded)}
			case contains(spec.Args, "jsonpath={.data.GRAFANA_ADMIN_PASSWORD}"):
				return &MockCommand{Args: spec.Args, OutputData: []byte(passwordEncoded)}
			default:
				return &MockCommand{Args: spec.Args}
			}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	manifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := secretStringDataFromManifest(t, manifest)
	if data["API_KEYS"] != "api-key" {
		t.Fatalf("expected existing API key to be reused, got %q", data["API_KEYS"])
	}
	if data["UI_API_KEY"] != "ui-key" {
		t.Fatalf("expected existing UI API key to be reused, got %q", data["UI_API_KEY"])
	}
	if data["POSTGRES_USER"] != "mcp_runtime" {
		t.Fatalf("expected default postgres user to be rendered, got %q", data["POSTGRES_USER"])
	}
	if data["POSTGRES_DB"] != "mcp_runtime" {
		t.Fatalf("expected default postgres db to be rendered, got %q", data["POSTGRES_DB"])
	}
	if !strings.HasPrefix(data["POSTGRES_DSN"], "postgres://mcp_runtime:") {
		t.Fatalf("expected derived postgres DSN to be rendered, got %q", data["POSTGRES_DSN"])
	}
	if data["GRAFANA_ADMIN_PASSWORD"] != "grafana-password" {
		t.Fatalf("expected existing grafana password to be reused, got %q", data["GRAFANA_ADMIN_PASSWORD"])
	}
}

func TestRenderAnalyticsSecretManifestEscapesPostgresCredentialsInDSN(t *testing.T) {
	postgresUserEncoded := base64.StdEncoding.EncodeToString([]byte("user@runtime"))
	postgresPasswordEncoded := base64.StdEncoding.EncodeToString([]byte(`pa:ss?/#[%]`))
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			switch {
			case contains(spec.Args, "jsonpath={.data.POSTGRES_USER}"):
				return &MockCommand{Args: spec.Args, OutputData: []byte(postgresUserEncoded)}
			case contains(spec.Args, "jsonpath={.data.POSTGRES_PASSWORD}"):
				return &MockCommand{Args: spec.Args, OutputData: []byte(postgresPasswordEncoded)}
			default:
				return &MockCommand{Args: spec.Args}
			}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	manifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := secretStringDataFromManifest(t, manifest)

	encodedUserInfo := url.UserPassword("user@runtime", `pa:ss?/#[%]`).String()
	want := "postgres://" + encodedUserInfo + "@mcp-sentinel-postgres.mcp-sentinel.svc.cluster.local:5432/mcp_runtime?sslmode=disable"
	if data["POSTGRES_DSN"] != want {
		t.Fatalf("expected encoded postgres DSN %q, got %q", want, data["POSTGRES_DSN"])
	}
}

func TestRenderAnalyticsSecretManifestGeneratesKeysWhenMissing(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			if contains(spec.Args, "get") && contains(spec.Args, "secret") {
				return &MockCommand{
					Args:       spec.Args,
					OutputData: []byte("Error from server (NotFound): secrets \"mcp-sentinel-secrets\" not found"),
					OutputErr:  errors.New("not found"),
				}
			}
			return &MockCommand{Args: spec.Args}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	manifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := secretStringDataFromManifest(t, manifest)
	if data["API_KEYS"] == "" {
		t.Fatalf("expected generated API key, got %q", manifest)
	}
	if data["UI_API_KEY"] == "" {
		t.Fatalf("expected generated UI API key, got %q", manifest)
	}
	if data["GRAFANA_ADMIN_PASSWORD"] == "" {
		t.Fatalf("expected generated grafana password, got %q", manifest)
	}
	if data["POSTGRES_PASSWORD"] == "" {
		t.Fatalf("expected generated postgres password, got %q", manifest)
	}
	if data["POSTGRES_DSN"] == "" {
		t.Fatalf("expected generated postgres DSN, got %q", manifest)
	}
	if data["PLATFORM_JWT_SECRET"] == "" {
		t.Fatalf("expected generated platform jwt secret, got %q", manifest)
	}
}

func TestPrepareAnalyticsImagesUsesTestModeImageSet(t *testing.T) {
	// setupImageTag() reads MCP_RUNTIME_TEST_MODE, not the boolean testMode argument,
	// to decide between the "latest" tag and a git SHA. Opt into the test-mode tag
	// here so the expected ":latest" image refs line up.
	t.Setenv("MCP_RUNTIME_TEST_MODE", "1")

	var buildCalls int32
	var pushCalls int32
	deps := SetupDeps{
		BuildAnalyticsImage: func(string, string, string) error {
			atomic.AddInt32(&buildCalls, 1)
			return nil
		},
		PushAnalyticsImage: func(string) error {
			atomic.AddInt32(&pushCalls, 1)
			return nil
		},
	}

	got, err := prepareAnalyticsImages(zap.NewNop(), &ExternalRegistryConfig{URL: "registry.example.com"}, true, true, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := AnalyticsImageSet{
		Ingest:    "registry.example.com/mcp-sentinel-ingest:latest",
		API:       "registry.example.com/mcp-sentinel-api:latest",
		Processor: "registry.example.com/mcp-sentinel-processor:latest",
		UI:        "registry.example.com/mcp-sentinel-ui:latest",
	}
	if got != want {
		t.Fatalf("prepareAnalyticsImages() = %+v, want %+v", got, want)
	}
	if atomic.LoadInt32(&buildCalls) != int32(len(analyticsComponents)) {
		t.Fatalf("expected %d builds in test mode, got %d", len(analyticsComponents), buildCalls)
	}
	if atomic.LoadInt32(&pushCalls) != int32(len(analyticsComponents)) {
		t.Fatalf("expected %d pushes in test mode, got %d", len(analyticsComponents), pushCalls)
	}
}

func TestRenderAnalyticsManifestInjectsImagePullSecrets(t *testing.T) {
	content := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-sentinel-ingest
spec:
  template:
    spec:
      containers:
        - name: ingest
          image: mcp-sentinel-ingest:latest
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: promtail
spec:
  template:
    spec:
      containers:
        - name: promtail
          image: grafana/promtail:2.9.4
`

	rendered, err := renderAnalyticsManifest(content, AnalyticsImageSet{Ingest: "registry.example.com/mcp-sentinel-ingest:latest"}, defaultRegistrySecretName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rendered, "image: registry.example.com/mcp-sentinel-ingest:latest") {
		t.Fatalf("expected image replacement, got %s", rendered)
	}
	if !strings.Contains(rendered, "imagePullSecrets:") || !strings.Contains(rendered, "name: "+defaultRegistrySecretName) {
		t.Fatalf("expected injected imagePullSecrets, got %s", rendered)
	}
}

func TestDeployAnalyticsManifestsReturnsRolloutFailures(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() {
		DefaultCLIConfig = orig
	})
	DefaultCLIConfig = &CLIConfig{}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	root := t.TempDir()
	manifestDir := filepath.Join(root, "k8s")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o755); err != nil {
		t.Fatalf("failed to create services dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	manifestContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fixture\n  namespace: mcp-sentinel\n"
	for _, name := range []string{
		"00-namespace.yaml",
		"01-config.yaml",
		"03-clickhouse.yaml",
		"03-clickhouse-hostpath.yaml",
		"04-clickhouse-init.yaml",
		"05-kafka.yaml",
		"05-kafka-hostpath.yaml",
		"06-ingest.yaml",
		"07-processor.yaml",
		"08-api.yaml",
		"08-api-rbac.yaml",
		"09-ui.yaml",
		"10-gateway.yaml",
		"11-prometheus.yaml",
		"12-grafana.yaml",
		"15-otel-collector.yaml",
		"16-tempo.yaml",
		"17-loki.yaml",
		"18-promtail.yaml",
		"19-grafana-datasources.yaml",
		"20-postgres.yaml",
		"20-postgres-hostpath.yaml",
	} {
		if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(manifestContent), 0o644); err != nil {
			t.Fatalf("failed to write fixture manifest %s: %v", name, err)
		}
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir to fixture root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	var applied []string
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			switch {
			case contains(spec.Args, "apply") && contains(spec.Args, "-f"):
				for i := 0; i+1 < len(spec.Args); i++ {
					if spec.Args[i] == "-f" {
						applied = append(applied, spec.Args[i+1])
					}
				}
			case contains(spec.Args, "get") && contains(spec.Args, "secret"):
				cmd.OutputData = []byte("Error from server (NotFound): secrets \"mcp-sentinel-secrets\" not found")
				cmd.OutputErr = errors.New("not found")
			case contains(spec.Args, "rollout") && contains(spec.Args, "deployment/mcp-sentinel-api"):
				cmd.RunErr = errors.New("image pull failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	err = deployAnalyticsManifestsWithKubectl(kubectl, zap.NewNop(), AnalyticsImageSet{
		Ingest:    "example.com/mcp-sentinel-ingest:latest",
		API:       "example.com/mcp-sentinel-api:latest",
		Processor: "example.com/mcp-sentinel-processor:latest",
		UI:        "example.com/mcp-sentinel-ui:latest",
	}, "")
	if err == nil {
		t.Fatal("expected rollout failure")
	}
	if !strings.Contains(err.Error(), "deployment/mcp-sentinel-api") {
		t.Fatalf("expected failing workload in error, got %v", err)
	}
}

func TestDeployAnalyticsManifestsWithKubectl_HostpathUsesHostpathManifests(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() { DefaultCLIConfig = orig })
	DefaultCLIConfig = &CLIConfig{}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	root := t.TempDir()
	manifestDir := filepath.Join(root, "k8s")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o755); err != nil {
		t.Fatalf("failed to create services dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	manifestContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fixture\n  namespace: mcp-sentinel\n"
	for _, name := range []string{
		"00-namespace.yaml",
		"01-config.yaml",
		"03-clickhouse-hostpath.yaml",
		"04-clickhouse-init.yaml",
		"05-kafka-hostpath.yaml",
		"20-postgres-hostpath.yaml",
	} {
		if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(manifestContent), 0o644); err != nil {
			t.Fatalf("failed to write fixture manifest %s: %v", name, err)
		}
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir to fixture root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if contains(spec.Args, "get") && contains(spec.Args, "secret") {
				cmd.OutputData = []byte("Error from server (NotFound): secrets \"mcp-sentinel-secrets\" not found")
				cmd.OutputErr = errors.New("not found")
			}
			if contains(spec.Args, "rollout") && contains(spec.Args, "statefulset") && contains(spec.Args, "clickhouse") {
				cmd.RunErr = errors.New("rollout timeout")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	err = deployAnalyticsManifestsWithKubectl(kubectl, zap.NewNop(), AnalyticsImageSet{
		Ingest:    "example.com/mcp-sentinel-ingest:latest",
		API:       "example.com/mcp-sentinel-api:latest",
		Processor: "example.com/mcp-sentinel-processor:latest",
		UI:        "example.com/mcp-sentinel-ui:latest",
	}, StorageModeHostpath)
	if err == nil {
		t.Fatal("expected failure from rollout timeout")
	}
	if strings.Contains(err.Error(), "03-clickhouse.yaml") || strings.Contains(err.Error(), "05-kafka.yaml") {
		t.Fatalf("expected hostpath manifests to be used (default manifests are not present), got err=%v", err)
	}
}

func TestDeployAnalyticsManifestsWithKubectl_WaitsForPostgresStatefulSet(t *testing.T) {
	orig := DefaultCLIConfig
	t.Cleanup(func() { DefaultCLIConfig = orig })
	DefaultCLIConfig = &CLIConfig{}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	root := t.TempDir()
	manifestDir := filepath.Join(root, "k8s")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o755); err != nil {
		t.Fatalf("failed to create services dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	manifestContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fixture\n  namespace: mcp-sentinel\n"
	for _, name := range []string{
		"00-namespace.yaml",
		"01-config.yaml",
		"03-clickhouse.yaml",
		"04-clickhouse-init.yaml",
		"05-kafka.yaml",
		"06-ingest.yaml",
		"07-processor.yaml",
		"08-api.yaml",
		"08-api-rbac.yaml",
		"09-ui.yaml",
		"10-gateway.yaml",
		"11-prometheus.yaml",
		"12-grafana.yaml",
		"15-otel-collector.yaml",
		"16-tempo.yaml",
		"17-loki.yaml",
		"18-promtail.yaml",
		"19-grafana-datasources.yaml",
		"20-postgres.yaml",
	} {
		if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(manifestContent), 0o644); err != nil {
			t.Fatalf("failed to write fixture manifest %s: %v", name, err)
		}
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir to fixture root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var sawPostgresStatefulSet bool
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if contains(spec.Args, "get") && contains(spec.Args, "secret") {
				cmd.OutputData = []byte("Error from server (NotFound): secrets \"mcp-sentinel-secrets\" not found")
				cmd.OutputErr = errors.New("not found")
			}
			if contains(spec.Args, "rollout") && contains(spec.Args, "statefulset/mcp-sentinel-postgres") {
				sawPostgresStatefulSet = true
				cmd.RunErr = errors.New("rollout timeout")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	err = deployAnalyticsManifestsWithKubectl(kubectl, zap.NewNop(), AnalyticsImageSet{
		Ingest:    "example.com/mcp-sentinel-ingest:latest",
		API:       "example.com/mcp-sentinel-api:latest",
		Processor: "example.com/mcp-sentinel-processor:latest",
		UI:        "example.com/mcp-sentinel-ui:latest",
	}, "")
	if err == nil {
		t.Fatal("expected failure from postgres rollout timeout")
	}
	if !sawPostgresStatefulSet {
		t.Fatal("expected setup to wait on statefulset/mcp-sentinel-postgres")
	}
	if !strings.Contains(err.Error(), "statefulset/mcp-sentinel-postgres") {
		t.Fatalf("expected statefulset postgres in error, got %v", err)
	}
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
	if deps.BuildGatewayProxyImage == nil {
		t.Fatal("expected BuildGatewayProxyImage default")
	}
	if deps.PushGatewayProxyImage == nil {
		t.Fatal("expected PushGatewayProxyImage default")
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
	if deps.PushGatewayProxyImageToInternal == nil {
		t.Fatal("expected PushGatewayProxyImageToInternal default")
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
	if deps.GatewayProxyImageFor == nil {
		t.Fatal("expected GatewayProxyImageFor default")
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
		GatewayProxyImageFor: func(_ *ExternalRegistryConfig) string {
			return "custom-gateway-image"
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
	if got.GatewayProxyImageFor(nil) != "custom-gateway-image" {
		t.Fatal("expected GatewayProxyImageFor to be preserved")
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
	gatewayProxyImage := "registry.example.com/mcp-sentinel-mcp-proxy:dev"
	operatorArgs := []string{
		"--metrics-bind-address=:9090",
		"--health-probe-bind-address=:9091",
	}
	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), operatorImage, gatewayProxyImage, operatorArgs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if managerManifest == "" {
		t.Fatal("expected manager manifest to be captured")
	}
	if !strings.Contains(managerManifest, "image: "+operatorImage) {
		t.Fatalf("expected manager manifest to include image %q", operatorImage)
	}
	if !strings.Contains(managerManifest, "imagePullPolicy: Always") {
		t.Fatalf("expected non-test operator image to preserve imagePullPolicy Always, got:\n%s", managerManifest)
	}
	if !strings.Contains(managerManifest, "- --leader-elect") {
		t.Fatalf("expected manager manifest to preserve leader election flag, got:\n%s", managerManifest)
	}
	if !strings.Contains(managerManifest, "- --metrics-bind-address=:9090") {
		t.Fatalf("expected manager manifest to include custom metrics arg, got:\n%s", managerManifest)
	}
	if !strings.Contains(managerManifest, "- --health-probe-bind-address=:9091") {
		t.Fatalf("expected manager manifest to include custom probe arg, got:\n%s", managerManifest)
	}
	if !strings.Contains(managerManifest, "name: MCP_GATEWAY_PROXY_IMAGE") || !strings.Contains(managerManifest, "value: "+gatewayProxyImage) {
		t.Fatalf("expected manager manifest to include gateway proxy image env, got:\n%s", managerManifest)
	}
	if !strings.Contains(managerManifest, "name: MCP_SENTINEL_INGEST_URL") || !strings.Contains(managerManifest, "value: "+defaultAnalyticsIngestURL) {
		t.Fatalf("expected manager manifest to include analytics ingest env, got:\n%s", managerManifest)
	}

	var (
		hasCRD          bool
		hasRBAC         bool
		hasDelete       bool
		hasManagerApply bool
		hasNamespace    bool
	)
	for _, cmd := range mock.Commands {
		if commandHasArgs(cmd, "apply", "--validate=false", "-f", "config/crd/bases") {
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

func TestDeployOperatorManifestsWithKubectlUsesIfNotPresentForTestModeImage(t *testing.T) {
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

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), testModeOperatorImage, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if managerManifest == "" {
		t.Fatal("expected manager manifest to be captured")
	}
	if !strings.Contains(managerManifest, "imagePullPolicy: IfNotPresent") {
		t.Fatalf("expected test mode operator image to use IfNotPresent, got:\n%s", managerManifest)
	}
}

func TestDeployOperatorManifestsWithKubectlCRDError(t *testing.T) {
	mockErr := errors.New("apply crd failed")
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "--validate=false", "-f", "config/crd/bases") {
				cmd.RunErr = mockErr
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example", "", nil); err == nil {
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

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example", "", nil); err == nil {
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

	if err := deployOperatorManifestsWithKubectl(kubectl, zap.NewNop(), "example", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetupTLSWithKubectl(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl

	if err := setupTLSPrivateCA(kubectl, zap.NewNop()); err != nil {
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
		// Registry Certificate is applied via `kubectl apply -f - -n registry` with the
		// manifest piped over stdin, not via `apply -f <path>`.
		if commandHasArgs(cmd, "apply", "-f", "-", "-n", NamespaceRegistry) {
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

	if err := setupTLSPrivateCA(kubectl, zap.NewNop()); err == nil {
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

	if err := setupTLSPrivateCA(kubectl, zap.NewNop()); err == nil {
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

	if err := setupTLSPrivateCA(kubectl, zap.NewNop()); err == nil {
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
