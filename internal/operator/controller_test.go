package operator

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func TestRewriteRegistry(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		registry string
		want     string
	}{
		{
			name:     "test-image",
			image:    "test-image",
			registry: "registry.registry.svc.cluster.local:5000",
			want:     "registry.registry.svc.cluster.local:5000/test-image",
		},
	}
	for _, test := range tests {
		got := rewriteRegistry(test.image, test.registry)
		if got != test.want {
			t.Errorf("rewriteRegistry(%q, %q) = %q, want %q", test.image, test.registry, got, test.want)
		}
	}
}

func TestApplyContainerResources(t *testing.T) {
	t.Run("fills all defaults when no overrides", func(t *testing.T) {
		var container corev1.Container
		err := applyContainerResources(&container, mcpv1alpha1.ResourceRequirements{})
		if err != nil {
			t.Fatalf("applyContainerResources() error = %v", err)
		}

		if got := container.Resources.Requests[corev1.ResourceCPU]; got.Cmp(resource.MustParse(defaultRequestCPU)) != 0 {
			t.Fatalf("requests.cpu = %q, want %q", got.String(), defaultRequestCPU)
		}
		if got := container.Resources.Requests[corev1.ResourceMemory]; got.Cmp(resource.MustParse(defaultRequestMemory)) != 0 {
			t.Fatalf("requests.memory = %q, want %q", got.String(), defaultRequestMemory)
		}
		if got := container.Resources.Limits[corev1.ResourceCPU]; got.Cmp(resource.MustParse(defaultLimitCPU)) != 0 {
			t.Fatalf("limits.cpu = %q, want %q", got.String(), defaultLimitCPU)
		}
		if got := container.Resources.Limits[corev1.ResourceMemory]; got.Cmp(resource.MustParse(defaultLimitMemory)) != 0 {
			t.Fatalf("limits.memory = %q, want %q", got.String(), defaultLimitMemory)
		}
	})

	t.Run("overrides specific fields while keeping defaults for others", func(t *testing.T) {
		var container corev1.Container
		resources := mcpv1alpha1.ResourceRequirements{
			Requests: &mcpv1alpha1.ResourceList{
				CPU: "250m",
			},
			Limits: &mcpv1alpha1.ResourceList{
				Memory: "1Gi",
			},
		}

		err := applyContainerResources(&container, resources)
		if err != nil {
			t.Fatalf("applyContainerResources() error = %v", err)
		}

		if got := container.Resources.Requests[corev1.ResourceCPU]; got.Cmp(resource.MustParse("250m")) != 0 {
			t.Fatalf("requests.cpu = %q, want %q", got.String(), "250m")
		}
		if got := container.Resources.Requests[corev1.ResourceMemory]; got.Cmp(resource.MustParse(defaultRequestMemory)) != 0 {
			t.Fatalf("requests.memory = %q, want %q", got.String(), defaultRequestMemory)
		}
		if got := container.Resources.Limits[corev1.ResourceCPU]; got.Cmp(resource.MustParse(defaultLimitCPU)) != 0 {
			t.Fatalf("limits.cpu = %q, want %q", got.String(), defaultLimitCPU)
		}
		if got := container.Resources.Limits[corev1.ResourceMemory]; got.Cmp(resource.MustParse("1Gi")) != 0 {
			t.Fatalf("limits.memory = %q, want %q", got.String(), "1Gi")
		}
	})

	t.Run("returns error for invalid CPU value", func(t *testing.T) {
		var container corev1.Container
		resources := mcpv1alpha1.ResourceRequirements{
			Requests: &mcpv1alpha1.ResourceList{
				CPU: "invalid",
			},
		}

		err := applyContainerResources(&container, resources)
		if err == nil {
			t.Fatal("expected error for invalid CPU value")
		}
	})

	t.Run("returns error for invalid memory value", func(t *testing.T) {
		var container corev1.Container
		resources := mcpv1alpha1.ResourceRequirements{
			Limits: &mcpv1alpha1.ResourceList{
				Memory: "invalid",
			},
		}

		err := applyContainerResources(&container, resources)
		if err == nil {
			t.Fatal("expected error for invalid memory value")
		}
	})
}

