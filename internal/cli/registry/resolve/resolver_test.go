package resolve

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestGitTag(t *testing.T) {
	t.Run("returns_latest_when_git_fails", func(t *testing.T) {
		tag := GitTag(func(name string, args []string) (OutputCommand, error) {
			return fakeCommand{outputErr: errors.New("git not found")}, nil
		})
		if tag != "latest" {
			t.Errorf("expected latest when git fails, got %q", tag)
		}
	})

	t.Run("returns_latest_when_output_empty", func(t *testing.T) {
		tag := GitTag(func(name string, args []string) (OutputCommand, error) {
			return fakeCommand{output: []byte("")}, nil
		})
		if tag != "latest" {
			t.Errorf("expected latest when output empty, got %q", tag)
		}
	})

	t.Run("returns_trimmed_sha", func(t *testing.T) {
		tag := GitTag(func(name string, args []string) (OutputCommand, error) {
			return fakeCommand{output: []byte("  abc1234  \n")}, nil
		})
		if tag != "abc1234" {
			t.Errorf("expected abc1234, got %q", tag)
		}
	})
}

func TestPlatformURL(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{
		RegistryEndpoint:        "",
		DefaultRegistryEndpoint: "registry.local",
		RegistryIngressHost:     "registry.local",
		DefaultRegistryHost:     "registry.local",
		RegistryPort:            5000,
	}

	t.Run("prefers_configured_registry_ingress_host_for_image_names", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{}).commandArgs, Config{
			RegistryEndpoint:        "10.43.39.164:5000",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.mcpruntime.org",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.mcpruntime.org" {
			t.Errorf("expected configured registry ingress host, got %q", url)
		}
	})

	t.Run("returns_configured_registry_endpoint_when_available", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{}).commandArgs, Config{
			RegistryEndpoint:        "10.43.39.164:5000",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "10.43.39.164:5000" {
			t.Errorf("expected configured registry endpoint, got %q", url)
		}
	})

	t.Run("non_test_prefers_discovered_registry_ingress_for_implicit_default_endpoint", func(t *testing.T) {
		t.Setenv("MCP_RUNTIME_TEST_MODE", "")
		kubectl := &fakeKubectl{clusterIP: "10.96.201.51", port: "5000", ingressHost: "registry.mcpruntime.org"}
		url := PlatformURL(logger, kubectl.commandArgs, Config{
			RegistryEndpoint:        "registry.local",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.mcpruntime.org" {
			t.Errorf("expected discovered registry ingress host, got %q", url)
		}
		if kubectl.clusterIPQueried {
			t.Error("expected ingress host discovery to avoid ClusterIP lookup")
		}
	})

	t.Run("non_test_uses_service_ip_when_registry_ingress_is_missing", func(t *testing.T) {
		t.Setenv("MCP_RUNTIME_TEST_MODE", "")
		url := PlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}).commandArgs, Config{
			RegistryEndpoint:        "registry.local",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "10.96.201.51:5000" {
			t.Errorf("expected service IP registry URL, got %q", url)
		}
	})

	t.Run("test_mode_prefers_service_dns_over_cluster_ip", func(t *testing.T) {
		t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
		kubectl := &fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}
		url := PlatformURL(logger, kubectl.commandArgs, Config{
			RegistryEndpoint:        "registry.local",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.registry.svc.cluster.local:5000" {
			t.Errorf("expected service DNS registry URL in test mode, got %q", url)
		}
		if kubectl.clusterIPQueried {
			t.Error("expected test mode to avoid ClusterIP lookup")
		}
	})

	t.Run("respects_explicit_default_endpoint_override", func(t *testing.T) {
		t.Setenv("MCP_REGISTRY_ENDPOINT", "registry.local")
		url := PlatformURL(logger, (&fakeKubectl{}).commandArgs, Config{
			RegistryEndpoint:        "registry.local",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.local" {
			t.Errorf("expected explicitly configured endpoint, got %q", url)
		}
	})

	t.Run("test_mode_respects_explicit_registry_host", func(t *testing.T) {
		t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
		t.Setenv("MCP_REGISTRY_HOST", "registry.example.com:5000")
		url := PlatformURL(logger, (&fakeKubectl{}).commandArgs, Config{
			RegistryEndpoint:        "registry.example.com:5000",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.example.com:5000" {
			t.Errorf("expected explicit registry host, got %q", url)
		}
	})

	t.Run("falls_back_to_service_dns_when_cluster_ip_missing", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{clusterIPErr: errors.New("kubectl error"), port: "5000"}).commandArgs, cfg)
		if url != "registry.registry.svc.cluster.local:5000" {
			t.Errorf("expected service DNS registry URL, got %q", url)
		}
	})

	t.Run("returns_default_when_port_command_fails", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", portErr: errors.New("kubectl error")}).commandArgs, cfg)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})

	t.Run("test_mode_returns_default_when_port_command_fails", func(t *testing.T) {
		t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
		url := PlatformURL(logger, (&fakeKubectl{portErr: errors.New("kubectl error")}).commandArgs, Config{
			RegistryEndpoint:        "",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.local",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5001,
		})
		if url != "registry.registry.svc.cluster.local:5001" {
			t.Errorf("expected default service DNS registry URL, got %q", url)
		}
	})

	t.Run("returns_service_dns_when_ip_empty", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{clusterIP: "", port: "5000"}).commandArgs, cfg)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})

	t.Run("returns_default_when_port_empty", func(t *testing.T) {
		url := PlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", port: ""}).commandArgs, cfg)
		if !strings.Contains(url, "registry.registry.svc.cluster.local") {
			t.Errorf("expected default registry URL, got %q", url)
		}
	})
}

