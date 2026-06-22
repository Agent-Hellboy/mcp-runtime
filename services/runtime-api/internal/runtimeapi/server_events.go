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
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	if namespace == "" || server == "" {
		writeAPIError(w, http.StatusBadRequest, "namespace and server are required")
		return
	}
	if s == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "analytics not available")
		return
	}
	if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
		writeAPIError(w, http.StatusForbidden, "forbidden namespace")
		return
	}
	if allowed, err := s.Access().canAdministerNamedServer(r.Context(), namespace, server); err != nil {
		code, msg := sensitiveServerReadStatus(err)
		if code == http.StatusInternalServerError {
			log.Printf("runtime server events: inspect server %s/%s failed: %v", namespace, server, err)
		}
		writeAPIError(w, code, msg)
		return
	} else if !allowed {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}
	if s.db == nil || s.db.Conn == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "analytics not available")
		return
	}
	events, err := s.db.QueryEventsFiltered(r.Context(), chpkg.EventFilters{
		Namespace: namespace,
		Server:    server,
		Limit:     clampServerEventsLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "query_failed")
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
