// Package manifest provides structured YAML manifest mutation utilities.
// This package replaces regex-based YAML manipulation with proper structured editing.
package manifest

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mutator provides structured mutation capabilities for Kubernetes manifests.
type Mutator struct {
	docs []map[string]any
}

// NewMutator creates a new manifest mutator from YAML content.
func NewMutator(yamlContent []byte) (*Mutator, error) {
	m := &Mutator{docs: make([]map[string]any, 0)}
	decoder := yaml.NewDecoder(bytes.NewReader(yamlContent))

	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode yaml: %w", err)
		}
		if len(doc) > 0 {
			m.docs = append(m.docs, doc)
		}
	}

	return m, nil
}

// FindDeployment finds a Deployment document by name.
// Returns nil if no matching deployment is found.
func (m *Mutator) FindDeployment(name string) map[string]any {
	for _, doc := range m.docs {
		if getString(doc, "kind") == "Deployment" {
			metadata, ok := doc["metadata"].(map[string]any)
			if ok && getString(metadata, "name") == name {
				return doc
			}
		}
	}
	return nil
}

// SetDeploymentImage sets the container image for a specific container in a deployment.
// If containerName is empty, it sets the image for the first container.
func (m *Mutator) SetDeploymentImage(deploymentName, containerName, image string) error {
	deployment := m.FindDeployment(deploymentName)
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	if spec == nil {
		return fmt.Errorf("deployment %s has no pod spec", deploymentName)
	}

	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", deploymentName)
	}

	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if containerName == "" || getString(container, "name") == containerName {
			container["image"] = image
			return nil
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// SetDeploymentImagePullPolicy sets the image pull policy for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentImagePullPolicy(deploymentName, containerName, pullPolicy string) error {
	deployment := m.FindDeployment(deploymentName)
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	if spec == nil {
		return fmt.Errorf("deployment %s has no pod spec", deploymentName)
	}

	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", deploymentName)
	}

	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if containerName == "" || getString(container, "name") == containerName {
			container["imagePullPolicy"] = pullPolicy
			return nil
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// SetDeploymentArgs sets the command-line arguments for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentArgs(deploymentName, containerName string, args []string) error {
	deployment := m.FindDeployment(deploymentName)
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	if spec == nil {
		return fmt.Errorf("deployment %s has no pod spec", deploymentName)
	}

	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", deploymentName)
	}

	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if containerName == "" || getString(container, "name") == containerName {
			container["args"] = args
			return nil
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// SetDeploymentEnv sets environment variables for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentEnv(deploymentName, containerName string, envVars map[string]string) error {
	deployment := m.FindDeployment(deploymentName)
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	if spec == nil {
		return fmt.Errorf("deployment %s has no pod spec", deploymentName)
	}

	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", deploymentName)
	}

	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if containerName == "" || getString(container, "name") == containerName {
			// Build env array
			env := make([]map[string]any, 0, len(envVars))
			for name, value := range envVars {
				env = append(env, map[string]any{
					"name":  name,
					"value": value,
				})
			}
			container["env"] = env
			return nil
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// MergeDeploymentEnv merges environment variables with existing ones.
// If containerName is empty, it merges for the first container.
func (m *Mutator) MergeDeploymentEnv(deploymentName, containerName string, envVars map[string]string) error {
	deployment := m.FindDeployment(deploymentName)
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(getMap(deployment, "spec"), "template", "spec")
	if spec == nil {
		return fmt.Errorf("deployment %s has no pod spec", deploymentName)
	}

	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", deploymentName)
	}

	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if containerName == "" || getString(container, "name") == containerName {
			// Get existing env
			existingEnv := make(map[string]map[string]any)
			if existing, ok := container["env"].([]any); ok {
				for _, e := range existing {
					if envEntry, ok := e.(map[string]any); ok {
						if name := getString(envEntry, "name"); name != "" {
							existingEnv[name] = envEntry
						}
					}
				}
			}

			// Merge new values
			for name, value := range envVars {
				existingEnv[name] = map[string]any{
					"name":  name,
					"value": value,
				}
			}

			// Convert back to array
			env := make([]map[string]any, 0, len(existingEnv))
			for _, entry := range existingEnv {
				env = append(env, entry)
			}
			container["env"] = env
			return nil
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// ToYAML renders the mutated manifests back to YAML.
func (m *Mutator) ToYAML() ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	defer encoder.Close()

	for i, doc := range m.docs {
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode document %d: %w", i, err)
		}
	}

	return buf.Bytes(), nil
}

// Helper functions for navigating map[string]any structures
func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMap(m map[string]any, keys ...string) map[string]any {
	current := m
	for _, key := range keys {
		if current == nil {
			return nil
		}
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// SimpleManifestRenderer performs simple string-based replacements in a manifest.
// This is a convenience function for basic use cases where structured mutation
// is not needed. For complex Kubernetes manifest manipulation, use Mutator instead.
// Note: This uses string replacement and may not handle all YAML edge cases correctly.
// Deprecated: Use Mutator for structured, safer manifest manipulation.
func SimpleManifestRenderer(content string, images map[string]string) string {
	result := content
	for oldValue, newValue := range images {
		result = strings.ReplaceAll(result, oldValue, newValue)
	}
	return result
}
