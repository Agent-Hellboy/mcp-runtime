package runtimeapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/controlplane"
	"mcp-runtime/pkg/k8sclient"
)

type runtimeServerApplyRequest struct {
	Name      string                    `json:"name"`
	Namespace string                    `json:"namespace,omitempty"`
	Labels    map[string]string         `json:"labels,omitempty"`
	Spec      mcpv1alpha1.MCPServerSpec `json:"spec"`
}

func (s *RuntimeServer) HandleRuntimeServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeServerList(w, r)
	case http.MethodPost:
		s.handleRuntimeServerApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleRuntimeServerList(w http.ResponseWriter, r *http.Request) {
	control := s.controlPlane()
	if control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	namespaces := []string{namespace}
	if p.Role != roleAdmin {
		if namespace == "" {
			namespaces = catalogNamespacesForPrincipal(p)
		} else if !principalCanReadNamespace(p, namespace) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
			return
		}
	} else if namespace == "" {
		if PlatformMode() == platformModeTenant {
			namespaces = []string{sharedCatalogNamespace}
		} else {
			namespaces = []string{defaultCatalogNamespaceForMode()}
		}
	}
	namespaces = dedupeNonEmptyStrings(namespaces)
	if len(namespaces) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"servers":        []serverInfo{},
			"publish_policy": s.publishPolicyStatusForPrincipal(ctx, p),
		})
		return
	}

	servers := make([]controlplane.ServerInfo, 0)
	for _, namespace := range namespaces {
		if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
			return
		}

		result, err := control.ListServers(ctx, namespace)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list servers"})
			return
		}
		if result.CRDError != nil && !apierrors.IsNotFound(result.CRDError) {
			log.Printf("runtime servers: list MCPServers failed in namespace %q: %v", namespace, result.CRDError)
		}
		servers = append(servers, result.Servers...)
	}
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].Namespace != servers[j].Namespace {
			return servers[i].Namespace < servers[j].Namespace
		}
		return servers[i].Name < servers[j].Name
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"servers":        serverInfosWithAccessJSON(servers, r),
		"publish_policy": s.publishPolicyStatusForPrincipal(ctx, p),
	})
}

func catalogNamespacesForPrincipal(p principal) []string {
	return publishNamespacesForPrincipal(p)
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *RuntimeServer) handleRuntimeServerApply(w http.ResponseWriter, r *http.Request) {
	control := s.controlPlane()
	if control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req runtimeServerApplyRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Spec.Image = strings.TrimSpace(req.Spec.Image)
	if req.Name == "" || req.Spec.Image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and spec.image are required"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if p.Role != roleAdmin && namespace == sharedCatalogNamespace && !sharedCatalogWritableForUsers() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "shared catalog namespace is read-only for team users"})
		return
	}
	if p.Role != roleAdmin && !principalCanPublishNamespace(p, namespace) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
		return
	}
	namespaceTeamID := strings.TrimSpace(s.teamIDForPrincipalNamespace(r.Context(), namespace))
	req.Spec.TeamID = strings.TrimSpace(req.Spec.TeamID)
	if req.Spec.TeamID == "" {
		req.Spec.TeamID = namespaceTeamID
	}
	if err := validateTeamIDValue("spec.teamID", req.Spec.TeamID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if namespaceTeamID != "" && req.Spec.TeamID != namespaceTeamID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "spec.teamID must match namespace team"})
		return
	}
	if p.Role != roleAdmin && namespaceTeamID == "" && req.Spec.TeamID != "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "spec.teamID is only allowed in a team namespace"})
		return
	}
	team, isTeamNamespace := p.TeamForNamespace(namespace)
	teamSlug := ""
	if isTeamNamespace {
		teamSlug = strings.TrimSpace(team.Slug)
	}
	if err := ValidateDeployImage(req.Spec.Image, namespace, teamSlug, p.Role); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	req.Namespace = namespace
	if req.Spec.PublicPathPrefix == "" {
		req.Spec.PublicPathPrefix = req.Name
	}
	if req.Spec.IngressPath == "" {
		req.Spec.IngressPath = "/" + req.Spec.PublicPathPrefix + "/mcp"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	current, err := control.GetServer(ctx, namespace, req.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Printf("runtime servers: fetch MCPServer %s/%s before apply failed: %v", namespace, req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to inspect existing server"})
		return
	}
	if p.Role != roleAdmin && current != nil && !serverWritableByPrincipal(*current, p) {
		msg := "server already exists and is not owned by this user"
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_publish", "denied", req.Name, namespace, req.Spec.Image, msg))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": msg})
		return
	}
	// This is an API-layer guard. Strict global quota enforcement under highly
	// concurrent publishes would need a shared reservation/locking mechanism.
	rejection, err := s.evaluateServerPublishPolicy(ctx, p, namespace, req.Name, current, time.Now().UTC())
	if err != nil {
		log.Printf("runtime servers: evaluate publish policy for %s/%s failed: %v", namespace, req.Name, err)
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_publish", "error", req.Name, namespace, req.Spec.Image, err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to evaluate publish policy"})
		return
	}
	if rejection != nil {
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_publish", "denied", req.Name, namespace, req.Spec.Image, rejection.message))
		if retryAfter := rejection.retryAfterHeader(); retryAfter != "" {
			w.Header().Set("retry-after", retryAfter)
		}
		writeJSON(w, rejection.status, rejection.payload())
		return
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "mcp-runtime",
	}
	for key, value := range req.Labels {
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if current != nil && p.Role == roleAdmin {
		preserveCurrentLabel(labels, current.Labels, platformUserIDLabel)
		preserveCurrentLabel(labels, current.Labels, createdByLabel)
	} else if userID := p.UserID(); userID != "" {
		labels[platformUserIDLabel] = userID
		labels[createdByLabel] = userID
	}
	if namespaceTeamID != "" {
		labels[platformTeamIDLabel] = namespaceTeamID
	}
	annotations := map[string]string{
		platformLastPushAtAnnotation: time.Now().UTC().Format(time.RFC3339),
	}
	if userID := p.UserID(); userID != "" {
		annotations[platformLastPushByAnnotation] = userID
	}

	server := &mcpv1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: mcpv1alpha1.GroupVersion.String(),
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        req.Name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: req.Spec,
	}

	if p.Role != roleAdmin && sharedCatalogWritableForUsers() && isModeCatalogNamespace(namespace) {
		if err := s.EnsureCatalogNamespace(ctx, namespace); err != nil {
			log.Printf("runtime servers: ensure catalog namespace %q failed: %v", namespace, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to ensure catalog namespace"})
			return
		}
	}
	applied, err := control.ApplyServer(ctx, server)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_publish", "error", req.Name, namespace, req.Spec.Image, msg))
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_publish", "success", req.Name, namespace, req.Spec.Image, ""))
	writeJSON(w, http.StatusOK, map[string]any{"server": serverInfoFromMCPServer(*applied, serverDeploymentStatus{}, r)})
}

