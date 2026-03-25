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

	if len(server.SecretEnvVars) > 0 {
		mcpServer.Spec.SecretEnvVars = make([]mcpv1alpha1.SecretEnvVar, 0, len(server.SecretEnvVars))
		for _, env := range server.SecretEnvVars {
			secretEnv := mcpv1alpha1.SecretEnvVar{Name: env.Name}
			if env.SecretKeyRef != nil {
				secretEnv.SecretKeyRef = &mcpv1alpha1.SecretKeyRef{
					Name: env.SecretKeyRef.Name,
					Key:  env.SecretKeyRef.Key,
				}
			}
			mcpServer.Spec.SecretEnvVars = append(mcpServer.Spec.SecretEnvVars, secretEnv)
		}
	}

	if len(server.Tools) > 0 {
		mcpServer.Spec.Tools = make([]mcpv1alpha1.ToolConfig, 0, len(server.Tools))
		for _, tool := range server.Tools {
			mcpTool := mcpv1alpha1.ToolConfig{
				Name:          tool.Name,
				Description:   tool.Description,
				RequiredTrust: mcpv1alpha1.TrustLevel(tool.RequiredTrust),
			}
			if len(tool.Labels) > 0 {
				mcpTool.Labels = make(map[string]string, len(tool.Labels))
				for k, v := range tool.Labels {
					mcpTool.Labels[k] = v
				}
			}
			mcpServer.Spec.Tools = append(mcpServer.Spec.Tools, mcpTool)
		}
	}

	if server.Auth != nil {
		mcpServer.Spec.Auth = &mcpv1alpha1.AuthConfig{
			Mode:            mcpv1alpha1.AuthMode(server.Auth.Mode),
			HumanIDHeader:   server.Auth.HumanIDHeader,
			AgentIDHeader:   server.Auth.AgentIDHeader,
			SessionIDHeader: server.Auth.SessionIDHeader,
			TokenHeader:     server.Auth.TokenHeader,
			IssuerURL:       server.Auth.IssuerURL,
			Audience:        server.Auth.Audience,
		}
	}

	if server.Policy != nil {
		mcpServer.Spec.Policy = &mcpv1alpha1.PolicyConfig{
			Mode:            mcpv1alpha1.PolicyMode(server.Policy.Mode),
			DefaultDecision: mcpv1alpha1.PolicyDecision(server.Policy.DefaultDecision),
			EnforceOn:       server.Policy.EnforceOn,
			PolicyVersion:   server.Policy.PolicyVersion,
		}
	}

	if server.Session != nil {
		mcpServer.Spec.Session = &mcpv1alpha1.SessionConfig{
			Required:            server.Session.Required,
			Store:               server.Session.Store,
			HeaderName:          server.Session.HeaderName,
			MaxLifetime:         server.Session.MaxLifetime,
			IdleTimeout:         server.Session.IdleTimeout,
			UpstreamTokenHeader: server.Session.UpstreamTokenHeader,
		}
	}

	if server.Gateway != nil {
		mcpServer.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			Enabled:     server.Gateway.Enabled,
			Image:       server.Gateway.Image,
			Port:        server.Gateway.Port,
			UpstreamURL: server.Gateway.UpstreamURL,
			StripPrefix: server.Gateway.StripPrefix,
		}
	}

	if server.Rollout != nil {
		mcpServer.Spec.Rollout = &mcpv1alpha1.RolloutConfig{
			Strategy:       mcpv1alpha1.RolloutStrategy(server.Rollout.Strategy),
			MaxUnavailable: server.Rollout.MaxUnavailable,
			MaxSurge:       server.Rollout.MaxSurge,
			CanaryReplicas: server.Rollout.CanaryReplicas,
		}
	}

	if server.Analytics != nil {
		mcpServer.Spec.Analytics = &mcpv1alpha1.AnalyticsConfig{
			Enabled:   server.Analytics.Enabled,
			IngestURL: server.Analytics.IngestURL,
			Source:    server.Analytics.Source,
			EventType: server.Analytics.EventType,
		}
		if server.Analytics.APIKeySecretRef != nil {
			mcpServer.Spec.Analytics.APIKeySecretRef = &mcpv1alpha1.SecretKeyRef{
				Name: server.Analytics.APIKeySecretRef.Name,
				Key:  server.Analytics.APIKeySecretRef.Key,
			}
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
