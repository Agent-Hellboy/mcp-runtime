package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"mcp-runtime/pkg/k8sclient"
)

func TestClientForPrincipalRequiresIdentityForUserRole(t *testing.T) {
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	_, err := server.clientForPrincipal(principal{
		Role:      roleUser,
		IsService: true,
	})
	if err == nil {
		t.Fatal("expected identity-required error")
	}
	if err != errPrincipalIdentityRequired {
		t.Fatalf("error = %v, want %v", err, errPrincipalIdentityRequired)
	}
}

func TestClientForPrincipalRejectsServiceAdminWithoutIdentity(t *testing.T) {
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	_, err := server.clientForPrincipal(principal{
		Role:      roleAdmin,
		IsService: true,
	})
	if err == nil {
		t.Fatal("expected identity-required error")
	}
	if err != errPrincipalIdentityRequired {
		t.Fatalf("error = %v, want %v", err, errPrincipalIdentityRequired)
	}
}

func TestEnsureDefaultDenyNetworkPolicyIncludesDNSEgress(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	if err := ensureDefaultDenyNetworkPolicy(context.Background(), client, "user-1"); err != nil {
		t.Fatalf("ensureDefaultDenyNetworkPolicy() error = %v", err)
	}
	policy, err := client.NetworkingV1().NetworkPolicies("user-1").Get(context.Background(), "platform-default-deny", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get networkpolicy: %v", err)
	}
	if len(policy.Spec.Egress) == 0 {
		t.Fatalf("egress rules missing: %#v", policy.Spec)
	}
	foundDNS := false
	for _, rule := range policy.Spec.Egress {
		for _, peer := range rule.To {
			if peer.NamespaceSelector == nil || peer.PodSelector == nil {
				continue
			}
			if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "kube-system" {
				continue
			}
			if peer.PodSelector.MatchLabels["k8s-app"] != "kube-dns" {
				continue
			}
			seen53 := map[int32]bool{}
			for _, port := range rule.Ports {
				if port.Port == nil {
					continue
				}
				if port.Port.Type == intstr.Int && port.Port.IntVal == 53 {
					seen53[53] = true
				}
			}
			if seen53[53] {
				foundDNS = true
			}
		}
	}
	if !foundDNS {
		t.Fatalf("expected kube-dns egress rule, got %#v", policy.Spec.Egress)
	}
}

func TestHandleDeploymentApplyAdminUsesRequestedNamespace(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/deployments", bytes.NewReader([]byte(`{
		"name": "demo-workload",
		"image": "registry.mcpruntime.org/mcp-servers/demo:latest",
		"namespace": "tenant-a",
		"replicas": 1,
		"port": 8088
	}`)))
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{
		Role:      roleAdmin,
		Subject:   "admin-1",
		Namespace: "admin-ns",
	}))
	recorder := httptest.NewRecorder()
	server.handleDeploymentApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := client.CoreV1().Namespaces().Get(context.Background(), "tenant-a", metav1.GetOptions{}); err != nil {
		t.Fatalf("target namespace not ensured: %v", err)
	}
}

func TestEnsureDefaultDenyNetworkPolicyIdempotent(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-default-deny", Namespace: "user-2"},
	})
	if err := ensureDefaultDenyNetworkPolicy(context.Background(), client, "user-2"); err != nil {
		t.Fatalf("ensureDefaultDenyNetworkPolicy() with existing policy returned %v", err)
	}
}

func TestEnsureUserNamespaceSetsManagedLabel(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	if err := server.ensureUserNamespace(context.Background(), principal{
		Role:      roleUser,
		Subject:   "user-77",
		Namespace: "user-77",
	}); err != nil {
		t.Fatalf("ensureUserNamespace() error = %v", err)
	}
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), "user-77", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	if ns.Labels[platformUserIDLabel] != "user-77" {
		t.Fatalf("platform user label = %q, want user-77", ns.Labels[platformUserIDLabel])
	}
	if ns.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Fatalf("pod-security label = %q, want restricted", ns.Labels["pod-security.kubernetes.io/enforce"])
	}
	// Quota and limit range should exist for the namespace.
	if _, err := client.CoreV1().ResourceQuotas("user-77").Get(context.Background(), "platform-default-quota", metav1.GetOptions{}); err != nil {
		t.Fatalf("quota missing: %v", err)
	}
	if _, err := client.CoreV1().LimitRanges("user-77").Get(context.Background(), "platform-default-limits", metav1.GetOptions{}); err != nil {
		t.Fatalf("limit range missing: %v", err)
	}
}

func TestHandleDeploymentItemRejectsServiceUserWithoutIdentity(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}},
	)
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/deployments/team-a/demo", nil)
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{
		Role:      roleUser,
		IsService: true,
		Namespace: "team-a",
	}))
	recorder := httptest.NewRecorder()
	server.handleDeploymentItem(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
