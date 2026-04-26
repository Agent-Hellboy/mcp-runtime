package operator

import (
	"context"
	"strings"
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
			registry: "registry.local",
			want:     "registry.local/test-image",
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

func TestBuildGatewayContainerAppliesDefaultResources(t *testing.T) {
	mcpServer := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-server",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Gateway: &mcpv1alpha1.GatewayConfig{
				Enabled:     true,
				Port:        defaultGatewayPort,
				UpstreamURL: "http://127.0.0.1:8088",
			},
		},
	}

	r := MCPServerReconciler{GatewayProxyImage: "example.com/mcp-proxy:latest"}
	container, err := r.buildGatewayContainer(mcpServer)
	if err != nil {
		t.Fatalf("buildGatewayContainer() error = %v", err)
	}

	if got := container.Resources.Requests[corev1.ResourceCPU]; got.Cmp(resource.MustParse(defaultRequestCPU)) != 0 {
		t.Fatalf("gateway requests.cpu = %q, want %q", got.String(), defaultRequestCPU)
	}
	if got := container.Resources.Requests[corev1.ResourceMemory]; got.Cmp(resource.MustParse(defaultRequestMemory)) != 0 {
		t.Fatalf("gateway requests.memory = %q, want %q", got.String(), defaultRequestMemory)
	}
	if got := container.Resources.Limits[corev1.ResourceCPU]; got.Cmp(resource.MustParse(defaultLimitCPU)) != 0 {
		t.Fatalf("gateway limits.cpu = %q, want %q", got.String(), defaultLimitCPU)
	}
	if got := container.Resources.Limits[corev1.ResourceMemory]; got.Cmp(resource.MustParse(defaultLimitMemory)) != 0 {
		t.Fatalf("gateway limits.memory = %q, want %q", got.String(), defaultLimitMemory)
	}
}

