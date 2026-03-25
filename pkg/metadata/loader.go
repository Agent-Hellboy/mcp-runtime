package metadata

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadFromFile reads a single registry YAML file from disk and applies default values.
func LoadFromFile(filePath string) (*RegistryFile, error) {
	cleanPath := filepath.Clean(filePath)
	// #nosec G304 -- path is user-supplied for local metadata loading.
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var registry RegistryFile
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Set defaults
	for i := range registry.Servers {
		setDefaults(&registry.Servers[i])
	}

	return &registry, nil
}

// LoadFromDirectory aggregates all .yaml/.yml registry files in a directory into one registry object.
func LoadFromDirectory(dirPath string) (*RegistryFile, error) {
	files, err := filepath.Glob(filepath.Join(dirPath, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	ymlFiles, err := filepath.Glob(filepath.Join(dirPath, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	files = append(files, ymlFiles...)

	var allServers []ServerMetadata
	for _, file := range files {
		registry, err := LoadFromFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", file, err)
		}
		allServers = append(allServers, registry.Servers...)
	}

	return &RegistryFile{
		Version: "v1",
		Servers: allServers,
	}, nil
}

func setDefaults(server *ServerMetadata) {
	// Set default image if not provided (will be updated by build command)
	if server.Image == "" {
		server.Image = fmt.Sprintf("registry.registry.svc.cluster.local:5000/%s", server.Name)
	}
	if server.ImageTag == "" {
		server.ImageTag = "latest"
	}
	if server.Route == "" {
		server.Route = fmt.Sprintf("/%s/mcp", server.Name)
	} else if server.Route[0] != '/' {
		server.Route = "/" + server.Route
	}
	if server.Port == 0 {
		server.Port = 8088
	}
	if server.Replicas == nil {
		replicas := int32(1)
		server.Replicas = &replicas
	}
	if server.Namespace == "" {
		server.Namespace = "mcp-servers"
	}
	if server.Auth != nil {
		if server.Auth.Mode == "" {
			server.Auth.Mode = AuthModeHeader
		}
		if server.Auth.HumanIDHeader == "" {
			server.Auth.HumanIDHeader = "X-MCP-Human-ID"
		}
		if server.Auth.AgentIDHeader == "" {
			server.Auth.AgentIDHeader = "X-MCP-Agent-ID"
		}
		if server.Auth.SessionIDHeader == "" {
			server.Auth.SessionIDHeader = "X-MCP-Agent-Session"
		}
		if server.Auth.TokenHeader == "" {
			server.Auth.TokenHeader = "Authorization"
		}
	}
	if server.Policy != nil {
		if server.Policy.Mode == "" {
			server.Policy.Mode = PolicyModeAllowList
		}
		if server.Policy.DefaultDecision == "" {
			server.Policy.DefaultDecision = PolicyDecisionDeny
		}
		if server.Policy.EnforceOn == "" {
			server.Policy.EnforceOn = "call_tool"
		}
		if server.Policy.PolicyVersion == "" {
			server.Policy.PolicyVersion = "v1"
		}
	}
	if server.Session != nil {
		if server.Session.Store == "" {
			server.Session.Store = "kubernetes"
		}
		if server.Session.HeaderName == "" {
			server.Session.HeaderName = "X-MCP-Agent-Session"
		}
		if server.Session.MaxLifetime == "" {
			server.Session.MaxLifetime = "24h"
		}
		if server.Session.IdleTimeout == "" {
			server.Session.IdleTimeout = "1h"
		}
		if server.Session.UpstreamTokenHeader == "" {
			server.Session.UpstreamTokenHeader = "Authorization"
		}
	}
	for i := range server.Tools {
		if server.Tools[i].RequiredTrust == "" {
			server.Tools[i].RequiredTrust = TrustLevelLow
		}
	}
	if server.Gateway != nil && server.Gateway.Enabled {
		if server.Gateway.Port == 0 {
			server.Gateway.Port = 8091
		}
		if server.Gateway.UpstreamURL == "" {
			server.Gateway.UpstreamURL = fmt.Sprintf("http://127.0.0.1:%d", server.Port)
		}
	}
	if server.Analytics != nil && server.Analytics.Enabled {
		if server.Analytics.Source == "" {
			server.Analytics.Source = server.Name
		}
		if server.Analytics.EventType == "" {
			server.Analytics.EventType = "mcp.request"
		}
	}
	if server.Rollout != nil {
		if server.Rollout.Strategy == "" {
			server.Rollout.Strategy = RolloutStrategyRollingUpdate
		}
		if server.Rollout.MaxUnavailable == "" {
			server.Rollout.MaxUnavailable = "25%"
		}
		if server.Rollout.MaxSurge == "" {
			server.Rollout.MaxSurge = "25%"
		}
	}
}
