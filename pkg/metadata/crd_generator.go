package metadata

import (
	"fmt"
	"os"
	"path/filepath"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateCRD generates a Kubernetes CRD YAML file for a single server metadata entry at the given output path.
func GenerateCRD(server *ServerMetadata, outputPath string) error {
	// Convert metadata to CRD
	mcpServer := &mcpv1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "mcpruntime.org/v1alpha1",
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:    server.Image,
			ImageTag: server.ImageTag,
			Port:     server.Port,
			Replicas: server.Replicas,
		},
	}

	// Set route (ingress path)
	mcpServer.Spec.IngressPath = server.Route

	// Set service port (default 80)
	if mcpServer.Spec.ServicePort == 0 {
		mcpServer.Spec.ServicePort = 80
	}

	// Convert resources
	if server.Resources != nil {
		if server.Resources.Limits != nil {
			mcpServer.Spec.Resources.Limits = &mcpv1alpha1.ResourceList{
				CPU:    server.Resources.Limits.CPU,
				Memory: server.Resources.Limits.Memory,
			}
		}
		if server.Resources.Requests != nil {
			mcpServer.Spec.Resources.Requests = &mcpv1alpha1.ResourceList{
				CPU:    server.Resources.Requests.CPU,
				Memory: server.Resources.Requests.Memory,
			}
		}
	}

	// Convert environment variables
	if len(server.EnvVars) > 0 {
		mcpServer.Spec.EnvVars = make([]mcpv1alpha1.EnvVar, 0, len(server.EnvVars))
		for _, env := range server.EnvVars {
			mcpServer.Spec.EnvVars = append(mcpServer.Spec.EnvVars, mcpv1alpha1.EnvVar{
				Name:  env.Name,
				Value: env.Value,
			})
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(mcpServer)
	if err != nil {
		return fmt.Errorf("failed to marshal CRD: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write CRD file: %w", err)
	}

	return nil
}

// GenerateCRDsFromRegistry renders CRD YAML files for every server in a registry into outputDir.
func GenerateCRDsFromRegistry(registry *RegistryFile, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	for _, server := range registry.Servers {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.yaml", server.Name))
		if err := GenerateCRD(&server, outputPath); err != nil {
			return fmt.Errorf("failed to generate CRD for %s: %w", server.Name, err)
		}
	}

	return nil
}
