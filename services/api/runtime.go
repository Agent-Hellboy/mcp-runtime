package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	chpkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/sentinel"
	"mcp-runtime/pkg/serviceutil"
)

var mcpServerGVR = schema.GroupVersionResource{
	Group:    sentinelaccess.APIGroup,
	Version:  sentinelaccess.APIVersion,
	Resource: sentinelaccess.MCPServerResource,
}

type accessGrantRequest struct {
	Name          string                         `json:"name"`
	Namespace     string                         `json:"namespace"`
	ServerRef     sentinelaccess.ServerReference `json:"serverRef"`
	Subject       sentinelaccess.SubjectRef      `json:"subject"`
	MaxTrust      sentinelaccess.TrustLevel      `json:"maxTrust"`
	PolicyVersion string                         `json:"policyVersion"`
	Disabled      *bool                          `json:"disabled,omitempty"`
	ToolRules     []sentinelaccess.ToolRule      `json:"toolRules"`
}

type accessSessionRequest struct {
	Name           string                         `json:"name"`
	Namespace      string                         `json:"namespace"`
	ServerRef      sentinelaccess.ServerReference `json:"serverRef"`
	Subject        sentinelaccess.SubjectRef      `json:"subject"`
	ConsentedTrust sentinelaccess.TrustLevel      `json:"consentedTrust"`
	ExpiresAt      *metav1.Time                   `json:"expiresAt"`
	Revoked        *bool                          `json:"revoked,omitempty"`
	PolicyVersion  string                         `json:"policyVersion"`
}

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
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin {
		switch {
		case namespace == "":
			// Non-admin users should still see the shared MCP catalog by default.
			namespace = "mcp-servers"
		case namespace == "mcp-servers":
			// Allow explicit shared catalog lookup.
		case p.Namespace != "" && namespace == p.Namespace:
			// Allow user's private namespace lookup.
		default:
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
			return
		}
	}
	if namespace == "" {
		namespace = "mcp-servers"
	}

	serverObjects, err := s.k8sClients.Dynamic.Resource(mcpServerGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		deployments, deployErr := s.k8sClients.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=mcp-runtime,mcpruntime.org/rollout-track=stable",
		})
		deploymentStatus := map[string]serverDeploymentStatus{}
		if deployErr == nil {
			for _, d := range deployments.Items {
				deploymentStatus[d.Name] = statusForDeployment(d)
			}
		}
		servers := make([]serverInfo, 0, len(serverObjects.Items))
		for _, obj := range serverObjects.Items {
			var mcpServer mcpv1alpha1.MCPServer
			if convertErr := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &mcpServer); convertErr != nil {
				log.Printf("runtime servers: convert MCPServer %s/%s: %v", obj.GetNamespace(), obj.GetName(), convertErr)
				continue
			}
			servers = append(servers, serverInfoFromMCPServer(mcpServer, deploymentStatus[mcpServer.Name]))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"servers": servers})
		return
	}
	if !apierrors.IsNotFound(err) {
		log.Printf("runtime servers: list MCPServers failed: %v", err)
	}

	// Fall back to stable deployments for older clusters where the MCPServer CRD is not available.
	deployments, err := s.k8sClients.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=mcp-runtime,mcpruntime.org/rollout-track=stable",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list servers"})
		return
	}

	servers := make([]serverInfo, 0, len(deployments.Items))
	for _, d := range deployments.Items {
		deploymentStatus := statusForDeployment(d)
		servers = append(servers, serverInfo{
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     deploymentStatus.Ready,
			Status:    deploymentStatus.Status,
			Labels:    d.Labels,
			Age:       d.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
			Prompts:   []mcpv1alpha1.InventoryItem{},
			Resources: []mcpv1alpha1.InventoryItem{},
			Tasks:     []mcpv1alpha1.InventoryItem{},
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"servers": servers})
}

