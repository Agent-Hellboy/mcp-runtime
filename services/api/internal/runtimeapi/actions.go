package runtimeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"mcp-runtime/pkg/sentinel"
)

// HandleActionRestart restarts one or all Sentinel runtime components.
func (s *RuntimeServer) HandleActionRestart(w http.ResponseWriter, r *http.Request) {
	if s.sentinelMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	var req struct {
		Component string `json:"component"`
		All       bool   `json:"all"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if req.All {
		errs := s.sentinelMgr.RestartAllComponents(ctx)
		if len(errs) > 0 {
			errMsgs := make([]string, 0, len(errs))
			for _, e := range errs {
				errMsgs = append(errMsgs, e.Error())
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error":  "some components failed to restart",
				"errors": errMsgs,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"restarted": "all",
		})
		return
	}

	if req.Component == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "component required"})
		return
	}

	// Validate component exists
	if _, err := sentinel.FindComponent(req.Component); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown component"})
		return
	}

	if err := s.sentinelMgr.RestartComponent(ctx, req.Component); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to restart component"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"component": req.Component,
	})
}

// RuntimeServer is now fully wired up through individual handler functions
