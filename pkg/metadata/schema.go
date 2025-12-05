package metadata

// ServerMetadata defines the metadata for an MCP server
type ServerMetadata struct {
	// Name is the unique name of the MCP server
	Name string `yaml:"name" json:"name"`

	// Image is the container image for the server
	Image string `yaml:"image" json:"image"`

	// ImageTag is the tag of the container image (defaults to "latest")
	ImageTag string `yaml:"imageTag,omitempty" json:"imageTag,omitempty"`

	// Route is the route path for the server (defaults to name/mcp)
	Route string `yaml:"route,omitempty" json:"route,omitempty"`

	// Port is the port the container listens on (defaults to 8088)
	Port int32 `yaml:"port,omitempty" json:"port,omitempty"`

	// Replicas is the number of desired replicas (defaults to 1)
	Replicas *int32 `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Resources defines resource limits and requests
	Resources *ResourceRequirements `yaml:"resources,omitempty" json:"resources,omitempty"`

	// EnvVars are environment variables to pass to the container
	EnvVars []EnvVar `yaml:"envVars,omitempty" json:"envVars,omitempty"`

	// Namespace is the Kubernetes namespace (defaults to "mcp-servers")
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// ResourceRequirements defines resource limits and requests
type ResourceRequirements struct {
	Limits   *ResourceList `yaml:"limits,omitempty" json:"limits,omitempty"`
	Requests *ResourceList `yaml:"requests,omitempty" json:"requests,omitempty"`
}

// ResourceList defines CPU and memory resources
type ResourceList struct {
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// EnvVar defines an environment variable
type EnvVar struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// RegistryFile represents the complete registry/metadata file
type RegistryFile struct {
	// Version of the metadata format
	Version string `yaml:"version" json:"version"`

	// Servers is a list of MCP server definitions
	Servers []ServerMetadata `yaml:"servers" json:"servers"`
}