type serverInfo struct {
	Name       string                      `json:"name"`
	Namespace  string                      `json:"namespace"`
	Ready      string                      `json:"ready"`
	Status     string                      `json:"status"`
	Labels     map[string]string           `json:"labels,omitempty"`
	Age        string                      `json:"age"`
	Endpoint   string                      `json:"endpoint,omitempty"`
	Tools      []mcpv1alpha1.ToolConfig    `json:"tools,omitempty"`
	Prompts    []mcpv1alpha1.InventoryItem `json:"prompts"`
	Resources  []mcpv1alpha1.InventoryItem `json:"resources"`
	Tasks      []mcpv1alpha1.InventoryItem `json:"tasks"`
	AccessJSON map[string]any              `json:"access_json,omitempty"`
}

type serverDeploymentStatus struct {
	Ready  string
	Status string
}

func statusForDeployment(d appsv1.Deployment) serverDeploymentStatus {
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
	return serverDeploymentStatus{Ready: ready, Status: status}
}

func serverInfoFromMCPServer(mcpServer mcpv1alpha1.MCPServer, deploymentStatus serverDeploymentStatus) serverInfo {
	if deploymentStatus.Ready == "" {
		deploymentStatus = serverDeploymentStatus{Ready: "0/0", Status: strings.TrimSpace(mcpServer.Status.Phase)}
		if deploymentStatus.Status == "" {
			deploymentStatus.Status = "Unknown"
		}
	}
	endpoint := publicMCPEndpoint(mcpServer)
	info := serverInfo{
		Name:      mcpServer.Name,
		Namespace: mcpServer.Namespace,
		Ready:     deploymentStatus.Ready,
		Status:    deploymentStatus.Status,
		Labels:    mcpServer.Labels,
		Age:       mcpServer.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
		Endpoint:  endpoint,
		Tools:     mcpServer.Spec.Tools,
		Prompts:   inventoryItemsOrEmpty(mcpServer.Spec.Prompts),
		Resources: inventoryItemsOrEmpty(mcpServer.Spec.MCPResources),
		Tasks:     inventoryItemsOrEmpty(mcpServer.Spec.Tasks),
	}
	if endpoint != "" {
		info.AccessJSON = map[string]any{
			"mcpServers": map[string]any{
				mcpServer.Name: map[string]any{
					"type": "http",
					"url":  endpoint,
				},
			},
		}
	}
	return info
}

func inventoryItemsOrEmpty(items []mcpv1alpha1.InventoryItem) []mcpv1alpha1.InventoryItem {
	if len(items) == 0 {
		return []mcpv1alpha1.InventoryItem{}
	}
	return items
}

