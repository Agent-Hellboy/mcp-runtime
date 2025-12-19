// Package integration provides end-to-end integration tests for mcp-runtime.
// These tests require a running Kubernetes cluster (e.g., kind, minikube).
//
// Run with: go test -v ./test/integration/... -tags=integration
// Skip with: go test -short ./...
package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// skipIfShort skips the test if running in short mode.
func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

// skipIfNoCluster skips the test if no Kubernetes cluster is accessible.
func skipIfNoCluster(t *testing.T) {
	cmd := exec.Command("kubectl", "cluster-info")
	if err := cmd.Run(); err != nil {
		t.Skip("skipping integration test: no Kubernetes cluster accessible")
	}
}

// runCommand runs a command and returns its output.
func runCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout: %s\nstderr: %s",
			name, args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// runCommandAllowFail runs a command and returns output even if it fails.
func runCommandAllowFail(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String() + stderr.String(), err
}

// waitForCondition polls until a condition is met or timeout.
func waitForCondition(t *testing.T, timeout time.Duration, poll time.Duration, condition func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(poll)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// TestClusterConnectivity verifies the test cluster is accessible.
func TestClusterConnectivity(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	output := runCommand(t, "kubectl", "cluster-info")
	if !strings.Contains(output, "Kubernetes") {
		t.Errorf("unexpected cluster-info output: %s", output)
	}
}

// TestCRDInstalled verifies the MCPServer CRD is installed.
func TestCRDInstalled(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	output := runCommand(t, "kubectl", "get", "crd", "mcpservers.mcp.agent-hellboy.io")
	if !strings.Contains(output, "mcpservers.mcp.agent-hellboy.io") {
		t.Errorf("CRD not found: %s", output)
	}
}

// TestOperatorRunning verifies the operator is running.
func TestOperatorRunning(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	output := runCommand(t, "kubectl", "get", "deployment",
		"mcp-runtime-operator-controller-manager",
		"-n", "mcp-runtime",
		"-o", "jsonpath={.status.readyReplicas}")

	if strings.TrimSpace(output) != "1" {
		t.Errorf("operator not ready, replicas: %s", output)
	}
}

// TestRegistryRunning verifies the registry is running.
func TestRegistryRunning(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	output := runCommand(t, "kubectl", "get", "deployment",
		"registry",
		"-n", "registry",
		"-o", "jsonpath={.status.readyReplicas}")

	if strings.TrimSpace(output) != "1" {
		t.Errorf("registry not ready, replicas: %s", output)
	}
}

// TestMCPServerCreate tests creating an MCPServer resource.
func TestMCPServerCreate(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	serverName := "integration-test-server"
	namespace := "mcp-servers"

	// Clean up before and after
	cleanup := func() {
		_, _ = runCommandAllowFail("kubectl", "delete", "mcpserver", serverName, "-n", namespace, "--ignore-not-found")
	}
	cleanup()
	t.Cleanup(cleanup)

	// Ensure namespace exists
	_, _ = runCommandAllowFail("kubectl", "create", "namespace", namespace)

	// Create MCPServer
	manifest := `apiVersion: mcp.agent-hellboy.io/v1alpha1
kind: MCPServer
metadata:
  name: ` + serverName + `
  namespace: ` + namespace + `
spec:
  image: nginx
  imageTag: alpine
  replicas: 1
  port: 80
  servicePort: 80
  ingressPath: /` + serverName + `
`

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create MCPServer: %v\n%s", err, output)
	}

	// Wait for deployment to be created
	waitForCondition(t, 60*time.Second, 2*time.Second, func() bool {
		output, err := runCommandAllowFail("kubectl", "get", "deployment", serverName, "-n", namespace)
		return err == nil && strings.Contains(output, serverName)
	}, "deployment to be created")

	// Verify MCPServer status
	output := runCommand(t, "kubectl", "get", "mcpserver", serverName, "-n", namespace, "-o", "yaml")
	t.Logf("MCPServer status:\n%s", output)
}

// TestMain sets up and tears down test fixtures.
func TestMain(m *testing.M) {
	// Check if we should run integration tests
	if os.Getenv("INTEGRATION_TEST") != "1" && !strings.Contains(strings.Join(os.Args, " "), "-test.run") {
		// Not explicitly running integration tests
		os.Exit(0)
	}

	os.Exit(m.Run())
}
