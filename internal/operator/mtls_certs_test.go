package operator

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func mtlsServer() *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "secure-server", Namespace: "mcp-servers"},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:       "example.com/secure-server",
			ServicePort: 80,
			Gateway:     &mcpv1alpha1.GatewayConfig{Enabled: true, Port: 8091, Image: "example.com/gw:latest"},
			Auth:        &mcpv1alpha1.AuthConfig{Mode: mcpv1alpha1.AuthModeMTLS, TrustDomain: "example.org"},
		},
	}
}

func TestTraefikProxySPIFFEID(t *testing.T) {
	if got := traefikProxySPIFFEID(mtlsServer()); got != "spiffe://example.org/ns/traefik/sa/traefik" {
		t.Fatalf("traefikProxySPIFFEID = %q", got)
	}
	noTrust := mtlsServer()
	noTrust.Spec.Auth.TrustDomain = ""
	if got := traefikProxySPIFFEID(noTrust); got != "" {
		t.Fatalf("traefikProxySPIFFEID without trust domain = %q, want empty", got)
	}
}

func TestReconcileTraefikClientCertificateRequiresIssuerAndTrustDomain(t *testing.T) {
	t.Run("missing issuer", func(t *testing.T) {
		r := MCPServerReconciler{} // no MTLSClusterIssuer
		err := r.reconcileTraefikClientCertificate(context.Background(), mtlsServer())
		if err == nil || !strings.Contains(err.Error(), "MCP_MTLS_CLUSTER_ISSUER") {
			t.Fatalf("err = %v, want missing-issuer error", err)
		}
	})
	t.Run("missing trust domain", func(t *testing.T) {
		r := MCPServerReconciler{MTLSClusterIssuer: "mcp-runtime-ca"}
		server := mtlsServer()
		server.Spec.Auth.TrustDomain = ""
		err := r.reconcileTraefikClientCertificate(context.Background(), server)
		if err == nil || !strings.Contains(err.Error(), "trustDomain") {
			t.Fatalf("err = %v, want missing-trust-domain error", err)
		}
	})
}

func TestReconcileMTLSTrustBundle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	bundleKey := types.NamespacedName{Name: "secure-server-mtls-ca", Namespace: "mcp-servers"}
	gatewaySecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "secure-server-gateway-mtls", Namespace: "mcp-servers"},
			Data:       map[string][]byte{"ca.crt": []byte("CA-PEM"), "tls.crt": []byte("LEAF"), "tls.key": []byte("KEY")},
		}
	}

	t.Run("materializes bundle from gateway ca.crt", func(t *testing.T) {
		server := mtlsServer()
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server, gatewaySecret()).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		if err := r.reconcileMTLSTrustBundle(context.Background(), server); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		var bundle corev1.Secret
		if err := client.Get(context.Background(), bundleKey, &bundle); err != nil {
			t.Fatalf("expected bundle secret: %v", err)
		}
		if string(bundle.Data["tls.ca"]) != "CA-PEM" || string(bundle.Data["ca.crt"]) != "CA-PEM" {
			t.Fatalf("bundle data = %v, want CA-PEM under tls.ca and ca.crt", bundle.Data)
		}
	})

	t.Run("skips when gateway certificate not yet issued", func(t *testing.T) {
		server := mtlsServer()
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		if err := r.reconcileMTLSTrustBundle(context.Background(), server); err != nil {
			t.Fatalf("reconcile should not error when gateway secret absent: %v", err)
		}
		var bundle corev1.Secret
		if err := client.Get(context.Background(), bundleKey, &bundle); !apierrors.IsNotFound(err) {
			t.Fatalf("expected no bundle yet, got %v", err)
		}
	})

	t.Run("deleted for non-mtls", func(t *testing.T) {
		server := mtlsServer()
		server.Spec.Auth.Mode = mcpv1alpha1.AuthModeHeader
		existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secure-server-mtls-ca", Namespace: "mcp-servers"}}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server, existing).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		if err := r.reconcileMTLSTrustBundle(context.Background(), server); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		var bundle corev1.Secret
		if err := client.Get(context.Background(), bundleKey, &bundle); !apierrors.IsNotFound(err) {
			t.Fatalf("expected bundle deleted for non-mtls, got %v", err)
		}
	})
}

func TestGatewaySidecarPinsTrustedProxyForMTLS(t *testing.T) {
	r := MCPServerReconciler{}
	container, err := r.buildGatewayContainer(mtlsServer())
	if err != nil {
		t.Fatalf("buildGatewayContainer: %v", err)
	}
	env := map[string]string{}
	for _, e := range container.Env {
		env[e.Name] = e.Value
	}
	if env["TRUSTED_PROXY_SPIFFE_ID"] != "spiffe://example.org/ns/traefik/sa/traefik" {
		t.Fatalf("TRUSTED_PROXY_SPIFFE_ID = %q, want the traefik SPIFFE id", env["TRUSTED_PROXY_SPIFFE_ID"])
	}
	if env["TLS_CLIENT_CA_FILE"] == "" {
		t.Fatal("expected TLS_CLIENT_CA_FILE to be set for mtls gateway")
	}
}