func publicMCPEndpoint(mcpServer mcpv1alpha1.MCPServer) string {
	path := strings.TrimSpace(mcpServer.Spec.IngressPath)
	if path == "" {
		prefix := strings.Trim(strings.TrimSpace(mcpServer.Spec.PublicPathPrefix), "/")
		if prefix == "" {
			prefix = mcpServer.Name
		}
		path = "/" + prefix + "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	host := strings.TrimSpace(mcpServer.Spec.IngressHost)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("MCP_MCP_INGRESS_HOST"))
	}
	if host == "" {
		if domain := strings.TrimSpace(os.Getenv("MCP_PLATFORM_DOMAIN")); domain != "" {
			host = "mcp." + strings.Trim(strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://"), "/")
		}
	}
	if host == "" {
		return path
	}
	scheme := "https"
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + strings.TrimRight(host, "/") + path
}

// handleRuntimeGrants returns MCPAccessGrant resources.
func (s *RuntimeServer) handleRuntimeGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeGrantList(w, r)
	case http.MethodPost:
		s.handleRuntimeGrantApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleRuntimeGrantList(w http.ResponseWriter, r *http.Request) {
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

func (s *RuntimeServer) handleRuntimeGrantApply(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	var req accessGrantRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin && strings.TrimSpace(req.Namespace) == "" {
		req.Namespace = strings.TrimSpace(p.Namespace)
	}
	if err := validateGrantRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	scopedNamespace, err := s.scopedNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	req.Namespace = scopedNamespace

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// serverRef is checked with a live Get, not a transaction with ApplyGrant. Another actor
	// may delete the MCPServer after this call; the grant can still be written. Clients should retry on policy errors.
	if err := s.accessMgr.AssertMCPServerRef(ctx, req.ServerRef); err != nil {
		if sentinelaccess.IsMCPServerNotFoundForRef(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			log.Printf("runtime grant: assert MCPServer ref failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		}
		return
	}

	disabled, err := s.grantDisabledForApply(ctx, req)
	if err != nil {
		log.Printf("read grant state %s/%s failed: %v", req.Namespace, req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read grant state"})
		return
	}

	grant := &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: defaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef:     req.ServerRef,
			Subject:       req.Subject,
			MaxTrust:      req.MaxTrust,
			PolicyVersion: defaultPolicyVersion(req.PolicyVersion),
			Disabled:      disabled,
			ToolRules:     req.ToolRules,
		},
	}
	applied, err := s.accessMgr.ApplyGrant(ctx, grant)
	if err != nil {
		writeK8sApplyError(w, "grant", grant.Namespace, grant.Name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"grant": sentinelaccess.ToGrantSummary(*applied)})
}

// handleRuntimeSessions returns MCPAgentSession resources.
func (s *RuntimeServer) handleRuntimeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeSessionList(w, r)
	case http.MethodPost:
		s.handleRuntimeSessionApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleRuntimeSessionList(w http.ResponseWriter, r *http.Request) {
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

func (s *RuntimeServer) handleRuntimeSessionApply(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	var req accessSessionRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin && strings.TrimSpace(req.Namespace) == "" {
		req.Namespace = strings.TrimSpace(p.Namespace)
	}
	if err := validateSessionRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	scopedNamespace, err := s.scopedNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	req.Namespace = scopedNamespace

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// See handleRuntimeGrantApply: serverRef check is not transactional with the session write.
	if err := s.accessMgr.AssertMCPServerRef(ctx, req.ServerRef); err != nil {
		if sentinelaccess.IsMCPServerNotFoundForRef(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			log.Printf("runtime session: assert MCPServer ref failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		}
		return
	}

	revoked, err := s.sessionRevokedForApply(ctx, req)
	if err != nil {
		log.Printf("read session state %s/%s failed: %v", req.Namespace, req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read session state"})
		return
	}

	session := &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: defaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      req.ServerRef,
			Subject:        req.Subject,
			ConsentedTrust: req.ConsentedTrust,
			ExpiresAt:      req.ExpiresAt,
			Revoked:        revoked,
			PolicyVersion:  defaultPolicyVersion(req.PolicyVersion),
		},
	}
	applied, err := s.accessMgr.ApplySession(ctx, session)
	if err != nil {
		writeK8sApplyError(w, "session", session.Namespace, session.Name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"session": sentinelaccess.ToSessionSummary(*applied)})
}

func (s *RuntimeServer) grantDisabledForApply(ctx context.Context, req accessGrantRequest) (bool, error) {
	if req.Disabled != nil {
		return *req.Disabled, nil
	}
	existing, err := s.accessMgr.GetGrant(ctx, req.Name, defaultAccessNamespace(req.Namespace))
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing.Spec.Disabled, nil
}

func (s *RuntimeServer) sessionRevokedForApply(ctx context.Context, req accessSessionRequest) (bool, error) {
	if req.Revoked != nil {
		return *req.Revoked, nil
	}
	existing, err := s.accessMgr.GetSession(ctx, req.Name, defaultAccessNamespace(req.Namespace))
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing.Spec.Revoked, nil
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

// handleGrantItemPath handles POST /api/runtime/grants/{namespace}/{name}/disable|enable
// and DELETE /api/runtime/grants/{namespace}/{name}.
func (s *RuntimeServer) handleGrantItemPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ns, name, err := extractNamespacedPath(r.URL.Path, "/api/runtime/grants/", 2)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleGrantGet(w, r, ns, name)
		return
	case http.MethodDelete:
		ns, name, err := serviceutil.ExtractNamespacedResourceDelete(r, "/api/runtime/grants/")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleGrantDelete(w, r, ns, name)
		return
	case http.MethodPost:
		s.handleGrantPostTogglePath(w, r)
		return
	default:
		w.Header().Set("allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleGrantGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": sentinelaccess.ToGrantSummary(*grant)})
}

func (s *RuntimeServer) handleGrantDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.accessMgr.DeleteGrant(ctx, name, namespace); err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete grant %s/%s failed (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete grant: %s", msg)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
	})
}

// handleGrantPostTogglePath handles POST /api/runtime/grants/{namespace}/{name}/disable|enable
func (s *RuntimeServer) handleGrantPostTogglePath(w http.ResponseWriter, r *http.Request) {
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

	namespace, nsErr := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if nsErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": nsErr.Error()})
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

func validateGrantRequest(req *accessGrantRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultAccessNamespace(req.Namespace)
	req.ServerRef.Name = strings.TrimSpace(req.ServerRef.Name)
	req.ServerRef.Namespace = strings.TrimSpace(req.ServerRef.Namespace)
	req.Subject.HumanID = strings.TrimSpace(req.Subject.HumanID)
	req.Subject.AgentID = strings.TrimSpace(req.Subject.AgentID)
	req.PolicyVersion = defaultPolicyVersion(req.PolicyVersion)
	req.MaxTrust = normalizeTrust(req.MaxTrust)
	if err := sentinelaccess.ValidateResourceName("name", req.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("namespace", req.Namespace); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("serverRef.name", req.ServerRef.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateOptionalResourceName("serverRef.namespace", req.ServerRef.Namespace); err != nil {
		return err
	}
	if req.Subject.HumanID == "" && req.Subject.AgentID == "" {
		return errors.New("either subject.humanID or subject.agentID is required")
	}
	if req.MaxTrust != "" && !validTrust(req.MaxTrust) {
		return errors.New("maxTrust must be low, medium, or high")
	}
	for i := range req.ToolRules {
		req.ToolRules[i].Name = strings.TrimSpace(req.ToolRules[i].Name)
		req.ToolRules[i].Decision = sentinelaccess.PolicyDecision(strings.TrimSpace(string(req.ToolRules[i].Decision)))
		req.ToolRules[i].RequiredTrust = normalizeTrust(req.ToolRules[i].RequiredTrust)
		if req.ToolRules[i].Name == "" {
			return fmt.Errorf("toolRules[%d].name is required", i)
		}
		if !validDecision(req.ToolRules[i].Decision) {
			return fmt.Errorf("toolRules[%d].decision must be allow or deny", i)
		}
		if req.ToolRules[i].RequiredTrust != "" && !validTrust(req.ToolRules[i].RequiredTrust) {
			return fmt.Errorf("toolRules[%d].requiredTrust must be low, medium, or high", i)
		}
	}
	return nil
}

func validateSessionRequest(req *accessSessionRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultAccessNamespace(req.Namespace)
	req.ServerRef.Name = strings.TrimSpace(req.ServerRef.Name)
	req.ServerRef.Namespace = strings.TrimSpace(req.ServerRef.Namespace)
	req.Subject.HumanID = strings.TrimSpace(req.Subject.HumanID)
	req.Subject.AgentID = strings.TrimSpace(req.Subject.AgentID)
	req.PolicyVersion = defaultPolicyVersion(req.PolicyVersion)
	req.ConsentedTrust = normalizeTrust(req.ConsentedTrust)
	if err := sentinelaccess.ValidateResourceName("name", req.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("namespace", req.Namespace); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("serverRef.name", req.ServerRef.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateOptionalResourceName("serverRef.namespace", req.ServerRef.Namespace); err != nil {
		return err
	}
	if req.Subject.HumanID == "" && req.Subject.AgentID == "" {
		return errors.New("either subject.humanID or subject.agentID is required")
	}
	if req.ConsentedTrust != "" && !validTrust(req.ConsentedTrust) {
		return errors.New("consentedTrust must be low, medium, or high")
	}
	return nil
}

func defaultAccessNamespace(namespace string) string {
	if namespace = strings.TrimSpace(namespace); namespace != "" {
		return namespace
	}
	return sentinelaccess.DefaultMCPResourceNamespace
}

func defaultPolicyVersion(policyVersion string) string {
	if policyVersion = strings.TrimSpace(policyVersion); policyVersion != "" {
		return policyVersion
	}
	return "v1"
}

func writeK8sApplyError(w http.ResponseWriter, kind, namespace, name string, err error) {
	code, msg := k8sclient.HTTPStatusFromK8sError(err)
	log.Printf("apply %s %s/%s failed (status=%d): %v", kind, namespace, name, code, err)
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to apply %s: %s", kind, msg)})
}

const accessApplyMaxBytes = 64 * 1024

// writeBodyDecodeError distinguishes a body-size cap from a generic JSON decode
// failure so clients see a helpful 413 + size hint instead of a vague 400.
func writeBodyDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("request body exceeds %d bytes", accessApplyMaxBytes),
		})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
}

func normalizeTrust(trust sentinelaccess.TrustLevel) sentinelaccess.TrustLevel {
	return sentinelaccess.TrustLevel(strings.TrimSpace(string(trust)))
}

func validTrust(trust sentinelaccess.TrustLevel) bool {
	switch trust {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func validDecision(decision sentinelaccess.PolicyDecision) bool {
	switch decision {
	case "allow", "deny":
		return true
	default:
		return false
	}
}

// handleSessionItemPath handles POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke
// and DELETE /api/runtime/sessions/{namespace}/{name}.
func (s *RuntimeServer) handleSessionItemPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ns, name, err := extractNamespacedPath(r.URL.Path, "/api/runtime/sessions/", 2)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleSessionGet(w, r, ns, name)
		return
	case http.MethodDelete:
		ns, name, err := serviceutil.ExtractNamespacedResourceDelete(r, "/api/runtime/sessions/")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleSessionDelete(w, r, ns, name)
		return
	case http.MethodPost:
		s.handleSessionPostTogglePath(w, r)
		return
	default:
		w.Header().Set("allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleSessionGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	session, err := s.accessMgr.GetSession(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sentinelaccess.ToSessionSummary(*session)})
}

func extractNamespacedPath(path, prefix string, expectedParts int) (string, string, error) {
	path = strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != expectedParts {
		return "", "", fmt.Errorf("invalid path")
	}
	namespace := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if namespace == "" || name == "" {
		return "", "", fmt.Errorf("invalid path")
	}
	if err := sentinelaccess.ValidateResourceName("namespace", namespace); err != nil {
		return "", "", err
	}
	if err := sentinelaccess.ValidateResourceName("name", name); err != nil {
		return "", "", err
	}
	return namespace, name, nil
}

func (s *RuntimeServer) handleSessionDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.accessMgr.DeleteSession(ctx, name, namespace); err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete session %s/%s failed (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete session: %s", msg)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
	})
}

// handleSessionPostTogglePath handles POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke
func (s *RuntimeServer) handleSessionPostTogglePath(w http.ResponseWriter, r *http.Request) {
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

	namespace, nsErr := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if nsErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": nsErr.Error()})
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

func (s *RuntimeServer) scopedNamespaceForPrincipal(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	p, ok := principalFromContext(ctx)
	if !ok || p.Role == roleAdmin {
		return requested, nil
	}
	subjectNamespace := strings.TrimSpace(p.Namespace)
	if subjectNamespace == "" {
		return "", errPrincipalIdentityRequired
	}
	if requested == "" {
		return subjectNamespace, nil
	}
	if requested != subjectNamespace {
		return "", errors.New("forbidden namespace")
	}
	return requested, nil
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
