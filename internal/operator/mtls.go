package operator

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

const (
	gatewayTLSVolumeName = "gateway-mtls"
	gatewayTLSMountDir   = "/var/run/mcp-runtime/tls"
)

var certificateGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Certificate",
}

var ingressRouteTCPGVK = schema.GroupVersionKind{
	Group: "traefik.io", Version: "v1alpha1", Kind: "IngressRouteTCP",
}

func serverUsesMTLS(mcpServer *mcpv1alpha1.MCPServer) bool {
	return mcpServer != nil && mcpServer.Spec.Auth != nil && mcpServer.Spec.Auth.Mode == mcpv1alpha1.AuthModeMTLS
}

func gatewayTLSSecretName(mcpServer *mcpv1alpha1.MCPServer) string {
	return mcpServer.Name + "-gateway-mtls"
}

func (r *MCPServerReconciler) reconcileGatewayCertificate(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(certificateGVK)
	certificate.SetName(gatewayTLSSecretName(mcpServer))
	certificate.SetNamespace(mcpServer.Namespace)

	if !serverUsesMTLS(mcpServer) {
		if err := r.Delete(ctx, certificate); err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return err
		}
		return nil
	}
	issuer := strings.TrimSpace(r.MTLSClusterIssuer)
	if issuer == "" {
		return fmt.Errorf("auth.mode mtls requires MCP_MTLS_CLUSTER_ISSUER on the operator")
	}

	dnsNames := []any{
		mcpServer.Name,
		mcpServer.Name + "." + mcpServer.Namespace,
		mcpServer.Name + "." + mcpServer.Namespace + ".svc",
		mcpServer.Name + "." + mcpServer.Namespace + ".svc.cluster.local",
	}
	if host := effectiveIngressHost(mcpServer); host != "" {
		dnsNames = append(dnsNames, host)
	}
	certificate.Object["spec"] = map[string]any{
		"secretName":  gatewayTLSSecretName(mcpServer),
		"duration":    "24h",
		"renewBefore": "8h",
		"dnsNames":    dnsNames,
		"usages":      []any{"digital signature", "key encipherment", "server auth"},
		"issuerRef": map[string]any{
			"group": "cert-manager.io",
			"kind":  "ClusterIssuer",
			"name":  issuer,
		},
	}
	certificate.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "mcp-runtime",
		"mcpruntime.org/server":        mcpServer.Name,
	})
	certificate.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(mcpServer, mcpv1alpha1.GroupVersion.WithKind("MCPServer"))})

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(certificateGVK)
	key := types.NamespacedName{Name: certificate.GetName(), Namespace: certificate.GetNamespace()}
	if err := r.Get(ctx, key, current); err == nil {
		currentSpec, _, _ := unstructured.NestedMap(current.Object, "spec")
		desiredSpec, _, _ := unstructured.NestedMap(certificate.Object, "spec")
		if !reflect.DeepEqual(currentSpec, desiredSpec) ||
			!reflect.DeepEqual(current.GetLabels(), certificate.GetLabels()) ||
			!reflect.DeepEqual(current.GetOwnerReferences(), certificate.GetOwnerReferences()) {
			certificate.SetResourceVersion(current.GetResourceVersion())
			return r.Update(ctx, certificate)
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	return r.Create(ctx, certificate)
}

func (r *MCPServerReconciler) deleteMTLSIngress(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(ingressRouteTCPGVK)
	route.SetName(mcpServer.Name)
	route.SetNamespace(mcpServer.Namespace)
	if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}
	return nil
}

func (r *MCPServerReconciler) reconcileMTLSIngress(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(ingressRouteTCPGVK)
	route.SetName(mcpServer.Name)
	route.SetNamespace(mcpServer.Namespace)
	route.Object["spec"] = map[string]any{
		"entryPoints": []any{"websecure"},
		"routes": []any{map[string]any{
			"match": fmt.Sprintf("HostSNI(`%s`)", effectiveIngressHost(mcpServer)),
			"services": []any{map[string]any{
				"name": mcpServer.Name,
				"port": int64(mcpServer.Spec.ServicePort),
			}},
		}},
		"tls": map[string]any{"passthrough": true},
	}
	route.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(mcpServer, mcpv1alpha1.GroupVersion.WithKind("MCPServer"))})

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(ingressRouteTCPGVK)
	key := types.NamespacedName{Name: route.GetName(), Namespace: route.GetNamespace()}
	if err := r.Get(ctx, key, current); err == nil {
		currentSpec, _, _ := unstructured.NestedMap(current.Object, "spec")
		desiredSpec, _, _ := unstructured.NestedMap(route.Object, "spec")
		if !reflect.DeepEqual(currentSpec, desiredSpec) ||
			!reflect.DeepEqual(current.GetOwnerReferences(), route.GetOwnerReferences()) {
			route.SetResourceVersion(current.GetResourceVersion())
			return r.Update(ctx, route)
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	return r.Create(ctx, route)
}
