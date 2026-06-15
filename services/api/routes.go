package main

import (
	"net/http"
)

// registerRoutes wires the shrinking monolith HTTP surface onto mux.
// Platform identity/auth/registry routes live in services/platform-api.
func (s *apiServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
}

func (s *apiServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
