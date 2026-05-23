package runtimeconfig

import (
	"path/filepath"
	"testing"
)

func TestDirDefaultsToHomeDotMCPRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvDir, "")

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, DirName)
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestDirRespectsEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	t.Setenv(EnvDir, dir)

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("Dir() = %q, want %q", got, dir)
	}
}

func TestDefaultFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)

	got, err := DefaultFile()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, DefaultFileName)
	if got != want {
		t.Fatalf("DefaultFile() = %q, want %q", got, want)
	}
}
