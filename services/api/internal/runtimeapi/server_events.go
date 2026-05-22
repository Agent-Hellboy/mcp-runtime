package runtimeapi

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	chpkg "mcp-runtime/pkg/clickhouse"
)

// HandleRuntimeServerEvents returns recent analytics events for a server the caller can administer.
func (s *RuntimeServer) HandleRuntimeServerEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	if namespace == "" || server == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace and server are required"})
		return
	}
	if s == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "analytics not available"})
		return
	}
	if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
		return
	}
	if allowed, err := s.canAdministerNamedServer(r.Context(), namespace, server); err != nil {
		code, msg := sensitiveServerReadStatus(err)
		if code == http.StatusInternalServerError {
			log.Printf("runtime server events: inspect server %s/%s failed: %v", namespace, server, err)
		}
		writeJSON(w, code, map[string]string{"error": msg})
		return
	} else if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}
	if s.db == nil || s.db.Conn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "analytics not available"})
		return
	}
	events, err := s.db.QueryEventsFiltered(r.Context(), chpkg.EventFilters{
		Namespace: namespace,
		Server:    server,
		Limit:     clampServerEventsLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func clampServerEventsLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 20
	}
	if limit < 1 {
		return 1
	}
	if limit > 100 {
		return 100
	}
	return limit
}
