// Package manifest provides tests for the manifest mutator.
package manifest

import (
	"strings"
	"testing"
)

func TestNewMutator(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test-container
        image: nginx:latest
`
	m, err := NewMutator([]byte(yaml))
	if err != nil {
		t.Fatalf("NewMutator failed: %v", err)
	}

	deployment := m.FindDeployment("test-deployment", "")
	if deployment == nil {
		t.Error("FindDeployment should find the deployment")
	}

	notFound := m.FindDeployment("non-existent", "")
	if notFound != nil {
		t.Error("FindDeployment should return nil for non-existent deployment")
	}
}

func TestFindDeploymentByNamespace(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: first
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: second
`
	m, err := NewMutator([]byte(yaml))
	if err != nil {
		t.Fatalf("NewMutator failed: %v", err)
	}

	deployment := m.FindDeployment("test-deployment", "second")
	if deployment == nil {
		t.Fatal("FindDeployment should find deployment in requested namespace")
	}

	metadata := deployment["metadata"].(map[string]any)
	if got := metadata["namespace"]; got != "second" {
		t.Fatalf("Expected namespace second, got %v", got)
	}
}

func TestSetDeploymentImage(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: nginx:latest
`
	m, _ := NewMutator([]byte(yaml))

	err := m.SetDeploymentImage("test-deployment", "", "nginx:1.20")
	if err != nil {
		t.Fatalf("SetDeploymentImage failed: %v", err)
	}

	// Verify the image was set
	deployment := m.FindDeployment("test-deployment", "")
	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)

	if container["image"] != "nginx:1.20" {
		t.Errorf("Expected image nginx:1.20, got %v", container["image"])
	}
}

func TestSetDeploymentImageNotFound(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: nginx:latest
`
	m, _ := NewMutator([]byte(yaml))

	err := m.SetDeploymentImage("non-existent", "", "nginx:1.20")
	if err == nil {
		t.Error("SetDeploymentImage should error for non-existent deployment")
	}
}

func TestMergeDeploymentEnv(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: nginx:latest
        env:
        - name: EXISTING_VAR
          value: existing_value
`
	m, _ := NewMutator([]byte(yaml))

	envVars := map[string]string{
		"NEW_VAR":      "new_value",
		"EXISTING_VAR": "updated_value",
	}

	err := m.MergeDeploymentEnv("test-deployment", "", envVars)
	if err != nil {
		t.Fatalf("MergeDeploymentEnv failed: %v", err)
	}

	// Verify the environment variables
	deployment := m.FindDeployment("test-deployment", "")
	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)
	env := container["env"].([]any)

	if len(env) != 2 {
		t.Errorf("Expected 2 env vars, got %d", len(env))
	}

	foundExisting := false
	foundNew := false
	for _, e := range env {
		envEntry := e.(map[string]any)
		name := envEntry["name"].(string)
		value := envEntry["value"].(string)
		if name == "EXISTING_VAR" && value == "updated_value" {
			foundExisting = true
		}
		if name == "NEW_VAR" && value == "new_value" {
			foundNew = true
		}
	}

	if !foundExisting {
		t.Error("EXISTING_VAR should have been updated")
	}
	if !foundNew {
		t.Error("NEW_VAR should have been added")
	}
}

func TestMergeDeploymentEnvClearsValueFrom(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: nginx:latest
        env:
        - name: FROM_SECRET
          valueFrom:
            secretKeyRef:
              name: test-secret
              key: password
`
	m, err := NewMutator([]byte(yaml))
	if err != nil {
		t.Fatalf("NewMutator failed: %v", err)
	}

	if err := m.MergeDeploymentEnv("test-deployment", "", map[string]string{"FROM_SECRET": "literal"}); err != nil {
		t.Fatalf("MergeDeploymentEnv failed: %v", err)
	}

	deployment := m.FindDeployment("test-deployment", "")
	spec := getMap(deployment, "spec", "template", "spec")
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)
	env := container["env"].([]any)
	envEntry := env[0].(map[string]any)

	if _, exists := envEntry["valueFrom"]; exists {
		t.Fatal("valueFrom should be removed when a literal value overrides an env var")
	}
	if got := envEntry["value"]; got != "literal" {
		t.Fatalf("Expected literal value, got %v", got)
	}
}

func TestToYAML(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
`
	m, _ := NewMutator([]byte(yaml))

	output, err := m.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	if !strings.Contains(string(output), "test-deployment") {
		t.Error("Output should contain deployment name")
	}
}

func TestSimpleManifestRenderer(t *testing.T) {
	content := "image: nginx:latest"
	images := map[string]string{
		"nginx:latest": "nginx:1.20",
	}

	result := SimpleManifestRenderer(content, images)
	if result != "image: nginx:1.20" {
		t.Errorf("Expected 'image: nginx:1.20', got %q", result)
	}
}

func TestMergeDeploymentEnvIdempotent(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: nginx
`
	m, err := NewMutator([]byte(yaml))
	if err != nil {
		t.Fatalf("NewMutator failed: %v", err)
	}

	// First merge: add A=1
	err = m.MergeDeploymentEnv("test-deployment", "", map[string]string{
		"A": "1",
	})
	if err != nil {
		t.Fatalf("First MergeDeploymentEnv failed: %v", err)
	}

	// Second merge: add B=2
	err = m.MergeDeploymentEnv("test-deployment", "", map[string]string{
		"B": "2",
	})
	if err != nil {
		t.Fatalf("Second MergeDeploymentEnv failed: %v", err)
	}

	// Verify both keys are present
	deployment := m.FindDeployment("test-deployment", "")
	if deployment == nil {
		t.Fatal("Deployment not found")
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)
	env := container["env"].([]any)

	foundA := false
	foundB := false
	for _, e := range env {
		envEntry := e.(map[string]any)
		name := envEntry["name"].(string)
		value := envEntry["value"].(string)
		if name == "A" && value == "1" {
			foundA = true
		}
		if name == "B" && value == "2" {
			foundB = true
		}
	}

	if !foundA {
		t.Error("Variable A should be present after idempotent merge")
	}
	if !foundB {
		t.Error("Variable B should be present after idempotent merge")
	}
}
