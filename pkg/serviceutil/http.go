// Package serviceutil provides HTTP utilities for MCP services.
package serviceutil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the specified status code.
// It sets appropriate Content-Type headers and handles JSON marshaling errors.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
