package operator

import (
	"context"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func (r *MCPServerReconciler) reconcileIngress(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		pathType := networkingv1.PathTypePrefix
		ingressClassName := mcpServer.Spec.IngressClass
		if ingressClassName == "" {
			ingressClassName = "traefik" // Default to traefik
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
		paths = append(paths, networkingv1.HTTPIngressPath{
			Path:     oauthProtectedResourceIngressPath(effectiveIngressPath(mcpServer)),
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
	prefix := strings.Trim(strings.TrimSpace(mcpServer.Spec.PublicPathPrefix), "/")
	if prefix == "" {
		return mcpServer.Spec.IngressPath
	}
	return "/" + prefix + "/mcp"
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

func oauthProtectedResourceIngressPath(ingressPath string) string {
	normalized := normalizeIngressPath(ingressPath)
	if normalized == "/" {
		return "/.well-known/oauth-protected-resource"
	}
	return "/.well-known/oauth-protected-resource" + normalized
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
		ingressClass = "traefik" // Default to traefik
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
