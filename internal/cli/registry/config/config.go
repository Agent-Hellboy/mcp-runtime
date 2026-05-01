package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	ErrURLRequired        = errors.New("registry url is required")
	ErrURLMissingInConfig = errors.New("registry url missing in config")
)

type ExternalRegistryConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

type Env struct {
	URL      string
	Username string
	Password string
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mcp-runtime", "registry.yaml"), nil
}

func Save(cfg *ExternalRegistryConfig) error {
	if cfg == nil || cfg.URL == "" {
		return ErrURLRequired
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Marshal(cfg *ExternalRegistryConfig) ([]byte, error) {
	data := map[string]string{
		"url": cfg.URL,
	}
	if cfg.Username != "" {
		data["username"] = cfg.Username
	}
	if cfg.Password != "" {
		data["password"] = cfg.Password
	}
	return yaml.Marshal(data)
}

func Load() (*ExternalRegistryConfig, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path is scoped to the user's config directory.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry config: %w", err)
	}
	var cfg ExternalRegistryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal registry config: %w", err)
	}
	if cfg.URL == "" {
		return nil, ErrURLMissingInConfig
	}
	return &cfg, nil
}

// Resolve returns external registry config using precedence: flags > env > config file.
func Resolve(flagCfg *ExternalRegistryConfig, env Env) (*ExternalRegistryConfig, error) {
	var cfg ExternalRegistryConfig
	sourceFound := false

	if fileCfg, err := Load(); err == nil && fileCfg != nil {
		cfg = *fileCfg
		if cfg.URL != "" {
			sourceFound = true
		}
	} else if err != nil {
		return nil, err
	}

	if env.URL != "" {
		cfg.URL = env.URL
		sourceFound = true
	}
	if env.Username != "" {
		cfg.Username = env.Username
		sourceFound = true
	}
	if env.Password != "" {
		cfg.Password = env.Password
		sourceFound = true
	}

	if flagCfg != nil {
		if flagCfg.URL != "" {
			cfg.URL = flagCfg.URL
			sourceFound = true
		}
		if flagCfg.Username != "" {
			cfg.Username = flagCfg.Username
			sourceFound = true
		}
		if flagCfg.Password != "" {
			cfg.Password = flagCfg.Password
			sourceFound = true
		}
	}

	if cfg.URL == "" {
		if sourceFound {
			return nil, ErrURLRequired
		}
		return nil, nil
	}

	return &cfg, nil
}
