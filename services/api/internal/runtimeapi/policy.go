package runtimeapi

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func (s *RuntimeServer) HandleRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	server := r.URL.Query().Get("server")

	if strings.TrimSpace(namespace) == "" || server == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace and server parameters required"})
		return
	}

	policy, err := s.accessMgr.GetServerPolicy(ctx, strings.TrimSpace(namespace), server)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

// HandleGrantItemPath handles POST /api/runtime/grants/{namespace}/{name}/disable|enable
