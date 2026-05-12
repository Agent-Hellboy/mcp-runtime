package kubeworkload

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultServiceAccountName is the service account used by managed MCP workloads.
	DefaultServiceAccountName = "mcp-workload"
	// RestrictedRunAsUser is the non-root user used by managed workload pods.
	RestrictedRunAsUser = int64(65532)
)

// ServiceAccount returns the restricted workload ServiceAccount object.
func ServiceAccount(namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultServiceAccountName,
			Namespace: namespace,
		},
		AutomountServiceAccountToken: boolPtr(false),
	}
}

// EnsureServiceAccount creates or updates the restricted workload ServiceAccount.
func EnsureServiceAccount(ctx context.Context, client kubernetes.Interface, namespace string) error {
	desired := ServiceAccount(namespace)
	existing, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, DefaultServiceAccountName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := client.CoreV1().ServiceAccounts(namespace).Create(ctx, desired, metav1.CreateOptions{})
		return createErr
	}
	if err != nil {
		return err
	}
	existing.AutomountServiceAccountToken = boolPtr(false)
	_, err = client.CoreV1().ServiceAccounts(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// ApplyRestrictedPodDefaults applies the shared restricted pod defaults.
func ApplyRestrictedPodDefaults(spec *corev1.PodSpec) {
	if spec == nil {
		return
	}
	spec.ServiceAccountName = DefaultServiceAccountName
	spec.AutomountServiceAccountToken = boolPtr(false)
	spec.SecurityContext = RestrictedPodSecurityContext()
}

// RestrictedPodSecurityContext returns the shared pod-level security context.
func RestrictedPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
		RunAsUser:    int64Ptr(RestrictedRunAsUser),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// RestrictedContainerSecurityContext returns the shared container-level security context.
func RestrictedContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		RunAsNonRoot:             boolPtr(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// RestrictedReadOnlyContainerSecurityContext returns the shared read-only security context.
func RestrictedReadOnlyContainerSecurityContext() *corev1.SecurityContext {
	ctx := RestrictedContainerSecurityContext()
	ctx.ReadOnlyRootFilesystem = boolPtr(true)
	return ctx
}

func boolPtr(value bool) *bool {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
