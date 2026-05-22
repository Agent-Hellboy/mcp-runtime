package runtimeapi

import (
	"context"
	"net/http"
	"time"
)

// HandleDashboardSummary returns analytics and live control-plane counters for the Sentinel dashboard.
func (s *RuntimeServer) HandleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get analytics data from ClickHouse
	summary, err := s.db.QueryDashboardSummary(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query dashboard summary"})
		return
	}
	if control := s.controlPlane(); control != nil {
		if result, err := control.ListServers(ctx, ""); err == nil {
			summary.ActiveServers = len(result.Servers)
		}
	}

	// Get grants and sessions counts from Kubernetes if available
	if s.accessMgr != nil {
		grants, err := s.accessMgr.ListGrants(ctx, "")
		if err == nil {
			activeGrants := 0
			for _, g := range grants.Items {
				if !g.Spec.Disabled {
					activeGrants++
				}
			}
			summary.ActiveGrants = activeGrants
		}

		sessions, err := s.accessMgr.ListSessions(ctx, "")
		if err == nil {
			activeSessions := 0
			for _, sess := range sessions.Items {
				if !sess.Spec.Revoked {
					activeSessions++
				}
			}
			summary.ActiveSessions = activeSessions
		}
	}

	writeJSON(w, http.StatusOK, summary)
}

// HandleRuntimeComponents returns admin-only health details for Sentinel platform components.
func (s *RuntimeServer) HandleRuntimeComponents(w http.ResponseWriter, r *http.Request) {
	if p, ok := principalFromContext(r.Context()); !ok || p.Role != roleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if s.sentinelMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	statuses, err := s.sentinelMgr.GetAllComponentStatuses(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get component statuses"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"components": statuses})
}
