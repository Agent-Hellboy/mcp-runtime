package access

import "mcp-runtime/internal/cli/core"

func validateAccessResourceInput(name, namespace string) (string, string, error) {
	return core.ValidateK8sNameAndNamespace("resource name", nil, name, namespace)
}
