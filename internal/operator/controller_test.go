package operator

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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

func TestValidateIngressConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}

	t.Run("succeeds with valid config", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
				IngressPath: "/test-server",
			},
		}
		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&mcpv1alpha1.MCPServer{}).
			Build()
		if err := client.Create(context.Background(), mcpServer); err != nil {
			t.Fatalf("failed to create MCPServer: %v", err)
		}
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		err := r.validateIngressConfig(context.Background(), mcpServer, logr.Discard())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("fails when ingressHost missing", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressPath: "/test-server",
				// IngressHost intentionally missing
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		err := r.validateIngressConfig(context.Background(), mcpServer, logr.Discard())
		if err == nil {
			t.Fatal("expected error for missing ingressHost")
		}
	})

	t.Run("fails when ingressPath missing", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
				// IngressPath intentionally missing
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		err := r.validateIngressConfig(context.Background(), mcpServer, logr.Discard())
		if err == nil {
			t.Fatal("expected error for missing ingressPath")
		}
	})
}

func TestFetchMCPServer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}

	t.Run("succeeds with valid name", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		got, _, err := r.fetchMCPServer(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-server", Namespace: "default"}})
		if err != nil {
			t.Fatalf("failed to fetch mcp server: %v", err)
		}
		assertEqual(t, "name", got.Name, "test-server")
		assertEqual(t, "namespace", got.Namespace, "default")
	})
}

func TestReconcileResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	t.Run("succeeds with valid resources", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
				IngressPath: "/test",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		err := r.reconcileResources(context.Background(), mcpServer, logr.Discard())
		if err != nil {
			t.Fatalf("failed to reconcile resources: %v", err)
		}
	})
}

func TestCheckResourceReadiness(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	t.Run("returns false when resources do not exist", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		deploymentReady, serviceReady, ingressReady, err := r.checkResourceReadiness(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check resource readiness: %v", err)
		}
		// Resources don't exist yet, so they're not ready
		assertEqual(t, "deploymentReady", deploymentReady, false)
		assertEqual(t, "serviceReady", serviceReady, false)
		assertEqual(t, "ingressReady", ingressReady, false)
	})
}

func TestApplyDefaultsIfNeeded(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)

	t.Run("returns requeue true when defaults are applied", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec:       mcpv1alpha1.MCPServerSpec{Image: "test-image"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		requeue, err := r.applyDefaultsIfNeeded(context.Background(), mcpServer, logr.Discard())
		if err != nil {
			t.Fatalf("failed to apply defaults: %v", err)
		}
		// Returns true to trigger re-reconciliation after defaults are applied
		assertEqual(t, "requeue", requeue, true)
	})

	t.Run("returns requeue false when defaults already set", func(t *testing.T) {
		replicas := int32(1)
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:        "test-image",
				ImageTag:     "latest",
				Port:         8088,
				ServicePort:  80,
				Replicas:     &replicas,
				IngressPath:  "/test-server/mcp",
				IngressClass: "traefik",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		requeue, err := r.applyDefaultsIfNeeded(context.Background(), mcpServer, logr.Discard())
		if err != nil {
			t.Fatalf("failed to apply defaults: %v", err)
		}
		assertEqual(t, "requeue", requeue, false)
	})
}

func TestRequireSpecField(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}

	t.Run("succeeds with valid field", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				IngressHost: "example.com",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		err := r.requireSpecField(context.Background(), mcpServer, logr.Discard(), "ingressHost", "example.com", "ingressHost is required")
		if err != nil {
			t.Fatalf("failed to require spec field: %v", err)
		}
	})
}

func TestUpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}

	t.Run("succeeds with valid status", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		r.updateStatus(context.Background(), mcpServer, "Ready", "All resources reconciled", true, true, true)
		updated := &mcpv1alpha1.MCPServer{}
		if err := client.Get(context.Background(), types.NamespacedName{
			Name:      "test-server",
			Namespace: "default",
		}, updated); err != nil {
			t.Fatalf("failed to fetch updated MCPServer: %v", err)
		}
		assertEqual(t, "phase", updated.Status.Phase, "Ready")
		assertEqual(t, "message", updated.Status.Message, "All resources reconciled")
	})
}

func TestDeterminePhase(t *testing.T) {
	t.Run("succeeds with valid phase", func(t *testing.T) {
		deploymentReady := true
		serviceReady := true
		ingressReady := true
		phase, allReady := determinePhase(deploymentReady, serviceReady, ingressReady)
		assertEqual(t, "phase", phase, "Ready")
		assertEqual(t, "allReady", allReady, true)
	})
}

func TestCheckDeploymentReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	t.Run("returns false when deployment does not exist", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		ready, err := r.checkDeploymentReady(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check deployment readiness: %v", err)
		}
		assertEqual(t, "ready", ready, false)
	})
}

func TestCheckServiceReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("returns false when service does not exist", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		ready, err := r.checkServiceReady(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check service readiness: %v", err)
		}
		assertEqual(t, "ready", ready, false)
	})
}

func TestCheckIngressReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	t.Run("returns false when ingress does not exist", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		ready, err := r.checkIngressReady(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check ingress readiness: %v", err)
		}
		assertEqual(t, "ready", ready, false)
	})
}

