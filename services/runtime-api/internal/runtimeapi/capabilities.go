package runtimeapi

import (
	"context"

	"mcp-runtime/pkg/controlplane"
)

func (s *DeploymentService) controlPlane() *controlplane.Manager {
	if s == nil || s.k8sClients == nil {
		return nil
	}
	return controlplane.New(s.k8sClients)
}

func (s *DeploymentService) writeAudit(ctx context.Context, ev auditEvent) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.WriteAudit(ctx, ev)
}

func (s *DeploymentService) purgeExpiredRegistryPushTransfers(ctx context.Context) {
	if s == nil {
		return
	}
	(&RegistryPushService{k8sClients: s.k8sClients}).purgeExpiredRegistryPushTransfers(ctx)
}

func (s *AccessService) controlPlane() *controlplane.Manager {
	if s == nil || s.k8sClients == nil {
		return nil
	}
	return controlplane.New(s.k8sClients)
}

func (s *AccessService) identityConfigured() bool {
	return s != nil && s.identity != nil && s.identity.Configured()
}

func (s *InventoryService) controlPlane() *controlplane.Manager {
	if s == nil {
		return nil
	}
	if s.control != nil {
		return s.control
	}
	if s.k8sClients == nil {
		return nil
	}
	return controlplane.New(s.k8sClients)
}
