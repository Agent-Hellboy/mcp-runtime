package main

import "net/http"

func (s *apiServer) handleAdminNamespaces(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	namespaces, err := s.platform.ListNamespaces(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list namespaces"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
}

func (s *apiServer) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	limit := clampInt(queryInt(r, "limit", 50), 1, 200)
	auditLogs, err := s.platform.ListAuditLogs(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit logs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit_logs": auditLogs})
}
