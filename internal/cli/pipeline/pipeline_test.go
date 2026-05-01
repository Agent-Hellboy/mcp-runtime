package pipeline

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

func TestManagerDeployCRDs(t *testing.T) {
	t.Run("returns error when no manifests found", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl, err := core.NewKubectlClient(mock)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
		mgr := &manager{kubectl: kubectl, logger: zap.NewNop()}

		runErr := mgr.DeployCRDs(t.TempDir(), "test-ns")
		if runErr == nil {
			t.Fatal("expected error when no manifests found")
		}
	})

	t.Run("applies each manifest file", func(t *testing.T) {
		var appliedManifests []string
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				cmd.RunFunc = func() error {
					if cmd.StdinR != nil {
						data, err := io.ReadAll(cmd.StdinR)
						if err != nil {
							return err
						}
						appliedManifests = append(appliedManifests, string(data))
					}
					return nil
				}
				return cmd
			},
		}
		kubectl, err := core.NewKubectlClient(mock)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
		mgr := &manager{kubectl: kubectl, logger: zap.NewNop()}

		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "server1.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "server2.yml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}

		err = mgr.DeployCRDs(tmpDir, "test-ns")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		applyCount := 0
		for _, cmd := range mock.Commands {
			if cmd.Name == "kubectl" && contains(cmd.Args, "apply") {
				applyCount++
			}
		}
		if applyCount != 2 {
			t.Fatalf("expected 2 kubectl apply calls, got %d", applyCount)
		}
		if len(appliedManifests) != 2 {
			t.Fatalf("expected 2 applied manifests, got %d", len(appliedManifests))
		}
	})
}

func TestManagerGenerateCRDsFromMetadata(t *testing.T) {
	t.Run("returns error for missing metadata", func(t *testing.T) {
		mgr := &manager{logger: zap.NewNop()}
		if err := mgr.GenerateCRDsFromMetadata("nonexistent.yaml", "", t.TempDir()); err == nil {
			t.Fatal("expected error for missing metadata file")
		}
	})

	t.Run("generates CRDs from file successfully", func(t *testing.T) {
		var buf bytes.Buffer
		origWriter := core.DefaultPrinter.Writer
		core.DefaultPrinter.Writer = &buf
		t.Cleanup(func() { core.DefaultPrinter.Writer = origWriter })

		mgr := &manager{kubectl: &core.KubectlClient{}, logger: zap.NewNop()}
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		metadataFile := filepath.Join(tmpDir, "servers.yaml")
		content := `version: "1"
servers:
  - name: test-server
    image: test-image:latest
`
		if err := os.WriteFile(metadataFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := mgr.GenerateCRDsFromMetadata(metadataFile, "", outputDir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		files, _ := filepath.Glob(filepath.Join(outputDir, "*.yaml"))
		if len(files) == 0 {
			t.Fatal("expected CRD files to be generated")
		}
	})
}

func TestManagerDeployCRDsErrors(t *testing.T) {
	t.Run("apply error", func(t *testing.T) {
		mock := &core.MockExecutor{DefaultRunErr: errors.New("apply failed")}
		kubectl, err := core.NewKubectlClient(mock)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
		mgr := &manager{kubectl: kubectl, logger: zap.NewNop()}

		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte("apiVersion: v1"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := mgr.DeployCRDs(tmpDir, ""); err == nil {
			t.Fatal("expected error when apply fails")
		}
	})

	t.Run("glob yaml error", func(t *testing.T) {
		originalGlob := filepathGlob
		t.Cleanup(func() { filepathGlob = originalGlob })
		filepathGlob = func(pattern string) ([]string, error) {
			return nil, errors.New("glob error")
		}

		mock := &core.MockExecutor{}
		kubectl, err := core.NewKubectlClient(mock)
		if err != nil {
			t.Fatalf("failed to create kubectl client: %v", err)
		}
		mgr := &manager{kubectl: kubectl, logger: zap.NewNop()}

		if err := mgr.DeployCRDs("/some/dir", ""); err == nil {
			t.Fatal("expected error when glob fails")
		}
	})
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if strings.TrimSpace(s) == val {
			return true
		}
	}
	return false
}
