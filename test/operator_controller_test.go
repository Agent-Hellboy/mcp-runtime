package operator_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/internal/operator"
)

func TestMCPServerReconciler_ReconcileCreatesResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&mcpv1alpha1.MCPServer{}).
		Build()

	reconciler := &operator.MCPServerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	mcpServer := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "mcp-servers",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:        "my-registry.com/my-image:v1.0",
			ImageTag:     "v1.0",
			Port:         9000,
			ServicePort:  8080,
			Replicas:     int32Ptr(3),
			IngressPath:  "/custom/path",
			IngressHost:  "example.com",
			IngressClass: "traefik",
			IngressAnnotations: map[string]string{
				"custom": "annotation",
			},
		},
	}

	ctx := context.Background()
	if err := fakeClient.Create(ctx, mcpServer); err != nil {
		t.Fatalf("failed to create MCPServer: %v", err)
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-server",
			Namespace: "mcp-servers",
		},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	key := types.NamespacedName{Name: "test-server", Namespace: "mcp-servers"}
	assertDeployment(t, ctx, fakeClient, key, 3, "my-registry.com/my-image:v1.0", 9000)
	assertService(t, ctx, fakeClient, key, 8080, 9000)
	assertIngress(t, ctx, fakeClient, key, "/custom/path", "example.com", "traefik", "custom", "annotation")
}

func int32Ptr(i int32) *int32 {
	return &i
}

func assertDeployment(t *testing.T, ctx context.Context, client client.Client, key types.NamespacedName, replicas int32, image string, port int32) {
	t.Helper()

	deployment := &appsv1.Deployment{}
	if err := client.Get(ctx, key, deployment); err != nil {
		t.Fatalf("expected Deployment to be created: %v", err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != replicas {
		t.Errorf("expected %d replicas, got %v", replicas, deployment.Spec.Replicas)
	}
	if deployment.Spec.Template.Spec.Containers[0].Image != image {
		t.Errorf("expected image %s, got %s", image, deployment.Spec.Template.Spec.Containers[0].Image)
	}
	if deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != port {
		t.Errorf("expected container port %d, got %d", port, deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
	}
}

func assertService(t *testing.T, ctx context.Context, client client.Client, key types.NamespacedName, servicePort, targetPort int32) {
	t.Helper()

	service := &corev1.Service{}
	if err := client.Get(ctx, key, service); err != nil {
		t.Fatalf("expected Service to be created: %v", err)
	}
	if service.Spec.Ports[0].Port != servicePort {
		t.Errorf("expected service port %d, got %d", servicePort, service.Spec.Ports[0].Port)
	}
	if service.Spec.Ports[0].TargetPort.IntVal != targetPort {
		t.Errorf("expected service target port %d, got %d", targetPort, service.Spec.Ports[0].TargetPort.IntVal)
	}
}

func assertIngress(t *testing.T, ctx context.Context, client client.Client, key types.NamespacedName, path, host, className, annotationKey, annotationValue string) {
	t.Helper()

	ingress := &networkingv1.Ingress{}
	if err := client.Get(ctx, key, ingress); err != nil {
		t.Fatalf("expected Ingress to be created: %v", err)
	}
	if ingress.Spec.Rules[0].HTTP.Paths[0].Path != path {
		t.Errorf("expected ingress path %s, got %s", path, ingress.Spec.Rules[0].HTTP.Paths[0].Path)
	}
	if ingress.Spec.Rules[0].Host != host {
		t.Errorf("expected ingress host %s, got %s", host, ingress.Spec.Rules[0].Host)
	}
	if ingress.Spec.IngressClassName == nil || *ingress.Spec.IngressClassName != className {
		t.Errorf("expected ingress class %s, got %v", className, ingress.Spec.IngressClassName)
	}
	if ingress.Annotations[annotationKey] != annotationValue {
		t.Errorf("expected custom annotation to be present, got %v", ingress.Annotations[annotationKey])
	}
}
