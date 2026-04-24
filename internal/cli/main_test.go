package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain chdirs to the module root before running tests so relative manifest
// paths used by the CLI (e.g. "config/cert-manager/example-registry-certificate.yaml")
// resolve the same way they do when the binary is invoked from the repo root.
func TestMain(m *testing.M) {
	if root, err := findModuleRoot(); err == nil {
		if err := os.Chdir(root); err != nil {
			fmt.Fprintf(os.Stderr, "TestMain: chdir to module root %q failed: %v\n", root, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func findModuleRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