func TestSetDefaults(t *testing.T) {
	t.Run("fills all defaults when unset", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-server",
				Namespace: "default",
			},
		}
		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		assertReplicas(t, mcpServer.Spec.Replicas, 1)
		assertEqual(t, "port", mcpServer.Spec.Port, int32(8088))
		assertEqual(t, "servicePort", mcpServer.Spec.ServicePort, int32(80))
		assertEqual(t, "imageTag", mcpServer.Spec.ImageTag, "latest")
		assertEqual(t, "ingressPath", mcpServer.Spec.IngressPath, "/test-server/mcp")
		assertEqual(t, "ingressClass", mcpServer.Spec.IngressClass, "traefik")
	})

	t.Run("preserves existing values", func(t *testing.T) {
		replicas := int32(5)
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "my-server"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Replicas:     &replicas,
				Port:         9000,
				ServicePort:  8080,
				IngressPath:  "/custom/path",
				IngressClass: "nginx",
			},
		}
		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		assertReplicas(t, mcpServer.Spec.Replicas, 5)
		assertEqual(t, "port", mcpServer.Spec.Port, int32(9000))
		assertEqual(t, "servicePort", mcpServer.Spec.ServicePort, int32(8080))
		assertEqual(t, "ingressPath", mcpServer.Spec.IngressPath, "/custom/path")
		assertEqual(t, "ingressClass", mcpServer.Spec.IngressClass, "nginx")
		assertEqual(t, "imageTag", mcpServer.Spec.ImageTag, "latest")
	})

	t.Run("skips imageTag if image has tag", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "nginx:1.19", // Already has tag
			},
		}
		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		assertEqual(t, "imageTag", mcpServer.Spec.ImageTag, "")
	})

	t.Run("skips ingressPath if name is empty", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{} // No name set
		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		assertEqual(t, "ingressPath", mcpServer.Spec.IngressPath, "")
	})
}

func TestReconcileDeploymentLabels(t *testing.T) {
	replicas := int32(1)
	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:       "example.com/test-server",
			ImageTag:    "latest",
			Port:        8088,
			ServicePort: 80,
			Replicas:    &replicas,
		},
	}

	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add apps scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mcpServer).Build()
	reconciler := MCPServerReconciler{
		Client: client,
		Scheme: scheme,
	}

	if err := reconciler.reconcileDeployment(context.Background(), &mcpServer); err != nil {
		t.Fatalf("reconcileDeployment() error = %v", err)
	}

	var deployment appsv1.Deployment
	if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &deployment); err != nil {
		t.Fatalf("failed to fetch deployment: %v", err)
	}

	if deployment.Labels["app"] != mcpServer.Name {
		t.Fatalf("deployment label app = %q, want %q", deployment.Labels["app"], mcpServer.Name)
	}
	if deployment.Labels["app.kubernetes.io/managed-by"] != "mcp-runtime" {
		t.Fatalf("deployment label managed-by = %q, want %q", deployment.Labels["app.kubernetes.io/managed-by"], "mcp-runtime")
	}

	if deployment.Spec.Template.Labels["app"] != mcpServer.Name {
		t.Fatalf("pod template label app = %q, want %q", deployment.Spec.Template.Labels["app"], mcpServer.Name)
	}
	if deployment.Spec.Template.Labels["app.kubernetes.io/managed-by"] != "mcp-runtime" {
		t.Fatalf("pod template label managed-by = %q, want %q", deployment.Spec.Template.Labels["app.kubernetes.io/managed-by"], "mcp-runtime")
	}
}

func assertReplicas(t *testing.T, replicas *int32, want int32) {
	t.Helper()
	if replicas == nil || *replicas != want {
		t.Errorf("replicas = %v, want %d", replicas, want)
	}
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