func TestInternalPlatformURL(t *testing.T) {
	logger := zap.NewNop()
	clearRegistryEnv := func(t *testing.T) {
		t.Helper()
		for _, key := range []string{
			"MCP_RUNTIME_TEST_MODE",
			"MCP_PLATFORM_DOMAIN",
			"MCP_REGISTRY_ENDPOINT",
			"MCP_REGISTRY_HOST",
			"MCP_REGISTRY_INGRESS_HOST",
		} {
			t.Setenv(key, "")
		}
	}

	t.Run("test_mode_ignores_public_host_from_platform_domain", func(t *testing.T) {
		clearRegistryEnv(t)
		t.Setenv("MCP_RUNTIME_TEST_MODE", "1")
		t.Setenv("MCP_PLATFORM_DOMAIN", "mcpruntime.org")
		kubectl := &fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}
		url := InternalPlatformURL(logger, kubectl.commandArgs, Config{
			RegistryEndpoint:        "registry.mcpruntime.org",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.mcpruntime.org",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.registry.svc.cluster.local:5000" {
			t.Errorf("expected service DNS registry URL, got %q", url)
		}
		if kubectl.clusterIPQueried {
			t.Error("expected test mode to avoid ClusterIP lookup")
		}
	})

	t.Run("non_test_ignores_public_host_from_platform_domain", func(t *testing.T) {
		clearRegistryEnv(t)
		t.Setenv("MCP_RUNTIME_TEST_MODE", "")
		t.Setenv("MCP_PLATFORM_DOMAIN", "mcpruntime.org")
		url := InternalPlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}).commandArgs, Config{
			RegistryEndpoint:        "registry.mcpruntime.org",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.mcpruntime.org",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "10.96.201.51:5000" {
			t.Errorf("expected ClusterIP registry URL, got %q", url)
		}
	})

	t.Run("respects_explicit_registry_endpoint", func(t *testing.T) {
		clearRegistryEnv(t)
		t.Setenv("MCP_REGISTRY_ENDPOINT", "registry.internal:5000")
		url := InternalPlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}).commandArgs, Config{
			RegistryEndpoint:        "registry.internal:5000",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.mcpruntime.org",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "registry.internal:5000" {
			t.Errorf("expected explicit registry endpoint, got %q", url)
		}
	})

	t.Run("uses_non_default_endpoint_when_it_differs_from_public_host", func(t *testing.T) {
		clearRegistryEnv(t)
		url := InternalPlatformURL(logger, (&fakeKubectl{clusterIP: "10.96.201.51", port: "5000"}).commandArgs, Config{
			RegistryEndpoint:        "10.43.39.164:5000",
			DefaultRegistryEndpoint: "registry.local",
			RegistryIngressHost:     "registry.mcpruntime.org",
			DefaultRegistryHost:     "registry.local",
			RegistryPort:            5000,
		})
		if url != "10.43.39.164:5000" {
			t.Errorf("expected non-default internal endpoint, got %q", url)
		}
	})
}

type fakeCommand struct {
	output    []byte
	outputErr error
}

func (c fakeCommand) Output() ([]byte, error) {
	return c.output, c.outputErr
}

type fakeKubectl struct {
	clusterIP        string
	clusterIPErr     error
	ingressHost      string
	ingressErr       error
	port             string
	portErr          error
	clusterIPQueried bool
}

func (k *fakeKubectl) commandArgs(args []string) (OutputCommand, error) {
	for _, arg := range args {
		switch arg {
		case "jsonpath={.spec.clusterIP}":
			k.clusterIPQueried = true
			return fakeCommand{output: []byte(k.clusterIP), outputErr: k.clusterIPErr}, nil
		case "jsonpath={.spec.rules[0].host}":
			return fakeCommand{output: []byte(k.ingressHost), outputErr: k.ingressErr}, nil
		case "jsonpath={.spec.ports[0].port}":
			return fakeCommand{output: []byte(k.port), outputErr: k.portErr}, nil
		}
	}
	return fakeCommand{}, nil
}
