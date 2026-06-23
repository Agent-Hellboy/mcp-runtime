package operator

import (
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func traefikScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	for _, gvk := range []schema.GroupVersionKind{
		ingressRouteGVK, middlewareGVK, tlsOptionGVK, serversTransportGVK, ingressRouteTCPGVK,
	} {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}
	return scheme
}

func getCR(t *testing.T, c client.Client, gvk schema.GroupVersionKind, name, ns string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: ns}, obj); err != nil {
		t.Fatalf("get %s/%s: %v", gvk.Kind, name, err)
	}
	return obj
}

func crFixture(gvk schema.GroupVersionKind, name, ns string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvk)
	o.SetName(name)
	o.SetNamespace(ns)
	return o
}

func TestReconcileMTLSIngressGeneratesTraefikResources(t *testing.T) {
	scheme := traefikScheme(t)
	server := mtlsServer()
	server.Spec.PublicPathPrefix = "secure-server"
	server.Spec.IngressHost = "mcp.example.com"

	// A leftover passthrough route from the old model must be removed.
	legacy := crFixture(ingressRouteTCPGVK, server.Name, server.Namespace)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server, legacy).Build()
	r := MCPServerReconciler{Client: client, Scheme: scheme}

	if err := r.reconcileMTLSIngress(context.Background(), server); err != nil {
		t.Fatalf("reconcileMTLSIngress: %v", err)
	}

	leftover := &unstructured.Unstructured{}
	leftover.SetGroupVersionKind(ingressRouteTCPGVK)
	if err := client.Get(context.Background(), types.NamespacedName{Name: server.Name, Namespace: server.Namespace}, leftover); !apierrors.IsNotFound(err) {
		t.Fatalf("legacy IngressRouteTCP should be deleted, got %v", err)
	}

	tlsOpt := getCR(t, client, tlsOptionGVK, mtlsTLSOptionName(server), server.Namespace)
	if authType, _, _ := unstructured.NestedString(tlsOpt.Object, "spec", "clientAuth", "clientAuthType"); authType != "RequireAndVerifyClientCert" {
		t.Fatalf("clientAuthType = %q", authType)
	}
	if secrets, _, _ := unstructured.NestedStringSlice(tlsOpt.Object, "spec", "clientAuth", "secretNames"); len(secrets) != 1 || secrets[0] != mtlsTrustBundleSecretName(server) {
		t.Fatalf("clientAuth secretNames = %v, want trust bundle", secrets)
	}

	mw := getCR(t, client, middlewareGVK, mtlsMiddlewareName(server), server.Namespace)
	if header, _, _ := unstructured.NestedString(mw.Object, "spec", "plugin", spiffeIdentityPluginName, "verifiedHeader"); header != verifiedSPIFFEHeader {
		t.Fatalf("middleware verifiedHeader = %q", header)
	}
	if td, _, _ := unstructured.NestedString(mw.Object, "spec", "plugin", spiffeIdentityPluginName, "trustDomain"); td != "example.org" {
		t.Fatalf("middleware trustDomain = %q", td)
	}

	st := getCR(t, client, serversTransportGVK, mtlsServersTransportName(server), server.Namespace)
	if sn, _, _ := unstructured.NestedString(st.Object, "spec", "serverName"); sn != gatewayServerName(server) {
		t.Fatalf("serversTransport serverName = %q", sn)
	}
	if certs, _, _ := unstructured.NestedStringSlice(st.Object, "spec", "certificatesSecrets"); len(certs) != 1 || certs[0] != traefikClientCertSecretName(server) {
		t.Fatalf("certificatesSecrets = %v", certs)
	}
	if roots, _, _ := unstructured.NestedStringSlice(st.Object, "spec", "rootCAsSecrets"); len(roots) != 1 || roots[0] != mtlsTrustBundleSecretName(server) {
		t.Fatalf("rootCAsSecrets = %v", roots)
	}

	ir := getCR(t, client, ingressRouteGVK, server.Name, server.Namespace)
	routes, _, _ := unstructured.NestedSlice(ir.Object, "spec", "routes")
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	match, _ := routes[0].(map[string]any)["match"].(string)
	if !strings.Contains(match, "Host(`mcp.example.com`)") || !strings.Contains(match, "PathPrefix(`/secure-server/mcp`)") {
		t.Fatalf("match = %q, want host + path prefix", match)
	}
	if tlsName, _, _ := unstructured.NestedString(ir.Object, "spec", "tls", "options", "name"); tlsName != mtlsTLSOptionName(server) {
		t.Fatalf("tls.options.name = %q", tlsName)
	}
}

func TestReconcileMTLSIngressHostTLSSecret(t *testing.T) {
	scheme := traefikScheme(t)
	server := mtlsServer()
	server.Spec.IngressHost = "mcp.example.com"
	server.Spec.PublicPathPrefix = "secure-demo"

	t.Run("references the platform host secret when configured", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
		r := MCPServerReconciler{Client: c, Scheme: scheme, DefaultIngressTLSSecret: "platform-host-tls"}
		if err := r.reconcileMTLSIngress(context.Background(), server); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		ir := getCR(t, c, ingressRouteGVK, server.Name, server.Namespace)
		if sn, _, _ := unstructured.NestedString(ir.Object, "spec", "tls", "secretName"); sn != "platform-host-tls" {
			t.Fatalf("tls.secretName = %q, want platform-host-tls", sn)
		}
	})

	t.Run("omits secretName when not configured", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
		r := MCPServerReconciler{Client: c, Scheme: scheme}
		if err := r.reconcileMTLSIngress(context.Background(), server); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		ir := getCR(t, c, ingressRouteGVK, server.Name, server.Namespace)
		if _, found, _ := unstructured.NestedString(ir.Object, "spec", "tls", "secretName"); found {
			t.Fatal("tls.secretName should be absent without DefaultIngressTLSSecret (Traefik default resolver)")
		}
		if name, _, _ := unstructured.NestedString(ir.Object, "spec", "tls", "options", "name"); name != mtlsTLSOptionName(server) {
			t.Fatalf("tls.options.name = %q", name)
		}
	})
}

func TestDeleteMTLSIngressRemovesAllResources(t *testing.T) {
	scheme := traefikScheme(t)
	server := mtlsServer()

	targets := []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{ingressRouteGVK, server.Name},
		{middlewareGVK, mtlsMiddlewareName(server)},
		{tlsOptionGVK, mtlsTLSOptionName(server)},
		{serversTransportGVK, mtlsServersTransportName(server)},
		{ingressRouteTCPGVK, server.Name},
	}
	objs := []client.Object{server}
	for _, target := range targets {
		objs = append(objs, crFixture(target.gvk, target.name, server.Namespace))
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := MCPServerReconciler{Client: c, Scheme: scheme}

	if err := r.deleteMTLSIngress(context.Background(), server); err != nil {
		t.Fatalf("deleteMTLSIngress: %v", err)
	}
	for _, target := range targets {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(target.gvk)
		if err := c.Get(context.Background(), types.NamespacedName{Name: target.name, Namespace: server.Namespace}, obj); !apierrors.IsNotFound(err) {
			t.Fatalf("%s/%s should be deleted, got %v", target.gvk.Kind, target.name, err)
		}
	}
}
