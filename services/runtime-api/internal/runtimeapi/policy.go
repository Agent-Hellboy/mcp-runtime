package runtimeapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
)

// HandleRuntimePolicy returns the rendered gateway policy for a server the caller can administer.
func (s *AccessService) HandleRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	server := r.URL.Query().Get("server")

	if strings.TrimSpace(namespace) == "" || server == "" {
		writeAPIError(w, http.StatusBadRequest, "namespace and server parameters required")
		return
	}
	if allowed, err := s.canAdministerNamedServer(ctx, strings.TrimSpace(namespace), strings.TrimSpace(server)); err != nil {
		code, msg := sensitiveServerReadStatus(err)
		if code == http.StatusInternalServerError {
			log.Printf("runtime policy: inspect server %s/%s failed: %v", namespace, server, err)
		}
		writeAPIError(w, code, msg)
		return
	} else if !allowed {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}

	policy, err := s.accessMgr.GetServerPolicy(ctx, strings.TrimSpace(namespace), server)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "policy not found")
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

// HandleGrantItemPath handles PATCH /api/runtime/grants/{namespace}/{name} and legacy POST /disable|enable.
