// Package kube contains shared kubectl-oriented helpers for CLI commands.
package kube

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Command is the minimal command shape needed for stdin-based kubectl apply.
type Command interface {
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	Run() error
}

// ResolveRegularFilePath resolves a path and rejects directories.
func ResolveRegularFilePath(file string) (string, error) {
	absPath, err := filepath.Abs(file)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot access file %q: %w", file, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path %q is a directory, not a file", file)
	}

	return absPath, nil
}

// ReadFileAtPath reads a regular file without following symlink escapes outside its parent directory.
func ReadFileAtPath(path string) ([]byte, error) {
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

// ApplyManifestFromFile applies a manifest file using kubectl.
func ApplyManifestFromFile[T Command](commandArgs func([]string) (T, error), file string, stdout, stderr io.Writer) error {
	absPath, err := ResolveRegularFilePath(file)
	if err != nil {
		return err
	}

	manifestBytes, err := ReadFileAtPath(absPath)
	if err != nil {
		return err
	}

	applyCmd, err := commandArgs([]string{"apply", "-f", "-"})
	if err != nil {
		return err
	}
	applyCmd.SetStdin(bytes.NewReader(manifestBytes))
	applyCmd.SetStdout(stdout)
	applyCmd.SetStderr(stderr)
	return applyCmd.Run()
}

// ApplyManifestContent applies manifest YAML from a string via kubectl stdin.
func ApplyManifestContent[T Command](commandArgs func([]string) (T, error), manifest string) error {
	return ApplyManifestContentWithNamespace(commandArgs, manifest, "")
}

// ApplyManifestContentWithNamespace applies manifest YAML from stdin, optionally scoped to a namespace.
func ApplyManifestContentWithNamespace[T Command](commandArgs func([]string) (T, error), manifest, namespace string) error {
	args := []string{"apply", "-f", "-"}
	if strings.TrimSpace(namespace) != "" {
		args = append(args, "-n", namespace)
	}
	applyCmd, err := commandArgs(args)
	if err != nil {
		return err
	}
	applyCmd.SetStdin(strings.NewReader(manifest))
	applyCmd.SetStdout(os.Stdout)
	applyCmd.SetStderr(os.Stderr)
	return applyCmd.Run()
}

// EnsureNamespace applies/creates a namespace idempotently.
func EnsureNamespace[T Command](commandArgs func([]string) (T, error), name string) error {
	nsYAML := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, name)
	cmd, err := commandArgs([]string{"apply", "--validate=false", "-f", "-"})
	if err != nil {
		return err
	}
	cmd.SetStdin(strings.NewReader(nsYAML))
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}
