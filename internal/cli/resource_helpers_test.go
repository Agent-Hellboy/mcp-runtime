package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestReadFileAtPath(t *testing.T) {
	t.Run("reads regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(path, []byte("kind: Namespace\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		data, err := readFileAtPath(path)
		if err != nil {
			t.Fatalf("readFileAtPath() error = %v", err)
		}
		if string(data) != "kind: Namespace\n" {
			t.Fatalf("readFileAtPath() = %q", string(data))
		}
	})

	t.Run("rejects symlink that escapes the opened root", func(t *testing.T) {
		baseDir := t.TempDir()
		manifestDir := filepath.Join(baseDir, "manifests")
		if err := os.MkdirAll(manifestDir, 0o750); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		outsidePath := filepath.Join(baseDir, "outside.yaml")
		if err := os.WriteFile(outsidePath, []byte("kind: Secret\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		linkPath := filepath.Join(manifestDir, "linked.yaml")
		relTarget, err := filepath.Rel(manifestDir, outsidePath)
		if err != nil {
			t.Fatalf("Rel() error = %v", err)
		}
		if err := os.Symlink(relTarget, linkPath); err != nil {
			t.Skipf("Symlink() unavailable: %v", err)
		}

		if _, err := readFileAtPath(linkPath); err == nil {
			t.Fatal("readFileAtPath() error = nil, want symlink escape rejection")
		}
	})

	t.Run("rejects non-regular files", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("named pipes are not exercised in this test on Windows")
		}

		pipePath := filepath.Join(t.TempDir(), "manifest.pipe")
		if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
			t.Skipf("Mkfifo() unavailable: %v", err)
		}

		_, err := readFileAtPath(pipePath)
		if err == nil {
			t.Fatal("readFileAtPath() error = nil, want non-regular file rejection")
		}
		if !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("readFileAtPath() error = %v, want non-regular file rejection", err)
		}
	})
}

func TestWriteOutputFileUsesRestrictedDirectoryPermissions(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "exported", "server.yaml")
	if err := writeOutputFile(target, []byte("kind: Namespace\n")); err != nil {
		t.Fatalf("writeOutputFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "kind: Namespace\n" {
		t.Fatalf("writeOutputFile() content = %q", string(data))
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

	if err := writeOutputFile(target, []byte("kind: Namespace\n")); err != nil {
		t.Fatalf("writeOutputFile() error = %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("file permissions = %o, want 0600", perms)
	}
}
