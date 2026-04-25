package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sentinelaccess "mcp-runtime/pkg/access"
	chpkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/sentinel"
	"mcp-runtime/pkg/serviceutil"
)

// RuntimeServer extends apiServer with Kubernetes and enhanced ClickHouse capabilities.
type RuntimeServer struct {
	db          *chpkg.Client
	clickhouse  clickhouse.Conn
	dbName      string
	apiKeys     map[string]struct{}
	k8sClients  *k8sclient.Clients
	accessMgr   *sentinelaccess.Manager
	sentinelMgr *sentinel.Manager
}

// NewRuntimeServer creates a runtime server with Kubernetes access.
func NewRuntimeServer(db clickhouse.Conn, dbName string, apiKeys map[string]struct{}) (*RuntimeServer, error) {
	// Create ClickHouse client wrapper
	chClient := &chpkg.Client{
		Conn:   db,
		DBName: dbName,
	}

	// Initialize Kubernetes clients (in-cluster or kubeconfig)
	k8sClients, err := k8sclient.New()
	if err != nil {
		// Log warning but don't fail - some endpoints will be unavailable
		fmt.Printf("[WARN] Kubernetes client initialization failed: %v\n", err)
		k8sClients = nil
	}

	var accessMgr *sentinelaccess.Manager
	var sentinelMgr *sentinel.Manager

	if k8sClients != nil {
		accessMgr = sentinelaccess.NewManager(k8sClients.Dynamic, k8sClients.Clientset)
		sentinelMgr = sentinel.NewManager(k8sClients.Clientset)
	}

	return &RuntimeServer{
		db:          chClient,
		clickhouse:  db,
		dbName:      dbName,
		apiKeys:     apiKeys,
		k8sClients:  k8sClients,
		accessMgr:   accessMgr,
		sentinelMgr: sentinelMgr,
	}, nil
}

// handleDashboardSummary returns overview statistics for the dashboard.
func (s *RuntimeServer) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get analytics data from ClickHouse
	summary, err := s.db.QueryDashboardSummary(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query dashboard summary"})
		return
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

// handleRuntimeServers returns MCP server deployments.
func (s *RuntimeServer) handleRuntimeServers(w http.ResponseWriter, r *http.Request) {
	if s.k8sClients == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if namespace == "" {
		namespace = "mcp-servers"
	}

	// MCPServer deployments are reconciled by the runtime operator into the mcp-servers
	// namespace and labeled as managed-by=mcp-runtime with a stable/canary rollout track.
	// The UI needs the stable server set, not every deployment in the cluster.
	deployments, err := s.k8sClients.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=mcp-runtime,mcpruntime.org/rollout-track=stable",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list servers"})
		return
	}

	type ServerInfo struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Ready     string            `json:"ready"`
		Status    string            `json:"status"`
		Labels    map[string]string `json:"labels"`
		Age       string            `json:"age"`
	}

	servers := make([]ServerInfo, 0, len(deployments.Items))
	for _, d := range deployments.Items {
		ready := "0/0"
		status := "NotReady"
		if d.Spec.Replicas != nil {
			ready = fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, *d.Spec.Replicas)
			if d.Status.ReadyReplicas == *d.Spec.Replicas && *d.Spec.Replicas > 0 {
				status = "Ready"
			} else if d.Status.ReadyReplicas > 0 {
				status = "Degraded"
			}
		}

		servers = append(servers, ServerInfo{
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     ready,
			Status:    status,
			Labels:    d.Labels,
			Age:       d.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"servers": servers})
}

// handleRuntimeGrants returns MCPAccessGrant resources.
func (s *RuntimeServer) handleRuntimeGrants(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := r.URL.Query().Get("namespace")
	grants, err := s.accessMgr.ListGrants(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list grants"})
		return
	}

	summaries := make([]sentinelaccess.GrantSummary, 0, len(grants.Items))
	for _, g := range grants.Items {
		summaries = append(summaries, sentinelaccess.ToGrantSummary(g))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": summaries})
}

// handleRuntimeSessions returns MCPAgentSession resources.
func (s *RuntimeServer) handleRuntimeSessions(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := r.URL.Query().Get("namespace")
	sessions, err := s.accessMgr.ListSessions(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sessions"})
		return
	}

	summaries := make([]sentinelaccess.SessionSummary, 0, len(sessions.Items))
	for _, sess := range sessions.Items {
		summaries = append(summaries, sentinelaccess.ToSessionSummary(sess))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": summaries})
}

// handleRuntimeComponents returns Sentinel component health.
func (s *RuntimeServer) handleRuntimeComponents(w http.ResponseWriter, r *http.Request) {
	if s.sentinelMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Only return core components for the dashboard
	statuses, err := s.sentinelMgr.GetCoreComponentStatuses(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get component statuses"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"components": statuses})
}

// handleRuntimePolicy returns rendered policy for a server.
func (s *RuntimeServer) handleRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := r.URL.Query().Get("namespace")
	server := r.URL.Query().Get("server")

	if namespace == "" || server == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace and server parameters required"})
		return
	}

	policy, err := s.accessMgr.GetServerPolicy(ctx, namespace, server)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

// handleGrantTogglePath handles POST /api/runtime/grants/{namespace}/{name}/disable|enable
func (s *RuntimeServer) handleGrantTogglePath(w http.ResponseWriter, r *http.Request) {
	params, err := serviceutil.ExtractGrantActionParams(r, "/api/runtime/grants/")
	if err != nil {
		if errors.Is(err, serviceutil.ErrMethodNotAllowed) {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	disable := !serviceutil.IsActionEnabled(params.Action)
	s.handleGrantToggle(w, r, params.Namespace, params.Name, disable)
}

func (s *RuntimeServer) handleGrantToggle(w http.ResponseWriter, r *http.Request, namespace, name string, disable bool) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var err error
	if disable {
		err = s.accessMgr.DisableGrant(ctx, name, namespace)
	} else {
		err = s.accessMgr.EnableGrant(ctx, name, namespace)
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update grant"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
		"disabled":  disable,
	})
}

// handleSessionTogglePath handles POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke
func (s *RuntimeServer) handleSessionTogglePath(w http.ResponseWriter, r *http.Request) {
	params, err := serviceutil.ExtractSessionActionParams(r, "/api/runtime/sessions/")
	if err != nil {
		if errors.Is(err, serviceutil.ErrMethodNotAllowed) {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	revoke := !serviceutil.IsActionEnabled(params.Action)
	s.handleSessionToggle(w, r, params.Namespace, params.Name, revoke)
}

func (s *RuntimeServer) handleSessionToggle(w http.ResponseWriter, r *http.Request, namespace, name string, revoke bool) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var err error
	if revoke {
		err = s.accessMgr.RevokeSession(ctx, name, namespace)
	} else {
		err = s.accessMgr.UnrevokeSession(ctx, name, namespace)
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
		"revoked":   revoke,
	})
}

// handleActionRestart handles restart requests for components.
func (s *RuntimeServer) handleActionRestart(w http.ResponseWriter, r *http.Request) {
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
