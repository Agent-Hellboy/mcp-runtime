// Package e2e provides end-to-end tests for mcp-runtime.
//
// These tests require a REAL Kubernetes cluster with mcp-runtime fully deployed.
// Use these tests to verify the complete system works end-to-end.
//
// Prerequisites:
//   - A running Kubernetes cluster (kind, minikube, etc.)
//   - mcp-runtime deployed: mcp-runtime setup cluster
//   - kubectl configured to access the cluster
//
// Run with: go test -v ./test/e2e/...
// Skip with: go test -short ./...
package e2e

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
		t.Skip("skipping e2e test in short mode")
	}
}

// skipIfNoCluster skips the test if no Kubernetes cluster is accessible.
func skipIfNoCluster(t *testing.T) {
	cmd := exec.Command("kubectl", "cluster-info")
	if err := cmd.Run(); err != nil {
		t.Skip("skipping: no Kubernetes cluster accessible (run 'kind create cluster' or similar)")
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
	t.Log("Cluster is accessible")
}

// TestCRDInstalled verifies the MCPServer CRD is installed.
func TestCRDInstalled(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	output := runCommand(t, "kubectl", "get", "crd", "mcpservers.mcp-runtime.org")
	if !strings.Contains(output, "mcpservers.mcp-runtime.org") {
		t.Errorf("CRD not found: %s", output)
	}
	t.Log("MCPServer CRD is installed")
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
	t.Log("Operator is running")
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
	t.Log("Registry is running")
}

// TestMCPServerLifecycle tests creating and deleting an MCPServer resource end-to-end.
func TestMCPServerLifecycle(t *testing.T) {
	skipIfShort(t)
	skipIfNoCluster(t)

	serverName := "e2e-test-server"
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
	manifest := `apiVersion: mcp-runtime.org/v1alpha1
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
	t.Log("MCPServer created")

	// Wait for deployment to be created
	waitForCondition(t, 60*time.Second, 2*time.Second, func() bool {
		output, err := runCommandAllowFail("kubectl", "get", "deployment", serverName, "-n", namespace)
		return err == nil && strings.Contains(output, serverName)
	}, "deployment to be created")
	t.Log("Deployment created")

	// Wait for deployment to be ready
	waitForCondition(t, 120*time.Second, 5*time.Second, func() bool {
		output, _ := runCommandAllowFail("kubectl", "get", "deployment", serverName, "-n", namespace,
			"-o", "jsonpath={.status.readyReplicas}")
		return strings.TrimSpace(output) == "1"
	}, "deployment to be ready")
	t.Log("Deployment is ready")

	// Verify Service exists
	runCommand(t, "kubectl", "get", "service", serverName, "-n", namespace)
	t.Log("Service exists")

	// Verify MCPServer status
	output := runCommand(t, "kubectl", "get", "mcpserver", serverName, "-n", namespace,
		"-o", "jsonpath={.status.phase}")
	t.Logf("MCPServer phase: %s", output)
}

// TestMain sets up and tears down test fixtures.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
