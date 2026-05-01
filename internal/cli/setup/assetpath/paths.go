// Package assetpath resolves repository-relative asset paths from the current
// working directory by walking upward until go.mod, services/, and k8s/ match.
package assetpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveRepoAssetPath finds a repo-relative path from the current working directory
// by walking upward until the asset exists. The repo assumes a flattened root
// layout (for example services/ and k8s/ at the top level).
func ResolveRepoAssetPath(path string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" {
		return "", fmt.Errorf("empty repo asset path")
	}
	if cleaned == "." {
		return ResolveRepoRoot()
	}
	if filepath.IsAbs(cleaned) {
		if _, err := os.Stat(cleaned); err != nil {
			return "", fmt.Errorf("repo asset path %q not found: %w", cleaned, err)
		}
		return cleaned, nil
	}

	root, err := ResolveRepoRoot()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, cleaned)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("repo asset path %q not found from repo root %s", cleaned, root)
}

// ResolveRepoRoot walks upward from the working directory until IsRepoRoot reports true.
func ResolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if IsRepoRoot(cwd) {
			return cwd, nil
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return "", fmt.Errorf("repo root not found from current directory")
}

// IsRepoRoot reports whether dir looks like the mcp-runtime repository root.
func IsRepoRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "services")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "k8s")); err != nil {
		return false
	}
	return true
}
