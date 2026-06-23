package operator

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

const (
	// traefikNamespace is where the ingress controller runs; the gateway only
	// honors the injected verified-identity header on connections from it.
	traefikNamespace = "traefik"
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

func mtlsNetworkPolicyName(mcpServer *mcpv1alpha1.MCPServer) string {
	return mcpServer.Name + "-mtls-gateway"
}

// reconcileMTLSNetworkPolicy restricts ingress to the gateway port so the
// verified-identity header can only originate from the TLS-terminating ingress.
// Without it, any pod could connect directly to the gateway and forge the
// header. It is defense-in-depth on top of the gateway's own requirement that
// the connection be a verified mTLS hop (see authenticateMTLS). The gateway
// metrics port stays open so Prometheus scraping is unaffected.
func (r *MCPServerReconciler) reconcileMTLSNetworkPolicy(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mtlsNetworkPolicyName(mcpServer),
			Namespace: mcpServer.Namespace,
		},
	}
	if !serverUsesMTLS(mcpServer) {
		if err := r.Delete(ctx, policy); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	gatewayPort := mcpServer.Spec.Gateway.Port
	metricsPort := int32(DefaultGatewayMetricsPort)
	tcp := corev1.ProtocolTCP

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		gatewayTarget := intstr.FromInt32(gatewayPort)
		metricsTarget := intstr.FromInt32(metricsPort)
		policy.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": mcpServer.Name},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Only the ingress controller may reach the gateway port.
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": traefikNamespace},
						},
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "traefik"},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &gatewayTarget}},
				},
				{
					// Metrics scraping is allowed from any source.
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &metricsTarget}},
				},
			},
		}
		return ctrl.SetControllerReference(mcpServer, policy, r.Scheme)
	})
	return err
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
