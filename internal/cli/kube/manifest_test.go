package kube

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

type applyCommand struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	runErr error
}

func (c *applyCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *applyCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *applyCommand) SetStderr(w io.Writer) { c.stderr = w }
func (c *applyCommand) Run() error            { return c.runErr }

type applyRunner struct {
	args []string
	cmd  *applyCommand
	err  error
}

func (r *applyRunner) CommandArgs(args []string) (Command, error) {
	r.args = args
	if r.err != nil {
		return nil, r.err
	}
	r.cmd = &applyCommand{}
	return r.cmd, nil
}

func TestApplyManifestFromFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "manifest-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })
	if _, err := tmpFile.WriteString("apiVersion: v1\nkind: ConfigMap\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	runner := &applyRunner{}
	if err := ApplyManifestFromFile(runner.CommandArgs, tmpFile.Name(), io.Discard, io.Discard); err != nil {
		t.Fatalf("ApplyManifestFromFile() error = %v", err)
	}
	if got, want := strings.Join(runner.args, " "), "apply -f -"; got != want {
		t.Fatalf("kubectl args = %q, want %q", got, want)
	}
	captured, err := io.ReadAll(runner.cmd.stdin)
	if err != nil {
		t.Fatalf("ReadAll(stdin) error = %v", err)
	}
	if string(captured) != "apiVersion: v1\nkind: ConfigMap\n" {
		t.Fatalf("stdin = %q", string(captured))
	}
}

func TestReadFileAtPath(t *testing.T) {
	t.Run("reads regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(path, []byte("kind: Namespace\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		data, err := ReadFileAtPath(path)
		if err != nil {
			t.Fatalf("ReadFileAtPath() error = %v", err)
		}
		if string(data) != "kind: Namespace\n" {
			t.Fatalf("ReadFileAtPath() = %q", string(data))
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

		if _, err := ReadFileAtPath(linkPath); err == nil {
			t.Fatal("ReadFileAtPath() error = nil, want symlink escape rejection")
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

		_, err := ReadFileAtPath(pipePath)
		if err == nil {
			t.Fatal("ReadFileAtPath() error = nil, want non-regular file rejection")
		}
		if !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("ReadFileAtPath() error = %v, want non-regular file rejection", err)
		}
	})
}