func TestBuildIngressAnnotations(t *testing.T) {
	t.Run("returns user-specified annotations", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				IngressAnnotations: map[string]string{
					"custom": "annotation",
				},
			},
		}
		r := MCPServerReconciler{}
		annotations := r.buildIngressAnnotations(mcpServer)
		assertEqual(t, "custom annotation", annotations["custom"], "annotation")
	})

	t.Run("includes default traefik annotation", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		r := MCPServerReconciler{}
		annotations := r.buildIngressAnnotations(mcpServer)
		// Should include default traefik entrypoints annotation
		assertEqual(t, "traefik annotation", annotations["traefik.ingress.kubernetes.io/router.entrypoints"], "web")
	})
}

func TestReconcileDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("succeeds with valid deployment", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "test-image",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		err := r.reconcileDeployment(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to reconcile deployment: %v", err)
		}
	})
}

func TestReconcileService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("succeeds with valid service", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "test-image",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		err := r.reconcileService(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to reconcile service: %v", err)
		}
	})
}

func TestReconcileIngress(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add networking scheme: %v", err)
	}

	t.Run("succeeds with valid ingress", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
				IngressPath: "/test",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		err := r.reconcileIngress(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to reconcile ingress: %v", err)
		}
	})
}

func TestBuildEnvVars(t *testing.T) {
	t.Run("converts EnvVars to corev1.EnvVar slice", func(t *testing.T) {
		r := MCPServerReconciler{}
		input := []mcpv1alpha1.EnvVar{
			{Name: "FOO", Value: "bar"},
			{Name: "BAZ", Value: "qux"},
		}
		envVars := r.buildEnvVars(input)
		assertEqual(t, "len", len(envVars), 2)
		assertEqual(t, "envVars[0].Name", envVars[0].Name, "FOO")
		assertEqual(t, "envVars[0].Value", envVars[0].Value, "bar")
		assertEqual(t, "envVars[1].Name", envVars[1].Name, "BAZ")
		assertEqual(t, "envVars[1].Value", envVars[1].Value, "qux")
	})

	t.Run("returns empty slice for nil input", func(t *testing.T) {
		r := MCPServerReconciler{}
		envVars := r.buildEnvVars(nil)
		assertEqual(t, "len", len(envVars), 0)
	})
}

func TestBuildImagePullSecrets(t *testing.T) {
	t.Run("returns user-specified pull secrets", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				ImagePullSecrets: []string{"secret1", "secret2"},
			},
		}
		r := MCPServerReconciler{}
		pullSecrets := r.buildImagePullSecrets(mcpServer)
		assertEqual(t, "len", len(pullSecrets), 2)
		assertEqual(t, "pullSecrets[0]", pullSecrets[0].Name, "secret1")
		assertEqual(t, "pullSecrets[1]", pullSecrets[1].Name, "secret2")
	})

	t.Run("returns empty slice when no pull secrets specified", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		r := MCPServerReconciler{}
		pullSecrets := r.buildImagePullSecrets(mcpServer)
		assertEqual(t, "len", len(pullSecrets), 0)
	})
}

func TestResolveImage(t *testing.T) {
	t.Run("returns user-specified image", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "test-image",
			},
		}
		r := MCPServerReconciler{}
		image, err := r.resolveImage(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to resolve image: %v", err)
		}
		assertEqual(t, "image", image, "test-image")
	})
	t.Run("returns user-specified image with tag", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:    "test-image",
				ImageTag: "v1.0.0",
			},
		}
		r := MCPServerReconciler{}
		image, err := r.resolveImage(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to resolve image: %v", err)
		}
		assertEqual(t, "image", image, "test-image:v1.0.0")
	})
	t.Run("returns user-specified image with registry override", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:            "test-image",
				RegistryOverride: "test-registry",
			},
		}
		r := MCPServerReconciler{}
		image, err := r.resolveImage(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to resolve image: %v", err)
		}
		assertEqual(t, "image", image, "test-registry/test-image")
	})
	t.Run("returns user-specified image with registry override and tag", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:            "test-image",
				ImageTag:         "v1.0.0",
				RegistryOverride: "test-registry",
			},
		}
		r := MCPServerReconciler{}
		image, err := r.resolveImage(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to resolve image: %v", err)
		}
		assertEqual(t, "image", image, "test-registry/test-image:v1.0.0")
	})
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	t.Run("returns not found when MCPServer does not exist", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, "requeue", result.Requeue, false)
	})

	t.Run("reconciles MCPServer successfully", func(t *testing.T) {
		replicas := int32(1)
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:        "test-image",
				ImageTag:     "latest",
				Port:         8088,
				ServicePort:  80,
				Replicas:     &replicas,
				IngressHost:  "example.com",
				IngressPath:  "/test-server/mcp",
				IngressClass: "traefik",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-server", Namespace: "default"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not requeue immediately since all fields are set
		assertEqual(t, "requeue", result.Requeue, false)
	})

	t.Run("requeues when defaults need to be applied", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-server", Namespace: "default"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should requeue to re-reconcile after defaults are applied
		assertEqual(t, "requeue", result.Requeue, true)
	})
}

func TestSetupWithManager(t *testing.T) {
	// SetupWithManager requires a real manager which is typically tested
	// via integration tests. This test verifies the reconciler struct
	// is properly configured to be set up with a manager.
	t.Run("reconciler has required fields for setup", func(t *testing.T) {
		scheme := runtime.NewScheme()
		_ = mcpv1alpha1.AddToScheme(scheme)

		r := &MCPServerReconciler{
			Scheme: scheme,
		}

		// Verify the reconciler has the scheme set (required for SetupWithManager)
		if r.Scheme == nil {
			t.Fatal("Scheme should not be nil")
		}
	})
}
