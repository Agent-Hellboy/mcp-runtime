package controlplane

import (
	"errors"

	"mcp-runtime/pkg/k8sclient"
)

// Manager reads and mutates MCP Runtime control-plane resources in Kubernetes.
type Manager struct {
	clients *k8sclient.Clients
}

// New creates a control-plane manager backed by Kubernetes clients.
func New(clients *k8sclient.Clients) *Manager {
	return &Manager{clients: clients}
}

func (m *Manager) requireClients() (*k8sclient.Clients, error) {
	if m == nil || m.clients == nil || m.clients.Dynamic == nil || m.clients.Clientset == nil {
		return nil, errors.New("kubernetes not available")
	}
	return m.clients, nil
}
