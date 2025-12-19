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

func TestApplyContainerResourceDefaults(t *testing.T) {
	t.Run("fills all defaults when unset", func(t *testing.T) {
		var container corev1.Container
		applyContainerResourceDefaults(&container)

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

	t.Run("fills only missing fields", func(t *testing.T) {
		container := corev1.Container{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("250m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}

		applyContainerResourceDefaults(&container)

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

		// Check actual values
		if mcpServer.Spec.Replicas == nil || *mcpServer.Spec.Replicas != 1 {
			t.Errorf("replicas = %v, want 1", mcpServer.Spec.Replicas)
		}
		if mcpServer.Spec.Port != 8088 {
			t.Errorf("port = %d, want 8088", mcpServer.Spec.Port)
		}
		if mcpServer.Spec.ServicePort != 80 {
			t.Errorf("servicePort = %d, want 80", mcpServer.Spec.ServicePort)
		}
		if mcpServer.Spec.ImageTag != "latest" {
			t.Errorf("imageTag = %q, want 'latest'", mcpServer.Spec.ImageTag)
		}
		if mcpServer.Spec.IngressPath != "/test-server/mcp" {
			t.Errorf("ingressPath = %q, want '/test-server/mcp'", mcpServer.Spec.IngressPath)
		}
		if mcpServer.Spec.IngressClass != "traefik" {
			t.Errorf("ingressClass = %q, want 'traefik'", mcpServer.Spec.IngressClass)
		}
		// IngressHost only set from env var - not tested here
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

		// These should NOT be changed
		if *mcpServer.Spec.Replicas != 5 {
			t.Errorf("replicas changed to %d, should stay 5", *mcpServer.Spec.Replicas)
		}
		if mcpServer.Spec.Port != 9000 {
			t.Errorf("port changed to %d, should stay 9000", mcpServer.Spec.Port)
		}
		if mcpServer.Spec.ServicePort != 8080 {
			t.Errorf("servicePort changed to %d, should stay 8080", mcpServer.Spec.ServicePort)
		}
		if mcpServer.Spec.IngressPath != "/custom/path" {
			t.Errorf("ingressPath changed to %q, should stay '/custom/path'", mcpServer.Spec.IngressPath)
		}
		if mcpServer.Spec.IngressClass != "nginx" {
			t.Errorf("ingressClass changed to %q, should stay 'nginx'", mcpServer.Spec.IngressClass)
		}
		// ImageTag should be set since it was empty
		if mcpServer.Spec.ImageTag != "latest" {
			t.Errorf("imageTag = %q, want 'latest'", mcpServer.Spec.ImageTag)
		}
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

		if mcpServer.Spec.ImageTag != "" {
			t.Errorf("imageTag = %q, should stay empty when image has tag", mcpServer.Spec.ImageTag)
		}
	})

	t.Run("skips ingressPath if name is empty", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{} // No name set
		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		// IngressPath should remain empty (not "//mcp")
		if mcpServer.Spec.IngressPath != "" {
			t.Errorf("ingressPath = %q, should stay empty when name is empty", mcpServer.Spec.IngressPath)
		}
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
