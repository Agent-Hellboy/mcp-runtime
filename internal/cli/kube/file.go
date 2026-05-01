package kube

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteOutputFile writes data to a path under a resolved parent directory with
// 0600 file permissions and 0750 (or tighter) directory permissions.
func WriteOutputFile(file string, data []byte) error {
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
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("write output file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}
