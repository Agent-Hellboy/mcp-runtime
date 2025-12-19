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
}
