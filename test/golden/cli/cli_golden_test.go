package cli

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var (
	update     = flag.Bool("update", false, "update CLI golden files")
	binaryOnce sync.Once
	binaryPath string
)

func TestCLIHelpGoldens(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		golden string
	}{
		{name: "root_help", args: []string{"--help"}, golden: "mcp-runtime_help.golden"},
		{name: "server_help", args: []string{"server", "--help"}, golden: "mcp-runtime_server_help.golden"},
		{name: "server_build_help", args: []string{"server", "build", "--help"}, golden: "mcp-runtime_server_build_help.golden"},
		{name: "registry_help", args: []string{"registry", "--help"}, golden: "mcp-runtime_registry_help.golden"},
		{name: "setup_help", args: []string{"setup", "--help"}, golden: "mcp-runtime_setup_help.golden"},
		{name: "pipeline_help", args: []string{"pipeline", "--help"}, golden: "mcp-runtime_pipeline_help.golden"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := runCLI(t, tc.args...)
			goldenPath := filepath.Join(testdataDir(t), tc.golden)

			if *update {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("failed to update golden %s: %v", tc.golden, err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed to read golden %s: %v", tc.golden, err)
			}

			if diff := cmp.Diff(string(want), string(got)); diff != "" {
				t.Fatalf("CLI output mismatch for %s (-want +got):\n%s", tc.golden, diff)
			}
		})
	}
}

func runCLI(t *testing.T, args ...string) []byte {
	t.Helper()

	// Ensure binary is built once per test run
	binaryOnce.Do(func() {
		root := repoRoot(t)
		binaryPath = filepath.Join(root, "bin", "mcp-runtime")

		// Build binary if it doesn't exist
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			t.Logf("Building binary at %s", binaryPath)
			if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
				t.Fatalf("failed to create bin directory: %v", err)
			}

			// #nosec G204 -- test code with trusted paths
			buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/mcp-runtime")
			buildCmd.Dir = root
			if err := buildCmd.Run(); err != nil {
				t.Fatalf("failed to build binary: %v", err)
			}
		}
	})

	// Execute binary with args
	// #nosec G204 -- test code with trusted binary path
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = repoRoot(t)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed for args %v: %v\nOutput:\n%s", args, err, out)
	}

	return out
}

func testdataDir(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller")
	}

	return filepath.Join(filepath.Dir(filename), "testdata")
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}
