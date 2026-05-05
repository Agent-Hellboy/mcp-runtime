package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	expectedSuffix := filepath.Join(".mcp-runtime", "registry.yaml")
	if !strings.HasSuffix(path, expectedSuffix) {
		t.Fatalf("expected path to end with %q, got %q", expectedSuffix, path)
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("expected path to start with home %q, got %q", home, path)
	}
}

func TestSaveAndLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &ExternalRegistryConfig{
		URL:      "registry.example.com",
		Username: "user",
		Password: "pass",
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected config to be loaded")
	}
	if loaded.URL != cfg.URL || loaded.Username != cfg.Username || loaded.Password != cfg.Password {
		t.Fatalf("loaded config mismatch: %#v", loaded)
	}
}

func TestLoadMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when file missing, got %#v", cfg)
	}
}

func TestLoadInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(":::invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Save(&ExternalRegistryConfig{URL: "file.example.com"}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	t.Run("uses file config when no overrides", func(t *testing.T) {
		cfg, err := Resolve(nil, Env{})
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		if cfg.URL != "file.example.com" {
			t.Fatalf("expected file URL, got %#v", cfg)
		}
	})

	t.Run("env overrides file", func(t *testing.T) {
		cfg, err := Resolve(nil, Env{URL: "env.example.com", Username: "envuser"})
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		if cfg.URL != "env.example.com" || cfg.Username != "envuser" {
			t.Fatalf("expected env overrides, got %#v", cfg)
		}
	})

	t.Run("flags override env and file", func(t *testing.T) {
		cfg, err := Resolve(&ExternalRegistryConfig{URL: "flag.example.com", Password: "flagpass"}, Env{
			URL:      "env.example.com",
			Username: "envuser",
			Password: "envpass",
		})
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		if cfg.URL != "flag.example.com" || cfg.Username != "envuser" || cfg.Password != "flagpass" {
			t.Fatalf("expected flag/env precedence, got %#v", cfg)
		}
	})
}

func TestSaveErrors(t *testing.T) {
	if err := Save(nil); !errors.Is(err, ErrURLRequired) {
		t.Fatalf("expected ErrURLRequired for nil config, got %v", err)
	}
	if err := Save(&ExternalRegistryConfig{}); !errors.Is(err, ErrURLRequired) {
		t.Fatalf("expected ErrURLRequired for empty URL, got %v", err)
	}
}

func TestResolveErrors(t *testing.T) {
	t.Run("returns nil when no source found", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		cfg, err := Resolve(nil, Env{})
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

		_, err := Resolve(nil, Env{Username: "user"})
		if !errors.Is(err, ErrURLRequired) {
			t.Fatalf("expected ErrURLRequired, got %v", err)
		}
	})
}
