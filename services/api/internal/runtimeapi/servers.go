package runtimeapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin {
		if namespace == "" {
			namespace = strings.TrimSpace(p.Namespace)
			if namespace == "" {
				namespace = sharedCatalogNamespace
			}
		}
		if !p.HasNamespace(namespace) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
			return
		}
	}
	if namespace == "" {
		namespace = sharedCatalogNamespace
	}

	result, err := control.ListServers(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list servers"})
		return
	}
	if result.CRDError != nil && !apierrors.IsNotFound(result.CRDError) {
		log.Printf("runtime servers: list MCPServers failed: %v", result.CRDError)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"servers": serverInfosWithAccessJSON(result.Servers, r)})
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
	if req.Namespace == "" {
		req.Namespace = strings.TrimSpace(p.Namespace)
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if p.Role != roleAdmin && namespace == sharedCatalogNamespace {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "shared catalog namespace is read-only for team users"})
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

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "mcp-runtime",
	}
	for key, value := range req.Labels {
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	server := &mcpv1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: mcpv1alpha1.GroupVersion.String(),
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: req.Spec,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	applied, err := control.ApplyServer(ctx, server)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": serverInfoFromMCPServer(*applied, serverDeploymentStatus{}, r)})
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
