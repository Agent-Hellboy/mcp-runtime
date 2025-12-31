package main

import (
	"flag"
	"io"
	"testing"

	"mcp-runtime/internal/operator"
)

func TestRegistryConfigFromEnv(t *testing.T) {
	t.Run("missing_url_returns_nil", func(t *testing.T) {
		getenv := func(string) string { return "" }
		if got := registryConfigFromEnv(getenv); got != nil {
			t.Fatalf("expected nil config when url is missing")
		}
	})

	t.Run("builds_config_from_env", func(t *testing.T) {
		env := map[string]string{
			"PROVISIONED_REGISTRY_URL":         "registry.example.com",
			"PROVISIONED_REGISTRY_USERNAME":    "user",
			"PROVISIONED_REGISTRY_PASSWORD":    "pass",
			"PROVISIONED_REGISTRY_SECRET_NAME": "secret",
		}
		getenv := func(key string) string { return env[key] }

		got := registryConfigFromEnv(getenv)
		if got == nil {
			t.Fatalf("expected config")
		}

		want := &operator.RegistryConfig{
			URL:        "registry.example.com",
			Username:   "user",
			Password:   "pass",
			SecretName: "secret",
		}

		if *got != *want {
			t.Fatalf("unexpected config: got %+v want %+v", *got, *want)
		}
	})
}

func TestParseConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)

		cfg, err := parseConfig(fs, nil)
		if err != nil {
			t.Fatalf("parseConfig() error: %v", err)
		}

		if cfg.metricsAddr != ":8080" {
			t.Fatalf("unexpected metricsAddr: %q", cfg.metricsAddr)
		}
		if cfg.probeAddr != ":8081" {
			t.Fatalf("unexpected probeAddr: %q", cfg.probeAddr)
		}
		if cfg.enableLeaderElection {
			t.Fatalf("expected leader election disabled by default")
		}
		if !cfg.zapOptions.Development {
			t.Fatalf("expected development logging default")
		}
	})

	t.Run("overrides", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)

		args := []string{
			"--metrics-bind-address=localhost:9090",
			"--health-probe-bind-address=localhost:9091",
			"--leader-elect",
		}
		cfg, err := parseConfig(fs, args)
		if err != nil {
			t.Fatalf("parseConfig() error: %v", err)
		}

		if cfg.metricsAddr != "localhost:9090" {
			t.Fatalf("unexpected metricsAddr: %q", cfg.metricsAddr)
		}
		if cfg.probeAddr != "localhost:9091" {
			t.Fatalf("unexpected probeAddr: %q", cfg.probeAddr)
		}
		if !cfg.enableLeaderElection {
			t.Fatalf("expected leader election enabled")
		}
	})
}

func TestNewManagerOptions(t *testing.T) {
	cfg := &operatorConfig{
		metricsAddr:          "localhost:9999",
		probeAddr:            "localhost:9998",
		enableLeaderElection: true,
	}

	opts := newManagerOptions(cfg)

	if opts.Scheme != scheme {
		t.Fatalf("expected scheme to be set")
	}
	if opts.Metrics.BindAddress != "localhost:9999" {
		t.Fatalf("unexpected metrics bind address: %q", opts.Metrics.BindAddress)
	}
	if opts.HealthProbeBindAddress != "localhost:9998" {
		t.Fatalf("unexpected probe addr: %q", opts.HealthProbeBindAddress)
	}
	if !opts.LeaderElection {
		t.Fatalf("expected leader election enabled")
	}
	if opts.LeaderElectionID != "mcp-runtime-operator.mcpruntime.org" {
		t.Fatalf("unexpected leader election id: %q", opts.LeaderElectionID)
	}
}
