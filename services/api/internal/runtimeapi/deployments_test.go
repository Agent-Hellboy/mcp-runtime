package runtimeapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"mcp-runtime/pkg/k8sclient"
)

type fakeAuditWriter struct {
	events []auditEvent
}

func (f *fakeAuditWriter) WriteAudit(_ context.Context, ev auditEvent) {
	f.events = append(f.events, ev)
}

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

func TestClientForPrincipalUsesKubernetesImpersonation(t *testing.T) {
	var gotUser string
	var gotGroups []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("Impersonate-User")
		gotGroups = append([]string(nil), r.Header.Values("Impersonate-Group")...)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","metadata":{"resourceVersion":"1"},"items":[]}`))
	}))
	t.Cleanup(api.Close)

	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Clientset: kubernetesfake.NewSimpleClientset(),
			Config:    &rest.Config{Host: api.URL},
		},
	}
	client, err := server.clientForPrincipal(principal{
		Role:    roleUser,
		Subject: "user-123",
	})
	if err != nil {
		t.Fatalf("clientForPrincipal() error = %v", err)
	}
	if _, err := client.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("list pods with impersonated client: %v", err)
	}
	if gotUser != "platform:user:user-123" {
		t.Fatalf("Impersonate-User = %q, want %q", gotUser, "platform:user:user-123")
	}
	if !hasString(gotGroups, "platform:role:user") {
		t.Fatalf("Impersonate-Group values = %v, want platform:role:user", gotGroups)
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
	request = request.WithContext(withPrincipal(request.Context(), principal{
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

func TestHandleDeploymentApplyRejectsInvalidVersionTag(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/deployments", bytes.NewReader([]byte(`{
		"name": "demo-workload",
		"image": "registry.mcpruntime.org/mcp-servers/demo",
		"version": "latest@sha256:abc",
		"namespace": "tenant-a",
		"replicas": 1,
		"port": 8088
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleAdmin,
		Subject:   "admin-1",
		Namespace: "admin-ns",
	}))
	recorder := httptest.NewRecorder()

	server.handleDeploymentApply(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "version must be a valid image tag") {
		t.Fatalf("body = %q, want invalid version message", recorder.Body.String())
	}
}

func TestHandleDeploymentApplyWritesAuditEvent(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	audit := &fakeAuditWriter{}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
		audit:      audit,
	}
	request := httptest.NewRequest(http.MethodPost, "/api/deployments", bytes.NewReader([]byte(`{
		"name": "demo-workload",
		"image": "registry.mcpruntime.org/team-a/demo:latest",
		"namespace": "team-a",
		"replicas": 1,
		"port": 8088
	}`)))
	request.Header.Set("x-mcp-source", "ui")
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleAdmin,
		Subject:   "admin-1",
		Email:     "admin@example.com",
		Namespace: "admin-ns",
		AuthType:  "platform_jwt",
	}))
	recorder := httptest.NewRecorder()
	server.handleDeploymentApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(audit.events) == 0 {
		t.Fatal("expected audit event")
	}
	got := audit.events[len(audit.events)-1]
	if got.Action != "deployment_apply" || got.Status != "success" {
		t.Fatalf("audit event = %#v, want successful deployment_apply", got)
	}
	if got.UserID != "admin-1" || got.ImageRef != "registry.mcpruntime.org/team-a/demo:latest" || got.DeploymentTarget != "team-a/demo-workload" {
		t.Fatalf("audit event metadata = %#v", got)
	}
	if got.Source != "ui:platform_jwt" || got.AuthIdentity != "platform_jwt:admin@example.com" {
		t.Fatalf("audit identity = %#v", got)
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

func TestEnsureTeamNamespaceConfiguresTraefikIngressWatch(t *testing.T) {
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "required")
	client := kubernetesfake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "traefik", Namespace: "traefik"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: "traefik",
							Args: []string{
								"--providers.kubernetesingress=true",
								"--providers.kubernetesingress.namespaces=registry,mcp-sentinel,mcp-servers",
							},
						}},
					},
				},
			},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-default-deny", Namespace: "mcp-team-acme"},
		},
	)
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	if err := server.ensureTeamNamespace(context.Background(), teamRecord{
		ID:        "team-acme-id",
		Slug:      "acme",
		Name:      "Acme",
		Namespace: "mcp-team-acme",
	}); err != nil {
		t.Fatalf("ensureTeamNamespace() error = %v", err)
	}
	role, err := client.RbacV1().Roles("mcp-team-acme").Get(context.Background(), traefikWatchRoleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("traefik watch role missing: %v", err)
	}
	if roleAllows(role, "", "secrets", "get") {
		t.Fatalf("API-created traefik watch role should not grant secret access: %#v", role.Rules)
	}
	binding, err := client.RbacV1().RoleBindings("mcp-team-acme").Get(context.Background(), traefikWatchRoleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("traefik watch rolebinding missing: %v", err)
	}
	if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != rbacv1.ServiceAccountKind || binding.Subjects[0].Namespace != "traefik" || binding.Subjects[0].Name != "traefik" {
		t.Fatalf("traefik watch binding subjects = %#v", binding.Subjects)
	}
	policy, err := client.NetworkingV1().NetworkPolicies("mcp-team-acme").Get(context.Background(), "platform-default-deny", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("network policy missing: %v", err)
	}
	if !networkPolicyAllowsNamespace(policy, "traefik") {
		t.Fatalf("network policy does not allow ingress from traefik: %#v", policy.Spec.Ingress)
	}
	deployment, err := client.AppsV1().Deployments("traefik").Get(context.Background(), "traefik", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("traefik deployment missing: %v", err)
	}
	args := strings.Join(deployment.Spec.Template.Spec.Containers[0].Args, "\n")
	if !strings.Contains(args, "--providers.kubernetesingress.namespaces=registry,mcp-sentinel,mcp-servers,mcp-team-acme") {
		t.Fatalf("traefik namespace args = %q", args)
	}
}

