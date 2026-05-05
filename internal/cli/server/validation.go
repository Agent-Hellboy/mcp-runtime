package server

import "mcp-runtime/internal/cli/core"

// validateManifestValue is a local alias so call sites in this package can use
// the short name; the implementation lives in core.
var validateManifestValue = core.ValidateManifestField

func validateServerInput(name, namespace string) (string, string, error) {
	return core.ValidateK8sNameAndNamespace("server name", core.ErrInvalidServerName, name, namespace)
}
