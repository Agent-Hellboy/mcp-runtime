package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"mcp-sentinel-api/admin"
	"mcp-sentinel-api/deployments"
	"mcp-sentinel-api/internal/runtimeapi"
	runtimehandlers "mcp-sentinel-api/runtime"
)

// registerRoutes wires the API HTTP surface onto mux.
func (s *apiServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/registry/authz", s.handleRegistryAuthz)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/oidc", s.handleOIDCLogin)
	mux.HandleFunc("/api/auth/signup", s.handleSignup)
	mux.Handle("/api/users", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleUsers))))
	mux.Handle("/api/events", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleEvents))))
	mux.Handle("/api/stats", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleStats))))
	mux.Handle("/api/sources", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleSources))))
	mux.Handle("/api/event-types", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleEventTypes))))
	mux.Handle("/api/analytics/usage", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleAnalyticsUsage))))
	mux.Handle("/api/events/filter", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(s.handleEventsFilter))))
	mux.Handle("/api/auth/me", s.auth(http.HandlerFunc(s.handleAuthMe)))
	mux.Handle("/api/user/analytics/usage", s.auth(http.HandlerFunc(s.handleUserAnalyticsUsage)))
	mux.Handle("/api/user/registry-credentials", s.auth(http.HandlerFunc(s.handleRegistryCredentials)))
	mux.Handle("/api/user/registry-credentials/", s.auth(http.HandlerFunc(s.handleRegistryCredentialItem)))
	mux.Handle("/api/user/activity/image-publish", s.auth(http.HandlerFunc(s.handleUserImagePublishActivity)))

	s.registerRuntimeRoutes(mux)
}

func (s *apiServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if strings.TrimSpace(s.runtimeInit) != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":                  false,
			"runtime_initialized": false,
			"runtime_error":       s.runtimeInit,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"runtime_initialized": true,
	})
}

func (s *apiServer) registerRuntimeRoutes(mux *http.ServeMux) {
	runtimeServer, err := runtimeapi.NewRuntimeServer(s.db, s.dbName, s.apiKeys, s.platform)
	if err != nil {
		s.runtimeInit = err.Error()
		log.Printf("ERROR: runtime server initialization failed: %v", err)
		return
	}
	s.runtime = runtimeServer
	runtimeServer.SetAuditWriter(s.platform)
	if s.userKeys == nil {
		s.userKeys = runtimeServer
	}

	mux.Handle("/api/dashboard/summary", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleDashboardSummary(runtimeServer, w, r)
	}))))
	mux.Handle("/api/runtime/servers", s.authOrPublicCatalog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServers(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/tools", s.authOrPublicCatalog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTools(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/servers/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServerItem(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/server-events", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServerEvents(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/observability/links", s.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityLinks)))
	mux.Handle("/api/runtime/observability/grafana/dashboard", s.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityGrafanaDashboard)))
	mux.Handle("/api/runtime/observability/prometheus/query", s.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityPrometheusQuery)))
	mux.Handle("/api/runtime/teams", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTeams(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/teams/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTeamItemPath(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/namespaces", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeNamespaces(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/namespaces/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeNamespaceItem(runtimeServer, w, r)
	})))
	mux.Handle("/api/deployments", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleDeployments(runtimeServer, w, r)
	})))
	mux.Handle("/api/deployments/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleDeploymentItem(runtimeServer, w, r)
	})))
	mux.Handle("/api/admin/namespaces", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleNamespaces(w, r, admin.Dependencies{
			Platform:  s.platform,
			WriteJSON: writeJSON,
		})
	}))))
	mux.Handle("/api/admin/audit", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleAudit(w, r, admin.Dependencies{
			Platform:  s.platform,
			WriteJSON: writeJSON,
		})
	}))))
	mux.Handle("/api/admin/operations", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleOperations(w, r, admin.Dependencies{
			Platform:  s.platform,
			Runtime:   s.runtime,
			WriteJSON: writeJSON,
		})
	}))))
	mux.Handle("/api/admin/deployments", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleAdminDeployments(runtimeServer, w, r)
	}))))
	mux.Handle("/api/runtime/grants", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeGrants(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/sessions", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeSessions(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/adapter/sessions", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleAdapterSession(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/registry/push", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeRegistryPush(runtimeServer, w, r)
	})))
	mux.HandleFunc("/internal/registry-push/tar", runtimeServer.HandleRegistryPushTransfer)
	go runtimeServer.ReconcileTeamNamespaceNetworkPolicies(context.Background())
	mux.Handle("/api/runtime/components", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeComponents(runtimeServer, w, r)
	}))))
	mux.Handle("/api/runtime/policy", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimePolicy(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/actions/restart", s.auth(s.requireRole(roleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleActionRestart(runtimeServer, w, r)
	}))))
	mux.Handle("/api/runtime/grants/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleGrantItemPath(runtimeServer, w, r)
	})))
	mux.Handle("/api/runtime/sessions/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleSessionItemPath(runtimeServer, w, r)
	})))
	mux.Handle("/api/user/api-keys", s.auth(http.HandlerFunc(s.handleUserAPIKeys)))
	mux.Handle("/api/user/api-keys/", s.auth(http.HandlerFunc(s.handleUserAPIKeyItem)))
}
