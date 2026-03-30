package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepoAssetPath(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	pkgDir := filepath.Join(repoRoot, "internal", "cli")

	if err := os.MkdirAll(filepath.Join(repoRoot, "k8s"), 0o755); err != nil {
		t.Fatalf("mkdir repo k8s: %v", err)
	}
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "k8s", "08-api.yaml"), []byte("kind: Service\n"), 0o644); err != nil {
		t.Fatalf("write test manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "services"), 0o755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(pkgDir); err != nil {
		t.Fatalf("chdir pkg dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	t.Run("walks upward to repo root", func(t *testing.T) {
		got, err := resolveRepoAssetPath(filepath.Join("k8s", "08-api.yaml"))
		if err != nil {
			t.Fatalf("resolveRepoAssetPath() error = %v", err)
		}

		want := filepath.Join(repoRoot, "k8s", "08-api.yaml")
		gotEval, err := filepath.EvalSymlinks(got)
		if err != nil {
			t.Fatalf("EvalSymlinks(got) error = %v", err)
		}
		wantEval, err := filepath.EvalSymlinks(want)
		if err != nil {
			t.Fatalf("EvalSymlinks(want) error = %v", err)
		}
		if gotEval != wantEval {
			t.Fatalf("resolveRepoAssetPath() = %q, want %q", gotEval, wantEval)
		}
	})

	t.Run("accepts absolute paths", func(t *testing.T) {
		want := filepath.Join(repoRoot, "k8s", "08-api.yaml")
		got, err := resolveRepoAssetPath(want)
		if err != nil {
			t.Fatalf("resolveRepoAssetPath() error = %v", err)
		}
		if got != want {
			t.Fatalf("resolveRepoAssetPath() = %q, want %q", got, want)
		}
	})

	t.Run("resolves repo root for dot context", func(t *testing.T) {
		got, err := resolveRepoAssetPath(".")
		if err != nil {
			t.Fatalf("resolveRepoAssetPath(.) error = %v", err)
		}

		gotEval, err := filepath.EvalSymlinks(got)
		if err != nil {
			t.Fatalf("EvalSymlinks(got) error = %v", err)
		}
		wantEval, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			t.Fatalf("EvalSymlinks(repoRoot) error = %v", err)
		}
		if gotEval != wantEval {
			t.Fatalf("resolveRepoAssetPath(.) = %q, want %q", gotEval, wantEval)
		}
	})

	t.Run("errors for missing assets", func(t *testing.T) {
		if _, err := resolveRepoAssetPath(filepath.Join("k8s", "missing.yaml")); err == nil {
			t.Fatal("expected error for missing asset")
		}
	})
}
