package openapi

import (
	"net/http"
)

// ServeYAML writes an embedded OpenAPI document with the standard YAML content type.
func ServeYAML(w http.ResponseWriter, spec []byte) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(spec)
}