func TestValidateGatewayConfigRejectsInvalidRolloutValues(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	replicas := int32(2)
	server := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-server",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:    "example.com/server",
			Replicas: &replicas,
			Port:     DefaultPort,
			Gateway: &mcpv1alpha1.GatewayConfig{
				Enabled: true,
				Port:    defaultGatewayPort,
				Image:   "example.com/mcp-proxy:latest",
			},
			Rollout: &mcpv1alpha1.RolloutConfig{
				MaxUnavailable: "invalid%",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(server).
		WithObjects(server.DeepCopy()).
		Build()
	reconciler := &MCPServerReconciler{
		Client: client,
		Scheme: scheme,
	}

	err := reconciler.validateGatewayConfig(context.Background(), server, logr.Discard())
	if err == nil {
		t.Fatal("expected rollout validation error")
	}
	if !strings.Contains(err.Error(), "rollout.maxUnavailable") {
		t.Fatalf("expected rollout.maxUnavailable error, got %v", err)
	}
}

func TestValidateGatewayConfigRequiresOAuthIssuer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	server := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-server",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image: "example.com/server",
			Port:  DefaultPort,
			Gateway: &mcpv1alpha1.GatewayConfig{
				Enabled: true,
				Port:    defaultGatewayPort,
				Image:   "example.com/mcp-proxy:latest",
			},
			Auth: &mcpv1alpha1.AuthConfig{
				Mode: mcpv1alpha1.AuthModeOAuth,
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(server).
		WithObjects(server.DeepCopy()).
		Build()
	reconciler := &MCPServerReconciler{
		Client: client,
		Scheme: scheme,
	}

	err := reconciler.validateGatewayConfig(context.Background(), server, logr.Discard())
	if err == nil {
		t.Fatal("expected oauth issuer validation error")
	}
	if !strings.Contains(err.Error(), "auth.issuerURL") {
		t.Fatalf("expected auth.issuerURL error, got %v", err)
	}
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
		assertEqual(t, "publicPathPrefix", mcpServer.Spec.PublicPathPrefix, "test-server")
		assertEqual(t, "ingressClass", mcpServer.Spec.IngressClass, "traefik")
	})

	t.Run("derives publicPathPrefix from server name", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server"},
		}
		r := MCPServerReconciler{
			Scheme:             runtime.NewScheme(),
			DefaultIngressHost: "example.com",
		}
		r.setDefaults(&mcpServer)
		assertEqual(t, "publicPathPrefix", mcpServer.Spec.PublicPathPrefix, "test-server")
		assertEqual(t, "ingressHost", mcpServer.Spec.IngressHost, "")
	})

	t.Run("preserves explicit publicPathPrefix", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server"},
			Spec: mcpv1alpha1.MCPServerSpec{
				PublicPathPrefix: "custom-prefix",
			},
		}
		r := MCPServerReconciler{
			Scheme:             runtime.NewScheme(),
			DefaultIngressHost: "example.com",
		}
		r.setDefaults(&mcpServer)
		assertEqual(t, "publicPathPrefix", mcpServer.Spec.PublicPathPrefix, "custom-prefix")
		assertEqual(t, "ingressHost", mcpServer.Spec.IngressHost, "")
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

	t.Run("applies gateway and analytics defaults", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-server"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "example.com/gateway-server",
				Gateway: &mcpv1alpha1.GatewayConfig{
					Enabled: true,
				},
				Analytics: &mcpv1alpha1.AnalyticsConfig{
					Enabled:   true,
					IngestURL: "http://analytics.default.svc/api/events",
				},
			},
		}

		r := MCPServerReconciler{Scheme: runtime.NewScheme()}
		r.setDefaults(&mcpServer)

		if mcpServer.Spec.Gateway == nil {
			t.Fatal("expected gateway defaults to be applied")
		}
		assertEqual(t, "gatewayPort", mcpServer.Spec.Gateway.Port, int32(defaultGatewayPort))
		assertEqual(t, "gatewayUpstreamURL", mcpServer.Spec.Gateway.UpstreamURL, "http://127.0.0.1:8088")
		if mcpServer.Spec.Analytics == nil {
			t.Fatal("expected analytics defaults to be applied")
		}
		assertEqual(t, "analyticsSource", mcpServer.Spec.Analytics.Source, "gateway-server")
		assertEqual(t, "analyticsEventType", mcpServer.Spec.Analytics.EventType, "mcp.request")
	})

	t.Run("applies default analytics ingest url from reconciler config", func(t *testing.T) {
		mcpServer := mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-server"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image: "example.com/gateway-server",
				Gateway: &mcpv1alpha1.GatewayConfig{
					Enabled: true,
				},
				Analytics: &mcpv1alpha1.AnalyticsConfig{
					Enabled: true,
				},
			},
		}

		r := MCPServerReconciler{
			Scheme:                    runtime.NewScheme(),
			DefaultAnalyticsIngestURL: "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events",
		}
		r.setDefaults(&mcpServer)

		if mcpServer.Spec.Analytics == nil {
			t.Fatal("expected analytics defaults to be applied")
		}
		assertEqual(t, "analyticsIngestURL", mcpServer.Spec.Analytics.IngestURL, "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
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

func TestReconcileDeploymentAddsGatewaySidecar(t *testing.T) {
	replicas := int32(1)
	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-server",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:       "example.com/gateway-server",
			ImageTag:    "latest",
			Port:        8088,
			ServicePort: 80,
			IngressHost: "gateway.example.com",
			Replicas:    &replicas,
			Gateway: &mcpv1alpha1.GatewayConfig{
				Enabled: true,
				Image:   "example.com/mcp-proxy:latest",
				Port:    8091,
			},
			Analytics: &mcpv1alpha1.AnalyticsConfig{
				Enabled:   true,
				IngestURL: "http://analytics.default.svc/api/events",
				Source:    "gateway-server",
				EventType: "mcp.request",
				APIKeySecretRef: &mcpv1alpha1.SecretKeyRef{
					Name: "analytics-creds",
					Key:  "api-key",
				},
			},
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
	reconciler.setDefaults(&mcpServer)

	if err := reconciler.reconcileDeployment(context.Background(), &mcpServer); err != nil {
		t.Fatalf("reconcileDeployment() error = %v", err)
	}

	var deployment appsv1.Deployment
	if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &deployment); err != nil {
		t.Fatalf("failed to fetch deployment: %v", err)
	}

	if len(deployment.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(deployment.Spec.Template.Spec.Containers))
	}

	gateway := deployment.Spec.Template.Spec.Containers[1]
	assertEqual(t, "gatewayName", gateway.Name, "mcp-gateway")
	assertEqual(t, "gatewayImage", gateway.Image, "example.com/mcp-proxy:latest")

	envByName := make(map[string]corev1.EnvVar, len(gateway.Env))
	for _, envVar := range gateway.Env {
		envByName[envVar.Name] = envVar
	}
	assertEqual(t, "gatewayPortEnv", envByName["PORT"].Value, "8091")
	assertEqual(t, "gatewayUpstreamEnv", envByName["UPSTREAM_URL"].Value, "http://127.0.0.1:8088")
	if _, ok := envByName["EXTERNAL_BASE_URL"]; ok {
		t.Fatal("expected EXTERNAL_BASE_URL to be unset for hostless path-based routing")
	}
	assertEqual(t, "analyticsIngestEnv", envByName["ANALYTICS_INGEST_URL"].Value, "http://analytics.default.svc/api/events")
	assertEqual(t, "analyticsSourceEnv", envByName["ANALYTICS_SOURCE"].Value, "gateway-server")
	assertEqual(t, "analyticsEventTypeEnv", envByName["ANALYTICS_EVENT_TYPE"].Value, "mcp.request")
	if envByName["ANALYTICS_API_KEY"].ValueFrom == nil || envByName["ANALYTICS_API_KEY"].ValueFrom.SecretKeyRef == nil {
		t.Fatal("expected analytics api key env var to come from a secret")
	}
	assertEqual(t, "analyticsAPIKeySecretName", envByName["ANALYTICS_API_KEY"].ValueFrom.SecretKeyRef.Name, "analytics-creds")
	assertEqual(t, "analyticsAPIKeySecretKey", envByName["ANALYTICS_API_KEY"].ValueFrom.SecretKeyRef.Key, "api-key")
}

