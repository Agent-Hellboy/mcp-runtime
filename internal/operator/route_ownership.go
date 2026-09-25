package operator

import (
	"context"
	"fmt"
	"path"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

// publicRoute is the host and path an MCPServer is served on. An empty host
// is a host-less (path-based) route that matches every host.
type publicRoute struct {
	host string
	path string
}

func publicRouteOf(server *mcpv1alpha1.MCPServer) publicRoute {
	routePath := strings.TrimSpace(server.EffectivePublicPath())
	if routePath != "" {
		routePath = path.Clean("/" + strings.TrimLeft(routePath, "/"))
	}
	return publicRoute{host: strings.ToLower(strings.TrimSpace(server.Spec.IngressHost)), path: routePath}
}

func (a publicRoute) overlaps(b publicRoute) bool {
	if a.path == "" || a.path != b.path {
		return false
	}
	return a.host == "" || b.host == "" || a.host == b.host
}

// olderServer orders servers by creation time, then namespace/name, so the
// same server wins a route conflict on every reconcile.
func olderServer(a, b *mcpv1alpha1.MCPServer) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	return a.Name < b.Name
}

// publicRouteOwner returns the server that owns server's public route among
// all, or nil when server owns it. Public routes, and therefore derived OAuth
// audiences, are shared across namespaces on one ingress host, so two servers
// on the same route would accept each other's tokens. The oldest claimant
// keeps the route; later ones are refused until it is freed.
func publicRouteOwner(server *mcpv1alpha1.MCPServer, all []mcpv1alpha1.MCPServer) *mcpv1alpha1.MCPServer {
	route := publicRouteOf(server)
	var owner *mcpv1alpha1.MCPServer
	for i := range all {
		other := &all[i]
		if other.Namespace == server.Namespace && other.Name == server.Name {
			continue
		}
		if !other.DeletionTimestamp.IsZero() || !publicRouteOf(other).overlaps(route) {
			continue
		}
		if olderServer(other, server) && (owner == nil || olderServer(other, owner)) {
			owner = other
		}
	}
	return owner
}

// checkPublicRouteOwnership refuses a server whose public route another,
// older MCPServer already serves, and removes any Ingress it created before
// the conflict existed so the route has a single backend.
func (r *MCPServerReconciler) checkPublicRouteOwnership(ctx context.Context, server *mcpv1alpha1.MCPServer) error {
	var servers mcpv1alpha1.MCPServerList
	if err := r.List(ctx, &servers); err != nil {
		return err
	}
	defaulted := make([]mcpv1alpha1.MCPServer, 0, len(servers.Items))
	for i := range servers.Items {
		defaulted = append(defaulted, *r.defaultedMCPServerForReconcile(&servers.Items[i]))
	}
	owner := publicRouteOwner(server, defaulted)
	if owner == nil {
		return nil
	}
	ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace}}
	if err := r.Delete(ctx, ingress); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return fmt.Errorf("public route %s is already served by MCPServer %s/%s; choose a different publicPathPrefix or ingressHost", publicRouteOf(server).path, owner.Namespace, owner.Name)
}