func TestEnsureTraefikWatchRBACPreservesExistingSecretRole(t *testing.T) {
	cfg := teamTraefikWatchConfig{
		namespace:      "traefik",
		serviceAccount: "traefik",
	}
	existingRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: traefikWatchRoleName, Namespace: "mcp-servers-public"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services", "endpoints", "secrets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	existingBinding := desiredTraefikWatchRoleBinding("mcp-servers-public", cfg)
	client := kubernetesfake.NewSimpleClientset(existingRole, existingBinding)

	if err := ensureTraefikWatchRBAC(context.Background(), client, "mcp-servers-public", cfg); err != nil {
		t.Fatalf("ensureTraefikWatchRBAC() error = %v", err)
	}
	role, err := client.RbacV1().Roles("mcp-servers-public").Get(context.Background(), traefikWatchRoleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("traefik watch role missing: %v", err)
	}
	if !roleAllows(role, "", "secrets", "get") {
		t.Fatalf("existing admin-created secret access was not preserved: %#v", role.Rules)
	}
}

func TestEnsureUserNamespaceSetsManagedLabel(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	if err := server.EnsureUserNamespace(context.Background(), principal{
		Role:      roleUser,
		Subject:   "user-77",
		Namespace: "user-77",
	}); err != nil {
		t.Fatalf("EnsureUserNamespace() error = %v", err)
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
	role, err := client.RbacV1().Roles("user-77").Get(context.Background(), platformNamespaceOwnerRoleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace owner role missing: %v", err)
	}
	if !roleAllows(role, "apps", "deployments", "create") || !roleAllows(role, "", "services", "create") {
		t.Fatalf("namespace owner role rules = %#v", role.Rules)
	}
	binding, err := client.RbacV1().RoleBindings("user-77").Get(context.Background(), platformNamespaceOwnerRoleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace owner rolebinding missing: %v", err)
	}
	if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != rbacv1.UserKind || binding.Subjects[0].Name != "platform:user:user-77" {
		t.Fatalf("namespace owner rolebinding subjects = %#v", binding.Subjects)
	}
}

func TestEnsureUserNamespaceMergesExistingNamespaceLabels(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user-88",
			Labels: map[string]string{
				"existing": "keep",
			},
		},
	})
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	if err := server.EnsureUserNamespace(context.Background(), principal{
		Role:      roleUser,
		Subject:   "user-88",
		Namespace: "user-88",
	}); err != nil {
		t.Fatalf("EnsureUserNamespace() error = %v", err)
	}
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), "user-88", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	if ns.Labels["existing"] != "keep" {
		t.Fatalf("existing label was not preserved: %#v", ns.Labels)
	}
	if ns.Labels[platformManagedLabel] != "true" || ns.Labels[platformUserIDLabel] != "user-88" {
		t.Fatalf("managed labels missing: %#v", ns.Labels)
	}
}

func TestDesiredDeploymentUsesRestrictedPodDefaults(t *testing.T) {
	deployment := desiredDeployment("demo", "user-77", "registry.example.com/demo:latest", 8088, 1, map[string]string{
		"app.kubernetes.io/name": "demo",
	})
	podSpec := deployment.Spec.Template.Spec
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Fatal("expected deployed user workloads to disable service account token automount")
	}
	if podSpec.SecurityContext == nil || podSpec.SecurityContext.SeccompProfile == nil {
		t.Fatal("expected pod security context with seccomp profile")
	}
	if podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("seccomp profile = %q, want %q", podSpec.SecurityContext.SeccompProfile.Type, corev1.SeccompProfileTypeRuntimeDefault)
	}
	if podSpec.SecurityContext.RunAsUser == nil || *podSpec.SecurityContext.RunAsUser != restrictedRunAsUser {
		t.Fatalf("runAsUser = %v, want %d", podSpec.SecurityContext.RunAsUser, restrictedRunAsUser)
	}
	container := podSpec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("expected user workload container to disallow privilege escalation")
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != corev1.Capability("ALL") {
		t.Fatalf("expected user workload container to drop all capabilities, got %#v", container.SecurityContext.Capabilities)
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
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		IsService: true,
		Namespace: "team-a",
	}))
	recorder := httptest.NewRecorder()
	server.HandleDeploymentItem(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func roleAllows(role *rbacv1.Role, apiGroup, resource, verb string) bool {
	for _, rule := range role.Rules {
		if !hasString(rule.APIGroups, apiGroup) || !hasString(rule.Resources, resource) || !hasString(rule.Verbs, verb) {
			continue
		}
		return true
	}
	return false
}

func networkPolicyAllowsNamespace(policy *networkingv1.NetworkPolicy, namespace string) bool {
	for _, rule := range policy.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.NamespaceSelector == nil {
				continue
			}
			if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == namespace {
				return true
			}
		}
	}
	return false
}
