// Package runtimeconfig centralizes per-user MCP Runtime configuration paths.
package runtimeconfig

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvDir overrides the per-user MCP Runtime config directory.
const EnvDir = "MCP_RUNTIME_CONFIG_DIR"

// DirName is the default config directory name under the user's home directory.
const DirName = ".mcpruntime"

// DefaultFileName is the default MCP Runtime config file name.
const DefaultFileName = "config.json"

// Dir returns the per-user MCP Runtime config directory.
func Dir() (string, error) {
	if d := strings.TrimSpace(os.Getenv(EnvDir)); d != "" {
		return filepath.Clean(d), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// Path joins path elements under the MCP Runtime config directory.
func Path(elem ...string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	parts := append([]string{dir}, elem...)
	return filepath.Join(parts...), nil
}

// DefaultFile returns the default MCP Runtime config file path.
func DefaultFile() (string, error) {
	return Path(DefaultFileName)
}
