package operator

import (
	"context"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/oauthresource"
)

func (r *MCPServerReconciler) reconcileIngress(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}
	if r.usesAdapterCertificates(mcpServer) {
		// Create the IngressRoute first so the server is never left without
		// a route while switching from the plain Ingress.
		if err := r.reconcileMTLSIngress(ctx, mcpServer); err != nil {
			return err
		}
		if err := r.Delete(ctx, ingress); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}
	if err := r.deleteMTLSIngress(ctx, mcpServer); err != nil {
		return err
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		pathType := networkingv1.PathTypePrefix
		ingressClassName := mcpServer.Spec.IngressClass
		if ingressClassName == "" {
			ingressClassName = DefaultIngressClass
		}

		ingress.Spec = networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: effectiveIngressHost(mcpServer),
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: ingressPathsForServer(mcpServer, pathType),
						},
					},
				},
			},
		}

		// Build annotations based on ingress class
		annotations := r.buildIngressAnnotations(mcpServer)
		ingress.Annotations = annotations

		if err := ctrl.SetControllerReference(mcpServer, ingress, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		logger.Info("Ingress reconciled", "operation", op, "name", ingress.Name)
	}

	return nil
}

func ingressPathsForServer(mcpServer *mcpv1alpha1.MCPServer, pathType networkingv1.PathType) []networkingv1.HTTPIngressPath {
	backend := networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: mcpServer.Name,
			Port: networkingv1.ServiceBackendPort{
				Number: mcpServer.Spec.ServicePort,
			},
		},
	}
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     normalizeIngressPath(effectiveIngressPath(mcpServer)),
			PathType: &pathType,
			Backend:  backend,
		},
	}
	if serverUsesOAuth(mcpServer) {
		// Route the metadata document for this server's own public path.
		// auth.audience is tenant input, so deriving the route from it would
		// let one server claim another server's metadata path on a shared,
		// host-less ingress and point its clients at a different issuer.
		paths = append(paths, networkingv1.HTTPIngressPath{
			Path:     oauthresource.ProtectedResourceMetadataPath(effectiveIngressPath(mcpServer)),
			PathType: &pathType,
			Backend:  backend,
		})
	}
	return paths
}

func effectiveIngressHost(mcpServer *mcpv1alpha1.MCPServer) string {
	return strings.TrimSpace(mcpServer.Spec.IngressHost)
}

func effectiveIngressPath(mcpServer *mcpv1alpha1.MCPServer) string {
	return mcpServer.EffectivePublicPath()
}

func normalizeIngressPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func (r *MCPServerReconciler) buildIngressAnnotations(mcpServer *mcpv1alpha1.MCPServer) map[string]string {
	annotations := make(map[string]string)

	// Start with user-provided annotations
	if mcpServer.Spec.IngressAnnotations != nil {
		for k, v := range mcpServer.Spec.IngressAnnotations {
			annotations[k] = v
		}
	}

	// Add controller-specific annotations based on ingress class
	ingressClass := mcpServer.Spec.IngressClass
	if ingressClass == "" {
		ingressClass = DefaultIngressClass
	}

	switch ingressClass {
	case "traefik":
		// Traefik Ingress Controller annotations
		if _, exists := annotations["traefik.ingress.kubernetes.io/router.entrypoints"]; !exists {
			entrypoints := strings.TrimSpace(r.DefaultIngressEntryPoints)
			if entrypoints == "" {
				entrypoints = "web"
			}
			annotations["traefik.ingress.kubernetes.io/router.entrypoints"] = entrypoints
		}
		if r.DefaultIngressTLS {
			if _, exists := annotations["traefik.ingress.kubernetes.io/router.tls"]; !exists {
				annotations["traefik.ingress.kubernetes.io/router.tls"] = "true"
			}
		}

	case "nginx":
		// Nginx Ingress Controller annotations
		if _, exists := annotations["nginx.ingress.kubernetes.io/ssl-redirect"]; !exists {
			annotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "false"
		}

	case "istio":
		// Istio Gateway/VirtualService annotations (Istio uses different approach)
		// For Istio, you typically use Gateway and VirtualService CRDs instead
		// This is a placeholder - Istio integration would need separate CRDs
		if _, exists := annotations["kubernetes.io/ingress.class"]; !exists {
			annotations["kubernetes.io/ingress.class"] = "istio"
		}

	default:
		// Generic ingress annotations for unknown controllers
		if _, exists := annotations["ingress.kubernetes.io/rewrite-target"]; !exists {
			annotations["ingress.kubernetes.io/rewrite-target"] = "/"
		}
	}

	return annotations
}
