# pkg/metadata

## schema.go
- L1: Declares `package metadata` defining the in-memory representation of MCP server metadata files.
- L3-L46: `ServerMetadata` struct describes required/optional fields (name, image, tag, route, ports, replicas, resources, env vars, namespace) with YAML/JSON tags for serialization.
- L48-L56: `ResourceRequirements` wrapper for optional limit/request maps.
- L58-L63: `ResourceList` holding CPU and memory strings.
- L65-L70: `EnvVar` struct for name/value pairs.
- L72-L81: `RegistryFile` root struct containing metadata file version and list of server entries.

## loader.go
- L1: Package declaration and imports for filesystem/YAML parsing utilities.
- L9-L26: `LoadFromFile` reads a YAML file into `RegistryFile`, applies per-server defaults via `setDefaults`, and returns the populated struct.
- L28-L55: `LoadFromDirectory` loads all `.yaml`/`.yml` files in a directory, aggregates servers from each file, and returns a combined registry (version `v1`).
- L57-L88: `setDefaults` fills missing fields on `ServerMetadata`: default image pointing at the internal registry, default tag `latest`, route prefixed with `/`, port `8088`, replicas `1`, and namespace `mcp-servers`.

## crd_generator.go
- L1: Package declaration and imports for file IO, YAML, and API types.
- L11-L49: `GenerateCRD` converts a single `ServerMetadata` entry into an MCPServer CRD object, setting type/meta, namespace, spec fields, ingress path, default service port, resources, and env vars, then marshals to YAML.
- L51-L70: Ensures the output directory exists and writes the YAML manifest to the requested path.
- L72-L85: `GenerateCRDsFromRegistry` walks all servers in a registry and emits one CRD YAML per server into an output directory, creating it if missing.

## Tests
- `loader_test.go` validates defaulting and error handling for both file and directory loading paths.
- `schema.go` is also exercised indirectly through generator and loader tests.
