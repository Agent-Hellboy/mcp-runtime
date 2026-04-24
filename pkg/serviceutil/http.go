// Package serviceutil provides HTTP utilities for MCP services.
package serviceutil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the specified status code.
// It sets appropriate Content-Type headers and handles JSON marshaling errors.
// It first marshals the payload to check for encoding errors before writing headers.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to encode response"})
		return
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
