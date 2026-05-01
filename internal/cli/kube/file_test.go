package kube

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOutputFileUsesRestrictedDirectoryPermissions(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "exported", "server.yaml")
	if err := WriteOutputFile(target, []byte("kind: Namespace\n")); err != nil {
		t.Fatalf("WriteOutputFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "kind: Namespace\n" {
		t.Fatalf("WriteOutputFile() content = %q", string(data))
	}

	info, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perms := info.Mode().Perm(); perms&0o027 != 0 {
		t.Fatalf("directory permissions = %o, want 0750 or less", perms)
	}
}

func TestWriteOutputFileTightensExistingFilePermissions(t *testing.T) {
	target := filepath.Join(t.TempDir(), "exported", "server.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(target, []byte("kind: Secret\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	if err := WriteOutputFile(target, []byte("kind: Namespace\n")); err != nil {
		t.Fatalf("WriteOutputFile() error = %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("file permissions = %o, want 0600", perms)
	}
}
