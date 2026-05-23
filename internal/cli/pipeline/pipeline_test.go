package pipeline

import (
	"bytes"
	"encoding/base64"
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

func TestManagerDeployCRDsInjectsAnalyticsSecretForGatewayServer(t *testing.T) {
	var appliedManifests []string
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if contains(spec.Args, "jsonpath={.data.INGEST_API_KEYS}") {
				cmd.OutputData = []byte(base64.StdEncoding.EncodeToString([]byte("ingest-key-1,ingest-key-2")))
			}
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
	kubectl := core.NewTestKubectlClient(mock)
	mgr := &manager{kubectl: kubectl, logger: zap.NewNop()}

	tmpDir := t.TempDir()
	manifest := `apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: demo
  namespace: mcp-team-acme
spec:
  image: registry.example.com/demo
  gateway:
    enabled: true
`
	if err := os.WriteFile(filepath.Join(tmpDir, "server.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := mgr.DeployCRDs(tmpDir, ""); err != nil {
		t.Fatalf("DeployCRDs() error = %v", err)
	}
	if len(appliedManifests) != 2 {
		t.Fatalf("applied manifests = %d, want analytics Secret and MCPServer", len(appliedManifests))
	}
	if !strings.Contains(appliedManifests[0], "kind: Secret") ||
		!strings.Contains(appliedManifests[0], "name: demo-analytics-creds") ||
		!strings.Contains(appliedManifests[0], "namespace: mcp-team-acme") ||
		!strings.Contains(appliedManifests[0], "api-key: "+base64.StdEncoding.EncodeToString([]byte("ingest-key-1"))) {
		t.Fatalf("analytics secret manifest not rendered as expected:\n%s", appliedManifests[0])
	}
	if !strings.Contains(appliedManifests[1], "apiKeySecretRef:") ||
		!strings.Contains(appliedManifests[1], "name: demo-analytics-creds") ||
		!strings.Contains(appliedManifests[1], "key: api-key") {
		t.Fatalf("MCPServer manifest missing injected analytics ref:\n%s", appliedManifests[1])
	}
}

func TestInjectPipelineAnalyticsSecretRefsSkipsExplicitRef(t *testing.T) {
	manifest := []byte(`apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: demo
  namespace: mcp-team-acme
spec:
  gateway:
    enabled: true
  analytics:
    apiKeySecretRef:
      name: existing
      key: api-key
`)
	updated, requests, err := injectPipelineAnalyticsSecretRefs(manifest, "")
	if err != nil {
		t.Fatalf("injectPipelineAnalyticsSecretRefs() error = %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %#v, want none", requests)
	}
	if string(updated) != string(manifest) {
		t.Fatalf("manifest changed unexpectedly:\n%s", string(updated))
	}
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