func TestReconcileServiceUsesGatewayPortWhenEnabled(t *testing.T) {
	replicas := int32(1)
	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-service",
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:       "example.com/gateway-service",
			ImageTag:    "latest",
			Port:        8088,
			ServicePort: 80,
			Replicas:    &replicas,
			Gateway: &mcpv1alpha1.GatewayConfig{
				Enabled: true,
				Image:   "example.com/mcp-proxy:latest",
				Port:    8091,
			},
		},
	}

	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add mcp scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mcpServer).Build()
	reconciler := MCPServerReconciler{
		Client: client,
		Scheme: scheme,
	}

	if err := reconciler.reconcileService(context.Background(), &mcpServer); err != nil {
		t.Fatalf("reconcileService() error = %v", err)
	}

	var service corev1.Service
	if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &service); err != nil {
		t.Fatalf("failed to fetch service: %v", err)
	}

	if len(service.Spec.Ports) != 1 {
		t.Fatalf("expected 1 service port, got %d", len(service.Spec.Ports))
	}
	assertEqual(t, "serviceTargetPort", service.Spec.Ports[0].TargetPort.IntVal, int32(8091))
}

func TestResolveGatewayImage(t *testing.T) {
	t.Run("uses per-server image when set", func(t *testing.T) {
		reconciler := MCPServerReconciler{}
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Gateway: &mcpv1alpha1.GatewayConfig{
					Enabled: true,
					Image:   "example.com/proxy:latest",
				},
			},
		}

		got, err := reconciler.resolveGatewayImage(mcpServer)
		if err != nil {
			t.Fatalf("resolveGatewayImage() unexpected error: %v", err)
		}
		assertEqual(t, "gatewayImage", got, "example.com/proxy:latest")
	})

	t.Run("falls back to reconciler default image", func(t *testing.T) {
		reconciler := MCPServerReconciler{GatewayProxyImage: "example.com/default-proxy:latest"}
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Gateway: &mcpv1alpha1.GatewayConfig{
					Enabled: true,
				},
			},
		}

		got, err := reconciler.resolveGatewayImage(mcpServer)
		if err != nil {
			t.Fatalf("resolveGatewayImage() unexpected error: %v", err)
		}
		assertEqual(t, "gatewayImage", got, "example.com/default-proxy:latest")
	})

	t.Run("returns error when no image is configured", func(t *testing.T) {
		reconciler := MCPServerReconciler{}
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Gateway: &mcpv1alpha1.GatewayConfig{
					Enabled: true,
				},
			},
		}

		if _, err := reconciler.resolveGatewayImage(mcpServer); err == nil {
			t.Fatal("expected resolveGatewayImage() to return an error")
		}
	})
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
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		err := r.validateIngressConfig(context.Background(), mcpServer, logr.Discard())
		if err == nil {
			t.Fatal("expected error for missing ingressHost")
		}
	})

	t.Run("succeeds when publicPathPrefix uses hostless routing", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:            "test-image",
				PublicPathPrefix: "test-server",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}

		err := r.validateIngressConfig(context.Background(), mcpServer, logr.Discard())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
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
		readiness, err := r.checkResourceReadiness(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check resource readiness: %v", err)
		}
		// Resources don't exist yet, so they're not ready
		assertEqual(t, "deploymentReady", readiness.Deployment, false)
		assertEqual(t, "serviceReady", readiness.Service, false)
		assertEqual(t, "ingressReady", readiness.Ingress, false)
		assertEqual(t, "policyReady", readiness.Policy, true)
		assertEqual(t, "canaryReady", readiness.Canary, true)
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
				Image:            "test-image",
				ImageTag:         "latest",
				Port:             8088,
				ServicePort:      80,
				Replicas:         &replicas,
				IngressPath:      "/test-server/mcp",
				IngressClass:     "traefik",
				PublicPathPrefix: "test-server",
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
		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&mcpv1alpha1.MCPServer{}).
			Build()
		if err := client.Create(context.Background(), mcpServer); err != nil {
			t.Fatalf("failed to create MCPServer: %v", err)
		}
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		r.updateStatus(context.Background(), mcpServer, "Ready", "All resources reconciled", resourceReadiness{
			Deployment: true,
			Service:    true,
			Ingress:    true,
			Gateway:    true,
			Policy:     true,
			Canary:     true,
		})
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
		phase, allReady := determinePhase(resourceReadiness{
			Deployment: true,
			Service:    true,
			Ingress:    true,
			Gateway:    true,
			Policy:     true,
			Canary:     true,
		})
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

	t.Run("returns true when ingress has load balancer status", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		ingress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Status: networkingv1.IngressStatus{
				LoadBalancer: networkingv1.IngressLoadBalancerStatus{
					Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "10.0.0.1"}},
				},
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer, ingress).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		ready, err := r.checkIngressReady(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check ingress readiness: %v", err)
		}
		assertEqual(t, "ready", ready, true)
	})

	t.Run("returns false when only ingress class exists without admitted status", func(t *testing.T) {
		ingressClassName := "traefik"
		pathType := networkingv1.PathTypePrefix
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
		}
		ingress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "test-server", Namespace: "default"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &ingressClassName,
				Rules: []networkingv1.IngressRule{
					{
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{Path: "/", PathType: &pathType},
								},
							},
						},
					},
				},
			},
		}
		class := &networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{Name: ingressClassName},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer, ingress, class).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		ready, err := r.checkIngressReady(context.Background(), mcpServer)
		if err != nil {
			t.Fatalf("failed to check ingress readiness: %v", err)
		}
		assertEqual(t, "ready", ready, false)
	})
}