func preserveCurrentLabel(labels, current map[string]string, key string) {
	if value := strings.TrimSpace(current[key]); value != "" {
		labels[key] = value
	}
}

func (s *RuntimeServer) HandleRuntimeServerItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("allow", "DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	control := s.controlPlane()
	if control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	namespace, name, err := extractNamespaceName(r.URL.Path, "/api/runtime/servers/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if p.Role != roleAdmin && !principalCanPublishNamespace(p, namespace) {
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_retire", "denied", name, namespace, "", "forbidden namespace"))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	current, err := control.GetServer(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "namespace": namespace, "name": name})
		return
	}
	if err != nil {
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_retire", "error", name, namespace, "", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to inspect server"})
		return
	}
	if p.Role != roleAdmin && !serverWritableByPrincipal(*current, p) {
		msg := "server is not owned by this user"
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_retire", "denied", name, namespace, "", msg))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": msg})
		return
	}
	if err := control.DeleteServer(ctx, namespace, name); err != nil {
		s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_retire", "error", name, namespace, current.Spec.Image, err.Error()))
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	s.writeAudit(r.Context(), serverPublishAuditEvent(r, p, "server_retire", "success", name, namespace, current.Spec.Image, ""))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "namespace": namespace, "name": name})
}

type serverInfo struct {
	controlplane.ServerInfo
	AccessJSON map[string]any `json:"access_json,omitempty"`
}

type serverDeploymentStatus = controlplane.ServerDeploymentStatus

func serverInfoFromMCPServer(mcpServer mcpv1alpha1.MCPServer, deploymentStatus serverDeploymentStatus, r *http.Request) serverInfo {
	return serverInfoWithAccessJSON(controlplane.ServerInfoFromMCPServer(mcpServer, deploymentStatus), r)
}

func serverInfosWithAccessJSON(items []controlplane.ServerInfo, r *http.Request) []serverInfo {
	out := make([]serverInfo, 0, len(items))
	for _, item := range items {
		out = append(out, serverInfoWithAccessJSON(item, r))
	}
	return out
}

func serverInfoWithAccessJSON(info controlplane.ServerInfo, r *http.Request) serverInfo {
	out := serverInfo{ServerInfo: info}
	connectEndpoint := publicMCPConnectEndpoint(info.Endpoint, r)
	if connectEndpoint != "" {
		out.AccessJSON = map[string]any{
			"mcpServers": map[string]any{
				info.Name: map[string]any{
					"type": "http",
					"url":  connectEndpoint,
				},
			},
		}
	}
	return out
}

func publicMCPEndpoint(mcpServer mcpv1alpha1.MCPServer) string {
	return controlplane.PublicMCPEndpoint(mcpServer)
}

func publicMCPConnectEndpoint(endpoint string, r *http.Request) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	path := endpoint
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	host := forwardedHost(r)
	if host == "" {
		return path
	}
	if strings.HasPrefix(strings.ToLower(host), "platform.") {
		host = "mcp." + host[len("platform."):]
	}
	return forwardedScheme(r) + "://" + strings.TrimRight(host, "/") + path
}

func forwardedHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	return firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
}

func forwardedScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	proto := strings.ToLower(firstForwardedValue(r.Header.Get("X-Forwarded-Proto")))
	if proto == "https" {
		return "https"
	}
	return "http"
}

func firstForwardedValue(value string) string {
	if idx := strings.IndexByte(value, ','); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}
