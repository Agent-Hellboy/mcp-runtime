package operator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