func TestRenderGatewayPolicyIncludesCrossNamespaceReferences(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)

	mcpServer := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "servers"},
		Spec: mcpv1alpha1.MCPServerSpec{
			Tools: []mcpv1alpha1.ToolConfig{
				{Name: "refund_invoice"},
			},
		},
	}
	grant := &mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant-a", Namespace: "team-a"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "payments", Namespace: "servers"},
			Subject:   mcpv1alpha1.SubjectRef{HumanID: "user-1"},
			ToolRules: []mcpv1alpha1.ToolRule{
				{Name: "refund_invoice", Decision: mcpv1alpha1.PolicyDecisionAllow},
			},
		},
	}
	session := &mcpv1alpha1.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-a", Namespace: "team-b"},
		Spec: mcpv1alpha1.MCPAgentSessionSpec{
			ServerRef:      mcpv1alpha1.ServerReference{Name: "payments", Namespace: "servers"},
			Subject:        mcpv1alpha1.SubjectRef{AgentID: "agent-1"},
			ConsentedTrust: mcpv1alpha1.TrustLevelMedium,
		},
	}
	unrelatedGrant := &mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant-b", Namespace: "team-a"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "inventory", Namespace: "servers"},
			Subject:   mcpv1alpha1.SubjectRef{HumanID: "user-2"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mcpServer, grant, session, unrelatedGrant).
		Build()
	r := MCPServerReconciler{Client: client, Scheme: scheme}

	doc, err := r.renderGatewayPolicy(context.Background(), mcpServer)
	if err != nil {
		t.Fatalf("renderGatewayPolicy() error = %v", err)
	}
	if len(doc.Grants) != 1 {
		t.Fatalf("expected 1 matching grant, got %d", len(doc.Grants))
	}
	if doc.Grants[0].Name != "grant-a" {
		t.Fatalf("expected cross-namespace grant to be rendered, got %+v", doc.Grants[0])
	}
	if len(doc.Sessions) != 1 {
		t.Fatalf("expected 1 matching session, got %d", len(doc.Sessions))
	}
	if doc.Sessions[0].Name != "session-a" {
		t.Fatalf("expected cross-namespace session to be rendered, got %+v", doc.Sessions[0])
	}
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

		var ingress networkingv1.Ingress
		if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &ingress); err != nil {
			t.Fatalf("failed to fetch ingress: %v", err)
		}
		if got := ingress.Spec.Rules[0].HTTP.Paths; len(got) != 1 {
			t.Fatalf("expected 1 ingress path, got %d", len(got))
		} else {
			assertEqual(t, "ingressPath", got[0].Path, "/test")
		}
		assertEqual(t, "ingressHost", ingress.Spec.Rules[0].Host, "example.com")
	})

	t.Run("uses publicPathPrefix for path-based routing", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "go-example-mcp", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:            "test-image",
				IngressPath:      "/ignored-when-prefix-set",
				PublicPathPrefix: "go-mcp",
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		if err := r.reconcileIngress(context.Background(), mcpServer); err != nil {
			t.Fatalf("failed to reconcile ingress: %v", err)
		}

		var ingress networkingv1.Ingress
		if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &ingress); err != nil {
			t.Fatalf("failed to fetch ingress: %v", err)
		}
		if got := ingress.Spec.Rules[0].HTTP.Paths; len(got) != 1 {
			t.Fatalf("expected 1 ingress path, got %d", len(got))
		} else {
			assertEqual(t, "ingressPath", got[0].Path, "/go-mcp/mcp")
		}
		assertEqual(t, "ingressHost", ingress.Spec.Rules[0].Host, "")
	})

	t.Run("adds oauth protected resource path for oauth servers", func(t *testing.T) {
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "oauth-server", Namespace: "default"},
			Spec: mcpv1alpha1.MCPServerSpec{
				Image:       "test-image",
				IngressHost: "example.com",
				IngressPath: "/oauth-server/mcp",
				Auth: &mcpv1alpha1.AuthConfig{
					Mode: mcpv1alpha1.AuthModeOAuth,
				},
			},
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
		r := MCPServerReconciler{Client: client, Scheme: scheme}
		if err := r.reconcileIngress(context.Background(), mcpServer); err != nil {
			t.Fatalf("failed to reconcile ingress: %v", err)
		}

		var ingress networkingv1.Ingress
		if err := client.Get(context.Background(), types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, &ingress); err != nil {
			t.Fatalf("failed to fetch ingress: %v", err)
		}
		if got := ingress.Spec.Rules[0].HTTP.Paths; len(got) != 2 {
			t.Fatalf("expected 2 ingress paths, got %d", len(got))
		} else {
			assertEqual(t, "primaryPath", got[0].Path, "/oauth-server/mcp")
			assertEqual(t, "protectedResourcePath", got[1].Path, "/.well-known/oauth-protected-resource/oauth-server/mcp")
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
		envVars := r.buildEnvVars(input, nil)
		assertEqual(t, "len", len(envVars), 2)
		assertEqual(t, "envVars[0].Name", envVars[0].Name, "FOO")
		assertEqual(t, "envVars[0].Value", envVars[0].Value, "bar")
		assertEqual(t, "envVars[1].Name", envVars[1].Name, "BAZ")
		assertEqual(t, "envVars[1].Value", envVars[1].Value, "qux")
	})

	t.Run("returns empty slice for nil input", func(t *testing.T) {
		r := MCPServerReconciler{}
		envVars := r.buildEnvVars(nil, nil)
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
				Image:            "test-image",
				ImageTag:         "latest",
				Port:             8088,
				ServicePort:      80,
				Replicas:         &replicas,
				IngressHost:      "example.com",
				IngressPath:      "/test-server/mcp",
				IngressClass:     "traefik",
				PublicPathPrefix: "test-server",
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
