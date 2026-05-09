package controlplane

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

const stableDeploymentSelector = "app.kubernetes.io/managed-by=mcp-runtime,mcpruntime.org/rollout-track=stable"

// MCPServerGVR is the dynamic-client resource identity for MCPServer objects.
var MCPServerGVR = mcpv1alpha1.GroupVersion.WithResource(mcpv1alpha1.MCPServerResource)

// ListServers lists MCPServer resources in namespace and joins them with the
// readiness of their stable backing Deployments. If the MCPServer resource is
// unavailable, it falls back to legacy managed Deployments.
func (m *Manager) ListServers(ctx context.Context, namespace string) (ListServersResult, error) {
	clients, err := m.requireClients()
	if err != nil {
		return ListServersResult{}, err
	}
	namespace = strings.TrimSpace(namespace)

	serverObjects, crdErr := clients.Dynamic.Resource(MCPServerGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if crdErr == nil {
		deploymentStatus := map[string]ServerDeploymentStatus{}
		deployments, deployErr := clients.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: stableDeploymentSelector,
		})
		if deployErr == nil {
			for _, d := range deployments.Items {
				deploymentStatus[d.Name] = StatusForDeployment(d)
			}
		}

		servers := make([]ServerInfo, 0, len(serverObjects.Items))
		for _, obj := range serverObjects.Items {
			var mcpServer mcpv1alpha1.MCPServer
			if convertErr := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &mcpServer); convertErr != nil {
				continue
			}
			servers = append(servers, ServerInfoFromMCPServer(mcpServer, deploymentStatus[mcpServer.Name]))
		}
		return ListServersResult{Servers: servers}, nil
	}

	deployments, err := clients.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: stableDeploymentSelector,
	})
	if err != nil {
		return ListServersResult{CRDError: crdErr, UsedDeploymentFallback: true}, err
	}

	servers := make([]ServerInfo, 0, len(deployments.Items))
	for _, d := range deployments.Items {
		deploymentStatus := StatusForDeployment(d)
		servers = append(servers, ServerInfo{
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

	return ListServersResult{
		Servers:                servers,
		CRDError:               crdErr,
		UsedDeploymentFallback: true,
	}, nil
}

// ApplyServer creates or updates an MCPServer resource.
func (m *Manager) ApplyServer(ctx context.Context, server *mcpv1alpha1.MCPServer) (*mcpv1alpha1.MCPServer, error) {
	clients, err := m.requireClients()
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, errors.New("server cannot be nil")
	}
	if strings.TrimSpace(server.Name) == "" {
		return nil, errors.New("server name is required")
	}
	if strings.TrimSpace(server.Namespace) == "" {
		return nil, errors.New("server namespace is required")
	}

	payload, err := apiruntime.DefaultUnstructuredConverter.ToUnstructured(server)
	if err != nil {
		return nil, fmt.Errorf("encode MCPServer: %w", err)
	}
	resource := &unstructured.Unstructured{Object: payload}

	current, err := clients.Dynamic.Resource(MCPServerGVR).Namespace(server.Namespace).Get(ctx, server.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		applied, createErr := clients.Dynamic.Resource(MCPServerGVR).Namespace(server.Namespace).Create(ctx, resource, metav1.CreateOptions{})
		if createErr != nil {
			return nil, createErr
		}
		return decodeMCPServer(applied, "created")
	}
	if err != nil {
		return nil, err
	}

	resource.SetResourceVersion(current.GetResourceVersion())
	applied, err := clients.Dynamic.Resource(MCPServerGVR).Namespace(server.Namespace).Update(ctx, resource, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return decodeMCPServer(applied, "updated")
}

func decodeMCPServer(obj *unstructured.Unstructured, action string) (*mcpv1alpha1.MCPServer, error) {
	var out mcpv1alpha1.MCPServer
	if err := apiruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &out); err != nil {
		return nil, fmt.Errorf("decode %s MCPServer: %w", action, err)
	}
	return &out, nil
}

// StatusForDeployment summarizes Deployment readiness in the format used by
// runtime server listings.
func StatusForDeployment(d appsv1.Deployment) ServerDeploymentStatus {
	status := "NotReady"
	desiredReplicas := DeploymentDesiredReplicas(d, 0)
	ready := fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desiredReplicas)
	if d.Status.ReadyReplicas == desiredReplicas && desiredReplicas > 0 {
		status = "Ready"
	} else if d.Status.ReadyReplicas > 0 {
		status = "Degraded"
	}
	return ServerDeploymentStatus{Ready: ready, Status: status}
}

// DeploymentDesiredReplicas returns the desired replica count, using
// defaultReplicas when the Deployment does not set spec.replicas.
func DeploymentDesiredReplicas(d appsv1.Deployment, defaultReplicas int32) int32 {
	if d.Spec.Replicas != nil {
		return *d.Spec.Replicas
	}
	return defaultReplicas
}

// DeploymentReady reports whether the Deployment has the desired number of
// ready replicas.
func DeploymentReady(d appsv1.Deployment, defaultReplicas int32) bool {
	return d.Status.ReadyReplicas == DeploymentDesiredReplicas(d, defaultReplicas)
}

// ServerInfoFromMCPServer projects an MCPServer plus optional Deployment status
// into the control-plane server summary shape.
func ServerInfoFromMCPServer(mcpServer mcpv1alpha1.MCPServer, deploymentStatus ServerDeploymentStatus) ServerInfo {
	if deploymentStatus.Ready == "" {
		deploymentStatus = ServerDeploymentStatus{Ready: "0/0", Status: strings.TrimSpace(mcpServer.Status.Phase)}
		if deploymentStatus.Status == "" {
			deploymentStatus.Status = "Unknown"
		}
	}
	return ServerInfo{
		Name:        mcpServer.Name,
		Namespace:   mcpServer.Namespace,
		Description: mcpServer.Spec.Description,
		Ready:       deploymentStatus.Ready,
		Status:      deploymentStatus.Status,
		Labels:      mcpServer.Labels,
		Age:         mcpServer.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
		Endpoint:    PublicMCPEndpoint(mcpServer),
		Tools:       mcpServer.Spec.Tools,
		Prompts:     inventoryItemsOrEmpty(mcpServer.Spec.Prompts),
		Resources:   inventoryItemsOrEmpty(mcpServer.Spec.MCPResources),
		Tasks:       inventoryItemsOrEmpty(mcpServer.Spec.Tasks),
	}
}

func inventoryItemsOrEmpty(items []mcpv1alpha1.InventoryItem) []mcpv1alpha1.InventoryItem {
	if len(items) == 0 {
		return []mcpv1alpha1.InventoryItem{}
	}
	return items
}

// PublicMCPEndpoint returns the public MCP endpoint path or URL for a server.
func PublicMCPEndpoint(mcpServer mcpv1alpha1.MCPServer) string {
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
