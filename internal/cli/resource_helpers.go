package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func resolveRegularFilePath(file string) (string, error) {
	absPath, err := filepath.Abs(file)
	if err != nil {
		return "", wrapWithSentinel(ErrInvalidFilePath, err, fmt.Sprintf("invalid file path: %v", err))
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", wrapWithSentinel(ErrFileNotAccessible, err, fmt.Sprintf("cannot access file %q: %v", file, err))
	}
	if info.IsDir() {
		return "", newWithSentinel(ErrFileIsDirectory, fmt.Sprintf("path %q is a directory, not a file", file))
	}

	return absPath, nil
}

func readFileAtPath(path string) ([]byte, error) {
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

	file, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

func applyManifestFromFile(kubectl *KubectlClient, file string, stdout, stderr io.Writer) error {
	absPath, err := resolveRegularFilePath(file)
	if err != nil {
		return err
	}

	return kubectl.RunWithOutput([]string{"apply", "-f", absPath}, stdout, stderr)
}

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

func normalizePatchDocument(raw string) (string, error) {
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

func normalizePatchFile(file string) (string, error) {
	absPath, err := resolveRegularFilePath(file)
	if err != nil {
		return "", err
	}

	data, err := readFileAtPath(absPath)
	if err != nil {
		return "", wrapWithSentinel(ErrFileNotAccessible, err, fmt.Sprintf("cannot read file %q: %v", file, err))
	}

	return normalizePatchDocument(string(data))
}

func writeOutputFile(file string, data []byte) error {
	absPath, err := filepath.Abs(file)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer root.Close()

	f, err := root.OpenFile(filepath.Base(absPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	n, err := f.Write(data)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("write output file: %w", err)
	}
	if n != len(data) {
		_ = f.Close()
		return fmt.Errorf("write output file: %w", io.ErrShortWrite)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}
