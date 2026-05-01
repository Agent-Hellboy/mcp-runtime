package kube

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func normalizePatchValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized[key] = normalizePatchValue(child)
		}
		return normalized
	case map[any]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized[fmt.Sprint(key)] = normalizePatchValue(child)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for i, child := range typed {
			normalized[i] = normalizePatchValue(child)
		}
		return normalized
	default:
		return value
	}
}

// NormalizePatchDocument parses YAML or JSON patch content and returns a JSON
// string suitable for kubectl patch --type=json (or merge) style inputs.
func NormalizePatchDocument(raw string) (string, error) {
	var value any
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		return "", fmt.Errorf("parse patch document: %w", err)
	}

	data, err := json.Marshal(normalizePatchValue(value))
	if err != nil {
		return "", fmt.Errorf("marshal patch document: %w", err)
	}

	return string(data), nil
}

func readRegularPatchFile(path string) ([]byte, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve file path: %w", err)
	}

	root, err := os.OpenRoot(filepath.Dir(absPath))
	if err != nil {
		return nil, err
	}
	defer root.Close()

	base := filepath.Base(absPath)
	info, err := root.Stat(base)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read file %q: not a regular file", path)
	}

	f, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// NormalizePatchFile reads a patch file from disk and returns normalized JSON
// like NormalizePatchDocument.
func NormalizePatchFile(file string) (string, error) {
	absPath, err := filepath.Abs(file)
	if err != nil {
		return "", fmt.Errorf("resolve patch file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat patch file %q: %w", file, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("patch path %q is a directory", file)
	}

	data, err := readRegularPatchFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read patch file %q: %w", file, err)
	}

	return NormalizePatchDocument(string(data))
}
