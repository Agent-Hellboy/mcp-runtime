package main

import (
	"net/http"
	"time"

	"mcp-runtime/pkg/serviceutil"
)

// configStatus is the sanitized view of the active policy snapshot exposed at
// /config/status. It deliberately omits the policy body (grants, sessions,
// identities) and reports only contract metadata and reload state.
type configStatus struct {
	Ready           bool   `json:"ready"`
	SchemaVersion   string `json:"schema_version,omitempty"`
	Revision        string `json:"revision,omitempty"`
	LoadedAt        string `json:"loaded_at,omitempty"`
	LastReloadError string `json:"last_reload_error,omitempty"`
}

// handleReady is a readiness probe that succeeds only after the first valid
// policy snapshot has been activated. It stays ready across later failed
// reloads because the last-known-good policy is retained.
func (s *gatewayServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.loadPolicySnapshot()
	if !snapshot.Ready {
		payload := map[string]any{"status": "not_ready", "reason": "policy_not_loaded"}
		if snapshot.Err != nil {
			payload["last_reload_error"] = snapshot.Err.Error()
		}
		serviceutil.WriteJSON(w, http.StatusServiceUnavailable, payload)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// handleConfigStatus reports the sanitized applied schema version, revision,
// load timestamp, and last reload error for the active policy snapshot.
func (s *gatewayServer) handleConfigStatus(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.loadPolicySnapshot()
	status := configStatus{
		Ready:    snapshot.Ready,
		Revision: snapshot.Revision,
	}
	if snapshot.Policy != nil {
		status.SchemaVersion = snapshot.Policy.SchemaVersion
	}
	if !snapshot.LoadedAt.IsZero() {
		status.LoadedAt = snapshot.LoadedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Err != nil {
		status.LastReloadError = snapshot.Err.Error()
	}
	serviceutil.WriteJSON(w, http.StatusOK, status)
}
