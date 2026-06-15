package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"mcp-runtime-control/admin"
	"mcp-runtime-control/deployments"
	"mcp-runtime-control/internal/platformclient"
	"mcp-runtime-control/internal/platforminternal"
	runtimehandlers "mcp-runtime-control/runtime"
	"mcp-runtime/pkg/apihttp"
	"mcp-runtime/pkg/platformauth"
)

func (s *server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	platforminternal.Handler{
		Runtime:  s.runtime,
		Platform: s.platform,
		Token:    strings.TrimSpace(os.Getenv("INTERNAL_AUTH_TOKEN")),
	}.Register(mux)

	auth := s.authentic.Middleware
	adminOnly := func(h http.Handler) http.Handler {
		return auth(s.authentic.RequireRole(platformauth.RoleAdmin, h))
	}

	register := func(pattern string, handler http.Handler) {
		mux.Handle("/api/v1"+pattern, handler)
	}

	if s.runtime == nil {
		return
	}
	runtimeServer := s.runtime
	runtimeServer.SetAuditWriter(s.platform)

	register("/dashboard/summary", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleDashboardSummary(runtimeServer, w, r)
	})))
	register("/runtime/servers", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServers(runtimeServer, w, r)
	})))
	register("/runtime/tools", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTools(runtimeServer, w, r)
	})))
	register("/runtime/servers/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServerItem(runtimeServer, w, r)
	})))
	register("/runtime/server-events", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeServerEvents(runtimeServer, w, r)
	})))
	register("/runtime/observability/links", auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityLinks)))
	register("/runtime/observability/grafana/dashboard", auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityGrafanaDashboard)))
	register("/runtime/observability/prometheus/query", auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityPrometheusQuery)))
	register("/runtime/teams", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTeams(runtimeServer, w, r)
	})))
	register("/runtime/teams/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeTeamItemPath(runtimeServer, w, r)
	})))
	register("/runtime/namespaces", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeNamespaces(runtimeServer, w, r)
	})))
	register("/runtime/namespaces/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeNamespaceItem(runtimeServer, w, r)
	})))
	register("/deployments", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleDeployments(runtimeServer, w, r)
	})))
	register("/deployments/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleDeploymentItem(runtimeServer, w, r)
	})))
	register("/admin/operations", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleOperations(w, r, admin.Dependencies{
			Platform:  s.platform,
			Runtime:   runtimeServer,
			WriteJSON: apihttp.WriteJSON,
		})
	})))
	register("/admin/deployments", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments.HandleAdminDeployments(runtimeServer, w, r)
	})))
	register("/runtime/grants", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeGrants(runtimeServer, w, r)
	})))
	register("/runtime/sessions", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeSessions(runtimeServer, w, r)
	})))
	register("/runtime/adapter/sessions", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleAdapterSession(runtimeServer, w, r)
	})))
	register("/runtime/adapter/certificates", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleAdapterCertificate(runtimeServer, w, r)
	})))
	register("/runtime/registry/push", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeRegistryPush(runtimeServer, w, r)
	})))
	mux.HandleFunc("/internal/registry-push/tar", runtimeServer.HandleRegistryPushTransfer)
	go runtimeServer.ReconcileTeamNamespaceNetworkPolicies(context.Background())
	register("/runtime/components", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimeComponents(runtimeServer, w, r)
	})))
	register("/runtime/policy", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleRuntimePolicy(runtimeServer, w, r)
	})))
	register("/runtime/actions/restart", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleActionRestart(runtimeServer, w, r)
	})))
	register("/runtime/grants/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleGrantItemPath(runtimeServer, w, r)
	})))
	register("/runtime/sessions/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimehandlers.HandleSessionItemPath(runtimeServer, w, r)
	})))
	register("/user/api-keys", auth(http.HandlerFunc(s.handleUserAPIKeys)))
	register("/user/api-keys/", auth(http.HandlerFunc(s.handleUserAPIKeyItem)))
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if s.runtimeInit != "" {
		apihttp.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":                  false,
			"runtime_initialized": false,
			"runtime_error":       s.runtimeInit,
		})
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"runtime_initialized": true,
	})
}

func (s *server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.runtimeInit != "" {
		apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, s.runtimeInit)
		return
	}
	if s.runtime == nil || !s.runtime.KubernetesAvailable() {
		apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "kubernetes not available")
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleUserAPIKeys(w http.ResponseWriter, r *http.Request) {
	p, ok := platformauth.FromContext(r.Context())
	if !ok || p.UserID() == "" {
		apihttp.WriteEnvelope(w, http.StatusUnauthorized, apihttp.CodeUnauthorized, "unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.runtime.ListUserAPIKeys(r.Context(), p.UserID())
		if err != nil {
			apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to list user api keys")
			return
		}
		apihttp.WriteJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
			return
		}
		key, cleartext, err := s.runtime.CreateUserAPIKey(r.Context(), p.UserID(), req.Name)
		if err != nil {
			s.platform.WriteAudit(r.Context(), platformAuditEvent(p, "api_key_create", strings.TrimSpace(req.Name), "error", err.Error(), r))
			apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
			return
		}
		s.platform.WriteAudit(r.Context(), platformAuditEvent(p, "api_key_create", key.ID, "success", "", r))
		apihttp.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "api_key": cleartext, "one_time_key": cleartext})
	default:
		w.Header().Set("allow", "GET, POST")
		apihttp.WriteEnvelope(w, http.StatusMethodNotAllowed, apihttp.CodeMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleUserAPIKeyItem(w http.ResponseWriter, r *http.Request) {
	p, ok := platformauth.FromContext(r.Context())
	if !ok || p.UserID() == "" {
		apihttp.WriteEnvelope(w, http.StatusUnauthorized, apihttp.CodeUnauthorized, "unauthorized")
		return
	}
	keyID, allowed, valid := parseUserAPIKeyItemPath(r.Method, r.URL.Path)
	if !allowed {
		w.Header().Set("allow", "DELETE, POST")
		apihttp.WriteEnvelope(w, http.StatusMethodNotAllowed, apihttp.CodeMethodNotAllowed, "method not allowed")
		return
	}
	if !valid {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid key path")
		return
	}
	key, err := s.runtime.RevokeUserAPIKey(r.Context(), p.UserID(), keyID)
	if err != nil {
		s.platform.WriteAudit(r.Context(), platformAuditEvent(p, "api_key_revoke", keyID, "error", err.Error(), r))
		if apierrors.IsNotFound(err) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "key not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to revoke key")
		return
	}
	s.platform.WriteAudit(r.Context(), platformAuditEvent(p, "api_key_revoke", key.ID, "success", "", r))
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"key": key})
}

func platformAuditEvent(p platformauth.Principal, action, resource, status, message string, r *http.Request) platformclient.AuditEvent {
	return platformclient.AuditEvent{
		UserID:       p.UserID(),
		Action:       action,
		Resource:     resource,
		Namespace:    p.Namespace,
		Status:       status,
		Message:      message,
		ActorIP:      platformauth.RequestIP(r),
		Source:       platformauth.AuditSource(r, p),
		AuthIdentity: platformauth.AuditIdentityLabel(p),
	}
}

func parseUserAPIKeyItemPath(method, path string) (keyID string, allowed bool, valid bool) {
	const prefix = "/api/v1/user/api-keys/"
	if strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch method {
	case http.MethodDelete:
		if len(parts) != 1 || parts[0] == "" {
			return "", true, false
		}
		return parts[0], true, true
	case http.MethodPost:
		if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" {
			return "", true, false
		}
		return parts[0], true, true
	default:
		return "", false, false
	}
}
