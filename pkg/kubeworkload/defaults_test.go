package kubeworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestApplyRestrictedPodDefaults(t *testing.T) {
	t.Parallel()

	spec := &corev1.PodSpec{}
	ApplyRestrictedPodDefaults(spec)

	if spec.ServiceAccountName != DefaultServiceAccountName {
		t.Fatalf("serviceAccountName = %q, want %q", spec.ServiceAccountName, DefaultServiceAccountName)
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatalf("automountServiceAccountToken = %v, want false", spec.AutomountServiceAccountToken)
	}
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsUser == nil || *spec.SecurityContext.RunAsUser != RestrictedRunAsUser {
		t.Fatalf("pod security context = %#v, want runAsUser=%d", spec.SecurityContext, RestrictedRunAsUser)
	}
}

func TestEnsureServiceAccount(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	if err := EnsureServiceAccount(context.Background(), client, "mcp-servers"); err != nil {
		t.Fatalf("EnsureServiceAccount() error = %v", err)
	}
	sa, err := client.CoreV1().ServiceAccounts("mcp-servers").Get(context.Background(), DefaultServiceAccountName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if sa.AutomountServiceAccountToken == nil || *sa.AutomountServiceAccountToken {
		t.Fatalf("automountServiceAccountToken = %v, want false", sa.AutomountServiceAccountToken)
	}
}
