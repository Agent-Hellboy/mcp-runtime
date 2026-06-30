package runtimeapi

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	runtimeaccess "mcp-runtime-api/internal/runtimeapi/access"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
)

type accessServerCache map[string]mcpv1alpha1.MCPServer

func (s *AccessService) grantVisibleToPrincipal(ctx context.Context, grant sentinelaccess.MCPAccessGrant) bool {
	if p, ok := principalFromContext(ctx); !ok || p.Role == roleAdmin {
		return true
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, grant.Namespace, grant.Spec.ServerRef)
	return err == nil && allowed
}

func (s *AccessService) sessionVisibleToPrincipal(ctx context.Context, session sentinelaccess.MCPAgentSession) bool {
	if p, ok := principalFromContext(ctx); !ok || p.Role == roleAdmin {
		return true
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, session.Namespace, session.Spec.ServerRef)
	return err == nil && allowed
}

func (s *AccessService) accessServerCacheForGrantRefs(ctx context.Context, namespace string, grants []sentinelaccess.MCPAccessGrant) (accessServerCache, error) {
	refs := make([]sentinelaccess.ServerReference, 0, len(grants))
	for _, grant := range grants {
		refs = append(refs, grant.Spec.ServerRef)
	}
	return s.accessServerCacheForRefs(ctx, namespace, refs)
}

func (s *AccessService) accessServerCacheForSessionRefs(ctx context.Context, namespace string, sessions []sentinelaccess.MCPAgentSession) (accessServerCache, error) {
	refs := make([]sentinelaccess.ServerReference, 0, len(sessions))
	for _, session := range sessions {
		refs = append(refs, session.Spec.ServerRef)
	}
	return s.accessServerCacheForRefs(ctx, namespace, refs)
}

func (s *AccessService) accessServerCacheForRefs(ctx context.Context, namespace string, refs []sentinelaccess.ServerReference) (accessServerCache, error) {
	namespaces := map[string]struct{}{}
	for _, ref := range refs {
		if strings.TrimSpace(string(ref.Name)) == "" {
			continue
		}
		namespaces[runtimeaccess.AccessServerRefNamespace(namespace, ref)] = struct{}{}
	}

	cache := accessServerCache{}
	for ns := range namespaces {
		servers, err := s.accessMgr.ListMCPServers(ctx, ns)
		if err != nil {
			return nil, err
		}
		for _, server := range servers.Items {
			cache[runtimeaccess.AccessServerCacheKey(server.Namespace, server.Name)] = server
		}
	}
	return cache, nil
}

func (s *AccessService) grantDisabledForApply(ctx context.Context, req accessGrantRequest) (bool, error) {
	if req.Disabled != nil {
		return *req.Disabled, nil
	}
	existing, err := s.accessMgr.GetGrant(ctx, req.Name, runtimeaccess.DefaultAccessNamespace(req.Namespace))
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing.Spec.Disabled, nil
}
