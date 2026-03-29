package access

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetServerPolicyRejectsConfigMapWithoutPolicyDocuments(t *testing.T) {
	t.Parallel()

	clientset := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-policy-example",
			Namespace: "mcp-servers",
		},
		Data: map[string]string{
			"notes": "no rendered policy here",
		},
	})

	manager := NewManager(nil, clientset)

	_, err := manager.GetServerPolicy(context.Background(), "mcp-servers", "example")
	if err == nil {
		t.Fatal("GetServerPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "policy.yaml or policy.json") {
		t.Fatalf("GetServerPolicy() error = %v, want missing policy document error", err)
	}
}
