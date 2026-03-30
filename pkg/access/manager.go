package access

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	APIGroup              = "mcpruntime.org"
	APIVersion            = "v1alpha1"
	AccessGrantResource   = "mcpaccessgrants"
	AccessSessionResource = "mcpagentsessions"
)

var (
	grantGVR   = schema.GroupVersionResource{Group: APIGroup, Version: APIVersion, Resource: AccessGrantResource}
	sessionGVR = schema.GroupVersionResource{Group: APIGroup, Version: APIVersion, Resource: AccessSessionResource}
)

// Manager provides operations for MCPAccessGrant and MCPAgentSession resources.
type Manager struct {
	dynamic   dynamic.Interface
	clientset kubernetes.Interface
}

// NewManager creates a new access resource manager.
func NewManager(dynamic dynamic.Interface, clientset kubernetes.Interface) *Manager {
	return &Manager{
		dynamic:   dynamic,
		clientset: clientset,
	}
}

// ListGrants returns all MCPAccessGrant resources, optionally filtered by namespace.
func (m *Manager) ListGrants(ctx context.Context, namespace string) (*MCPAccessGrantList, error) {
	var result *MCPAccessGrantList
	var err error

	if namespace != "" {
		var obj interface{}
		obj, err = m.dynamic.Resource(grantGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list grants in namespace %s: %w", namespace, err)
		}
		result, err = convertToGrantList(obj)
	} else {
		var obj interface{}
		obj, err = m.dynamic.Resource(grantGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list grants: %w", err)
		}
		result, err = convertToGrantList(obj)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetGrant returns a specific MCPAccessGrant resource.
func (m *Manager) GetGrant(ctx context.Context, name, namespace string) (*MCPAccessGrant, error) {
	obj, err := m.dynamic.Resource(grantGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get grant %s/%s: %w", namespace, name, err)
	}
	return convertToGrant(obj)
}

// DisableGrant disables an MCPAccessGrant by setting spec.disabled to true.
func (m *Manager) DisableGrant(ctx context.Context, name, namespace string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"disabled": true,
		},
	}
	return m.patchGrant(ctx, name, namespace, patch)
}

// EnableGrant enables an MCPAccessGrant by setting spec.disabled to false.
func (m *Manager) EnableGrant(ctx context.Context, name, namespace string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"disabled": false,
		},
	}
	return m.patchGrant(ctx, name, namespace, patch)
}

func (m *Manager) patchGrant(ctx context.Context, name, namespace string, patch map[string]interface{}) error {
	patchData, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = m.dynamic.Resource(grantGVR).Namespace(namespace).Patch(ctx, name, types.MergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch grant %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ListSessions returns all MCPAgentSession resources, optionally filtered by namespace.
func (m *Manager) ListSessions(ctx context.Context, namespace string) (*MCPAgentSessionList, error) {
	var result *MCPAgentSessionList
	var err error

	if namespace != "" {
		var obj interface{}
		obj, err = m.dynamic.Resource(sessionGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list sessions in namespace %s: %w", namespace, err)
		}
		result, err = convertToSessionList(obj)
	} else {
		var obj interface{}
		obj, err = m.dynamic.Resource(sessionGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list sessions: %w", err)
		}
		result, err = convertToSessionList(obj)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetSession returns a specific MCPAgentSession resource.
func (m *Manager) GetSession(ctx context.Context, name, namespace string) (*MCPAgentSession, error) {
	obj, err := m.dynamic.Resource(sessionGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get session %s/%s: %w", namespace, name, err)
	}
	return convertToSession(obj)
}

// RevokeSession revokes an MCPAgentSession by setting spec.revoked to true.
func (m *Manager) RevokeSession(ctx context.Context, name, namespace string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"revoked": true,
		},
	}
	return m.patchSession(ctx, name, namespace, patch)
}

// UnrevokeSession clears the revoked flag on an MCPAgentSession.
func (m *Manager) UnrevokeSession(ctx context.Context, name, namespace string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"revoked": false,
		},
	}
	return m.patchSession(ctx, name, namespace, patch)
}

func (m *Manager) patchSession(ctx context.Context, name, namespace string, patch map[string]interface{}) error {
	patchData, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = m.dynamic.Resource(sessionGVR).Namespace(namespace).Patch(ctx, name, types.MergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch session %s/%s: %w", namespace, name, err)
	}
	return nil
}

// GetServerPolicy returns the rendered policy for a specific server if available.
func (m *Manager) GetServerPolicy(ctx context.Context, namespace, serverName string) (map[string]interface{}, error) {
	// First, try to find a ConfigMap with the rendered policy
	configMapName := fmt.Sprintf("%s-gateway-policy", serverName)
	configMap, err := m.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("policy not found for server %s/%s: %w", namespace, serverName, err)
	}

	policy := make(map[string]interface{})
	hasPolicyDocument := false
	if policyYAML, ok := configMap.Data["policy.yaml"]; ok {
		policy["yaml"] = policyYAML
		hasPolicyDocument = true
	}
	if policyJSON, ok := configMap.Data["policy.json"]; ok {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(policyJSON), &decoded); err != nil {
			return nil, fmt.Errorf("failed to parse policy configmap %s/%s policy.json: %w", namespace, configMapName, err)
		}
		for key, value := range decoded {
			policy[key] = value
		}
		policy["json"] = policyJSON
		hasPolicyDocument = true
	}
	if !hasPolicyDocument {
		return nil, fmt.Errorf("policy configmap %s/%s does not contain policy.yaml or policy.json", namespace, configMapName)
	}
	policy["source"] = fmt.Sprintf("configmap/%s/%s", namespace, configMapName)

	return policy, nil
}
