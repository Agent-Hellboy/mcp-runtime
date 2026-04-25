// Package manifest provides structured YAML manifest mutation utilities.
// This package replaces regex-based YAML manipulation with proper structured editing.
package manifest

import (
	"bytes"
	"fmt"
	"io"
	"sort"
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

// FindDeployment finds a Deployment document by name and optionally namespace.
// If namespace is empty, it matches any namespace.
// Returns nil if no matching deployment is found.
func (m *Mutator) FindDeployment(name, namespace string) map[string]any {
	for _, doc := range m.docs {
		if getString(doc, "kind") == "Deployment" {
			metadata, ok := doc["metadata"].(map[string]any)
			if !ok {
				continue
			}
			if getString(metadata, "name") != name {
				continue
			}
			// If namespace is specified, it must match
			if namespace != "" && getString(metadata, "namespace") != namespace {
				continue
			}
			return doc
		}
	}
	return nil
}

// withContainer is a helper that finds a deployment and container, then invokes a callback
// to mutate the container. This eliminates duplicated scaffolding across SetDeployment* methods.
func (m *Mutator) withContainer(deploymentName, containerName string, fn func(map[string]any) error) error {
	deployment := m.FindDeployment(deploymentName, "")
	if deployment == nil {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	spec := getMap(deployment, "spec", "template", "spec")
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
			return fn(container)
		}
	}

	if containerName != "" {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	return fmt.Errorf("no containers found in deployment %s", deploymentName)
}

// SetDeploymentImage sets the container image for a specific container in a deployment.
// If containerName is empty, it sets the image for the first container.
func (m *Mutator) SetDeploymentImage(deploymentName, containerName, image string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		container["image"] = image
		return nil
	})
}

// SetDeploymentImagePullPolicy sets the image pull policy for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentImagePullPolicy(deploymentName, containerName, pullPolicy string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		container["imagePullPolicy"] = pullPolicy
		return nil
	})
}

// SetDeploymentArgs sets the command-line arguments for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentArgs(deploymentName, containerName string, args []string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		container["args"] = args
		return nil
	})
}

// MergeDeploymentArgs merges command-line arguments with existing ones.
// If containerName is empty, it merges for the first container.
// Args with the same key (extracted from the flag name) will be replaced;
// new args will be appended.
func (m *Mutator) MergeDeploymentArgs(deploymentName, containerName string, newArgs []string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		// Get existing args
		existingArgs := []string{}
		if existing, ok := container["args"].([]any); ok {
			for _, arg := range existing {
				if argStr, ok := arg.(string); ok {
					existingArgs = append(existingArgs, argStr)
				}
			}
		}

		existingUnits := groupArgTokens(existingArgs)
		newUnits := groupArgTokens(newArgs)

		// Build index by arg key (flag name)
		argIndex := make(map[string]int)
		for i, arg := range existingUnits {
			key := extractArgKey(arg)
			argIndex[key] = i
		}

		// Merge new args: replace if key exists, append otherwise
		mergedUnits := append(make([]string, 0, len(existingUnits)+len(newUnits)), existingUnits...)

		for _, newArg := range newUnits {
			key := extractArgKey(newArg)
			if idx, exists := argIndex[key]; exists {
				// Replace existing arg
				mergedUnits[idx] = newArg
			} else {
				// Append new arg
				argIndex[key] = len(mergedUnits)
				mergedUnits = append(mergedUnits, newArg)
			}
		}

		// Convert to []any for YAML
		container["args"] = flattenArgUnits(mergedUnits)
		return nil
	})
}

// extractArgKey extracts the flag name from a command-line argument.
// For example, "--leader-elect=true" and "--leader-elect true" return "--leader-elect".
func extractArgKey(arg string) string {
	arg, _, _ = strings.Cut(strings.TrimSpace(arg), " ")
	if idx := strings.Index(arg, "="); idx > 0 {
		return arg[:idx]
	}
	return arg
}

func groupArgTokens(args []string) []string {
	grouped := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			grouped = append(grouped, arg+" "+args[i+1])
			i++
			continue
		}
		grouped = append(grouped, arg)
	}
	return grouped
}

func flattenArgUnits(units []string) []any {
	args := make([]any, 0, len(units))
	for _, unit := range units {
		if flag, value, ok := strings.Cut(unit, " "); ok {
			args = append(args, flag, value)
			continue
		}
		args = append(args, unit)
	}
	return args
}

// SetDeploymentEnv sets environment variables for a specific container.
// If containerName is empty, it sets for the first container.
func (m *Mutator) SetDeploymentEnv(deploymentName, containerName string, envVars map[string]string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		// Build env array, sorted by key for deterministic output
		names := make([]string, 0, len(envVars))
		for name := range envVars {
			names = append(names, name)
		}
		sort.Strings(names)

		env := make([]any, 0, len(envVars))
		for _, name := range names {
			env = append(env, map[string]any{
				"name":  name,
				"value": envVars[name],
			})
		}
		container["env"] = env
		return nil
	})
}

// MergeDeploymentEnv merges environment variables with existing ones.
// If containerName is empty, it merges for the first container.
func (m *Mutator) MergeDeploymentEnv(deploymentName, containerName string, envVars map[string]string) error {
	return m.withContainer(deploymentName, containerName, func(container map[string]any) error {
		// Build ordered slice of env entries and name->index map
		orderedEnv := make([]any, 0)
		nameToIndex := make(map[string]int)

		if existing, ok := container["env"].([]any); ok {
			for _, e := range existing {
				if envEntry, ok := e.(map[string]any); ok {
					if name := getString(envEntry, "name"); name != "" {
						nameToIndex[name] = len(orderedEnv)
						orderedEnv = append(orderedEnv, envEntry)
					}
				}
			}
		}

		// Merge new values: update in-place if exists, append if new
		newNames := make([]string, 0)
		for name, value := range envVars {
			if idx, exists := nameToIndex[name]; exists {
				// Update existing entry in-place
				if envEntry, ok := orderedEnv[idx].(map[string]any); ok {
					delete(envEntry, "valueFrom")
					envEntry["value"] = value
				}
			} else {
				// Track new names to append
				newNames = append(newNames, name)
			}
		}

		// Append new entries in deterministic order
		if len(newNames) > 0 {
			sort.Strings(newNames)
			for _, name := range newNames {
				orderedEnv = append(orderedEnv, map[string]any{
					"name":  name,
					"value": envVars[name],
				})
			}
		}

		container["env"] = orderedEnv
		return nil
	})
}

// ToYAML renders the mutated manifests back to YAML.
func (m *Mutator) ToYAML() ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)

	for i, doc := range m.docs {
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode document %d: %w", i, err)
		}
	}

	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
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
