package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"mcp-runtime/pkg/kubeworkload"
)

const (
	platformManagedLabel  = "platform.mcpruntime.org/managed"
	platformUserIDLabel   = "platform.mcpruntime.org/user-id"
	platformTeamIDLabel   = "mcpruntime.org/team-id"
	platformTeamSlugLabel = "mcpruntime.org/team-slug"
	platformScopeLabel    = "mcpruntime.org/scope"
	createdByLabel        = "created-by"
	defaultDeployPort     = int32(8088)
	restrictedRunAsUser   = kubeworkload.RestrictedRunAsUser
	traefikWatchRoleName  = "traefik-watch"
)

var errPrincipalIdentityRequired = errors.New("authenticated user identity required")

type managedNamespaceOptions struct {
	ingressFromNamespaces []string
}

type teamTraefikWatchConfig struct {
	mode           string
	namespace      string
	deployment     string
	serviceAccount string
}

type deployRequest struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Version   string `json:"version"`
	Port      int32  `json:"port"`
	Replicas  int32  `json:"replicas"`
	Namespace string `json:"namespace,omitempty"`
}

func (s *RuntimeServer) HandleDeployments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleDeploymentList(w, r)
	case http.MethodPost:
		s.handleDeploymentApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) HandleDeploymentItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("allow", "DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ns, name, err := extractNamespaceName(r.URL.Path, "/api/deployments/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if p.Role != roleAdmin && (!p.HasNamespace(ns) || ns == sharedCatalogNamespace) {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_delete", "denied", name, ns, "", "forbidden"))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	client, err := s.clientForPrincipal(p)
	if err != nil {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_delete", "error", name, ns, "", err.Error()))
		if errors.Is(err, errPrincipalIdentityRequired) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "authenticated user identity required"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := client.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_delete", "error", name, ns, "", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete deployment"})
		return
	}
	if err := client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_delete", "error", name, ns, "", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete service"})
		return
	}
	s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_delete", "success", name, ns, "", ""))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "namespace": ns, "name": name})
}

func (s *RuntimeServer) handleDeploymentList(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.k8sClients == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if p.Role != roleAdmin {
		if namespace == "" {
			namespace = strings.TrimSpace(p.Namespace)
		}
		if namespace == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if !p.HasNamespace(namespace) || namespace == sharedCatalogNamespace {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	}
	client, err := s.clientForPrincipal(p)
	if err != nil {
		if errors.Is(err, errPrincipalIdentityRequired) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "authenticated user identity required"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: platformManagedLabel + "=true"})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": deploymentSummaries(list.Items)})
}

func (s *RuntimeServer) HandleAdminDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok || p.Role != roleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if s.k8sClients == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	summaries, err := s.ListAdminDeploymentSummaries(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": summaries})
}

func (s *RuntimeServer) ListAdminDeploymentSummaries(ctx context.Context, namespace string) ([]map[string]any, error) {
	if s.k8sClients == nil {
		return nil, errors.New("kubernetes not available")
	}
	listNamespace := metav1.NamespaceAll
	if namespace != "" {
		listNamespace = namespace
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := s.k8sClients.Clientset.AppsV1().Deployments(listNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return deploymentSummaries(list.Items), nil
}

func (s *RuntimeServer) handleDeploymentApply(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req deployRequest
	r.Body = http.MaxBytesReader(w, r.Body, deploymentApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Image = strings.TrimSpace(req.Image)
	req.Version = strings.TrimSpace(req.Version)
	if req.Port == 0 {
		req.Port = defaultDeployPort
	}
	if req.Replicas == 0 {
		req.Replicas = 1
	}
	if req.Name == "" || req.Image == "" {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "denied", req.Name, firstNonEmpty(strings.TrimSpace(req.Namespace), p.Namespace), req.Image, "name and image are required"))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and image are required"})
		return
	}
	namespace := p.Namespace
	if p.Role == roleAdmin && strings.TrimSpace(req.Namespace) != "" {
		namespace = strings.TrimSpace(req.Namespace)
	}
	if p.Role != roleAdmin && strings.TrimSpace(req.Namespace) != "" {
		namespace = strings.TrimSpace(req.Namespace)
	}
	if namespace == "" {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "denied", req.Name, namespace, req.Image, "namespace required"))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace required"})
		return
	}
	if p.Role != roleAdmin && (!p.HasNamespace(namespace) || namespace == sharedCatalogNamespace) {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "denied", req.Name, namespace, req.Image, "forbidden"))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	image := req.Image
	if req.Version != "" && !strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		image += ":" + req.Version
	}
	team, teamNamespace := p.TeamForNamespace(namespace)
	teamSlug := ""
	if teamNamespace {
		teamSlug = strings.TrimSpace(team.Slug)
	}
	if err := ValidateDeployImage(image, namespace, teamSlug, p.Role); err != nil {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "denied", req.Name, namespace, image, err.Error()))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	client, err := s.clientForPrincipal(p)
	if err != nil {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "error", req.Name, namespace, image, err.Error()))
		if errors.Is(err, errPrincipalIdentityRequired) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "authenticated user identity required"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	target := p
	target.Namespace = namespace
	if teamNamespace {
		if err := s.ensureTeamNamespace(ctx, teamRecord{
			ID:        team.ID,
			Slug:      team.Slug,
			Name:      team.Name,
			Namespace: team.Namespace,
		}); err != nil {
			s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "error", req.Name, namespace, image, err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to ensure team namespace"})
			return
		}
	} else {
		if err := s.EnsureUserNamespace(ctx, target); err != nil {
			s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "error", req.Name, namespace, image, err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to ensure namespace"})
			return
		}
	}
	labels := map[string]string{
		"app.kubernetes.io/name":       req.Name,
		"app.kubernetes.io/managed-by": "mcp-runtime",
		platformManagedLabel:           "true",
		platformUserIDLabel:            p.UserID(),
		createdByLabel:                 p.UserID(),
	}
	if teamNamespace {
		labels[platformTeamIDLabel] = team.ID
		labels[platformTeamSlugLabel] = team.Slug
		labels[platformScopeLabel] = namespaceScopeTeam
	}
	dep := desiredDeployment(req.Name, namespace, image, req.Port, req.Replicas, labels)
	applied, err := upsertDeployment(ctx, client, dep)
	if err != nil {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "error", req.Name, namespace, image, err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to apply deployment"})
		return
	}
	svc := desiredService(req.Name, namespace, req.Port, labels)
	if _, err := upsertService(ctx, client, svc); err != nil {
		s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "error", req.Name, namespace, image, err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to apply service"})
		return
	}
	s.writeAudit(r.Context(), deploymentAuditEvent(r, p, "deployment_apply", "success", req.Name, namespace, image, ""))
	writeJSON(w, http.StatusOK, map[string]any{"deployment": deploymentSummary(*applied)})
}

func (s *RuntimeServer) clientForPrincipal(p principal) (kubernetes.Interface, error) {
	if s.k8sClients == nil {
		return nil, fmt.Errorf("kubernetes not available")
	}
	if p.UserID() == "" {
		return nil, errPrincipalIdentityRequired
	}
	if s.k8sClients.Config == nil {
		return s.k8sClients.Clientset, nil
	}
	cfg := rest.CopyConfig(s.k8sClients.Config)
	cfg.Impersonate = rest.ImpersonationConfig{
		UserName: "platform:user:" + p.UserID(),
		Groups:   []string{"platform:role:" + p.Role},
	}
	return kubernetes.NewForConfig(cfg)
}

func (s *RuntimeServer) EnsureUserNamespace(ctx context.Context, p principal) error {
	if s.k8sClients == nil || p.Namespace == "" {
		return nil
	}
	labels := map[string]string{
		platformManagedLabel:                 "true",
		platformUserIDLabel:                  p.UserID(),
		platformScopeLabel:                   namespaceScopeUser,
		"pod-security.kubernetes.io/enforce": "restricted",
	}
	return s.ensureManagedNamespace(ctx, p.Namespace, labels, managedNamespaceOptions{})
}

func (s *RuntimeServer) ensureTeamNamespace(ctx context.Context, team teamRecord) error {
	if strings.TrimSpace(team.Namespace) == "" {
		return errors.New("team namespace required")
	}
	labels := map[string]string{
		platformManagedLabel:                 "true",
		platformTeamIDLabel:                  strings.TrimSpace(team.ID),
		platformTeamSlugLabel:                strings.TrimSpace(team.Slug),
		platformScopeLabel:                   namespaceScopeTeam,
		"pod-security.kubernetes.io/enforce": "restricted",
	}
	cfg := platformTeamTraefikWatchConfig()
	opts := managedNamespaceOptions{}
	if cfg.mode != "disabled" {
		opts.ingressFromNamespaces = []string{cfg.namespace}
	}
	if err := s.ensureManagedNamespace(ctx, team.Namespace, labels, opts); err != nil {
		return err
	}
	if cfg.mode == "disabled" {
		return nil
	}
	return s.ensureTeamTraefikWatch(ctx, team.Namespace, cfg)
}

func (s *RuntimeServer) ensureManagedNamespace(ctx context.Context, namespace string, labels map[string]string, opts managedNamespaceOptions) error {
	if s.k8sClients == nil || strings.TrimSpace(namespace) == "" {
		return nil
	}
	base := s.k8sClients.Clientset
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   namespace,
		Labels: labels,
	}}
	if _, err := base.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := ensureResourceQuota(ctx, base, namespace); err != nil {
		return err
	}
	if err := ensureLimitRange(ctx, base, namespace); err != nil {
		return err
	}
	if err := ensureDefaultDenyNetworkPolicy(ctx, base, namespace, opts.ingressFromNamespaces...); err != nil {
		return err
	}
	return kubeworkload.EnsureServiceAccount(ctx, base, namespace)
}

func ensureResourceQuota(ctx context.Context, client kubernetes.Interface, ns string) error {
	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "platform-default-quota", Namespace: ns}, Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
		corev1.ResourcePods:                   resource.MustParse("20"),
		corev1.ResourceRequestsCPU:            resource.MustParse("4"),
		corev1.ResourceRequestsMemory:         resource.MustParse("8Gi"),
		corev1.ResourceLimitsCPU:              resource.MustParse("8"),
		corev1.ResourceLimitsMemory:           resource.MustParse("16Gi"),
		corev1.ResourcePersistentVolumeClaims: resource.MustParse("4"),
	}}}
	if _, err := client.CoreV1().ResourceQuotas(ns).Create(ctx, quota, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func ensureLimitRange(ctx context.Context, client kubernetes.Interface, ns string) error {
	limit := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: "platform-default-limits", Namespace: ns}, Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
		Type: corev1.LimitTypeContainer,
		DefaultRequest: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Default: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}}}}
	if _, err := client.CoreV1().LimitRanges(ns).Create(ctx, limit, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func ensureDefaultDenyNetworkPolicy(ctx context.Context, client kubernetes.Interface, ns string, ingressFromNamespaces ...string) error {
	policy := desiredDefaultDenyNetworkPolicy(ns, ingressFromNamespaces...)
	current, err := client.NetworkingV1().NetworkPolicies(ns).Get(ctx, policy.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.NetworkingV1().NetworkPolicies(ns).Create(ctx, policy, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	policy.ResourceVersion = current.ResourceVersion
	policy.Labels = current.Labels
	policy.Annotations = current.Annotations
	_, err = client.NetworkingV1().NetworkPolicies(ns).Update(ctx, policy, metav1.UpdateOptions{})
	return err
}

func desiredDefaultDenyNetworkPolicy(ns string, ingressFromNamespaces ...string) *networkingv1.NetworkPolicy {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	ingress := make([]networkingv1.NetworkPolicyIngressRule, 0, 2)
	if len(ingressFromNamespaces) > 0 {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{}},
			},
		})
	}
	for _, namespace := range ingressFromNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace},
					},
				},
			},
		})
	}
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-default-deny", Namespace: ns},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress:     ingress,
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"k8s-app": "kube-dns"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udpProtocol, Port: intstrPtr(53)},
						{Protocol: &tcpProtocol, Port: intstrPtr(53)},
					},
				},
				{
					To: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProtocol, Port: intstrPtr(80)},
						{Protocol: &tcpProtocol, Port: intstrPtr(443)},
					},
				},
			},
		},
	}
	return policy
}

func platformTeamTraefikWatchConfig() teamTraefikWatchConfig {
	mode := strings.ToLower(strings.TrimSpace(envOr("PLATFORM_TEAM_TRAEFIK_WATCH", "auto")))
	switch mode {
	case "", "auto":
		mode = "auto"
	case "false", "off", "0", "no", "disabled":
		mode = "disabled"
	case "true", "on", "1", "yes", "required":
		mode = "required"
	default:
		mode = "auto"
	}
	return teamTraefikWatchConfig{
		mode:           mode,
		namespace:      envOr("PLATFORM_TRAEFIK_NAMESPACE", "traefik"),
		deployment:     envOr("PLATFORM_TRAEFIK_DEPLOYMENT", "traefik"),
		serviceAccount: envOr("PLATFORM_TRAEFIK_SERVICE_ACCOUNT", "traefik"),
	}
}

func (s *RuntimeServer) ensureTeamTraefikWatch(ctx context.Context, namespace string, cfg teamTraefikWatchConfig) error {
	if s.k8sClients == nil || strings.TrimSpace(namespace) == "" {
		return nil
	}
	if err := ensureTraefikWatchRBAC(ctx, s.k8sClients.Clientset, namespace, cfg); err != nil {
		return err
	}
	if err := ensureTraefikDeploymentWatchesNamespace(ctx, s.k8sClients.Clientset, namespace, cfg); err != nil {
		if cfg.mode == "auto" && apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func ensureTraefikWatchRBAC(ctx context.Context, client kubernetes.Interface, namespace string, cfg teamTraefikWatchConfig) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: traefikWatchRoleName, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services", "endpoints", "secrets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if err := upsertRole(ctx, client, role); err != nil {
		return err
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: traefikWatchRoleName, Namespace: namespace},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     traefikWatchRoleName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: cfg.serviceAccount, Namespace: cfg.namespace},
		},
	}
	return upsertRoleBinding(ctx, client, binding)
}

func upsertRole(ctx context.Context, client kubernetes.Interface, role *rbacv1.Role) error {
	current, err := client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.RbacV1().Roles(role.Namespace).Create(ctx, role, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	role.ResourceVersion = current.ResourceVersion
	role.Labels = current.Labels
	role.Annotations = current.Annotations
	_, err = client.RbacV1().Roles(role.Namespace).Update(ctx, role, metav1.UpdateOptions{})
	return err
}

func upsertRoleBinding(ctx context.Context, client kubernetes.Interface, binding *rbacv1.RoleBinding) error {
	current, err := client.RbacV1().RoleBindings(binding.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	binding.ResourceVersion = current.ResourceVersion
	binding.Labels = current.Labels
	binding.Annotations = current.Annotations
	_, err = client.RbacV1().RoleBindings(binding.Namespace).Update(ctx, binding, metav1.UpdateOptions{})
	return err
}

func ensureTraefikDeploymentWatchesNamespace(ctx context.Context, client kubernetes.Interface, namespace string, cfg teamTraefikWatchConfig) error {
	deployment, err := client.AppsV1().Deployments(cfg.namespace).Get(ctx, cfg.deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	const prefix = "--providers.kubernetesingress.namespaces="
	containerIndex := -1
	argIndex := -1
	argValue := ""
	for ci, container := range deployment.Spec.Template.Spec.Containers {
		for ai, arg := range container.Args {
			if strings.HasPrefix(arg, prefix) {
				containerIndex = ci
				argIndex = ai
				argValue = arg
				break
			}
		}
		if containerIndex >= 0 {
			break
		}
	}
	if containerIndex < 0 {
		if cfg.mode == "auto" {
			return nil
		}
		return errors.New("traefik deployment does not expose --providers.kubernetesingress.namespaces")
	}
	watched := splitCSV(strings.TrimPrefix(argValue, prefix))
	for _, watchedNamespace := range watched {
		if watchedNamespace == namespace {
			return nil
		}
	}
	watched = append(watched, namespace)
	updated := deployment.DeepCopy()
	updated.Spec.Template.Spec.Containers[containerIndex].Args[argIndex] = prefix + strings.Join(watched, ",")
	_, err = client.AppsV1().Deployments(cfg.namespace).Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func desiredDeployment(name, namespace, image string, port, replicas int32, labels map[string]string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": name}},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:            "server",
					Image:           image,
					Ports:           []corev1.ContainerPort{{ContainerPort: port}},
					SecurityContext: kubeworkload.RestrictedContainerSecurityContext(),
				}},
			},
		},
	}}
	kubeworkload.ApplyRestrictedPodDefaults(&deployment.Spec.Template.Spec)
	return deployment
}

func desiredService(name, namespace string, port int32, labels map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: corev1.ServiceSpec{
		Selector: map[string]string{"app.kubernetes.io/name": name},
		Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstrFromInt32(port)}},
		Type:     corev1.ServiceTypeClusterIP,
	}}
}

func intstrPtr(port int) *intstr.IntOrString {
	v := intstr.FromInt(port)
	return &v
}

func upsertDeployment(ctx context.Context, client kubernetes.Interface, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	existing, err := client.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return client.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	existing.Labels = dep.Labels
	existing.Spec = dep.Spec
	return client.AppsV1().Deployments(dep.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
}

func upsertService(ctx context.Context, client kubernetes.Interface, svc *corev1.Service) (*corev1.Service, error) {
	existing, err := client.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return client.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	existing.Labels = svc.Labels
	existing.Spec.Ports = svc.Spec.Ports
	existing.Spec.Selector = svc.Spec.Selector
	return client.CoreV1().Services(svc.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
}

func ValidateDeployImage(image, namespace, teamSlug, role string) error {
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return fmt.Errorf("image must include a registry/repository path")
	}
	if approved := approvedRegistries(); len(approved) > 0 {
		host := parts[0]
		if _, ok := approved[host]; !ok {
			return fmt.Errorf("registry %q is not approved", host)
		}
	}
	if role != roleAdmin && len(parts) >= 3 {
		expected := namespace
		if strings.TrimSpace(teamSlug) != "" {
			expected = teamSlug
		}
		if parts[1] != expected {
			return fmt.Errorf("image repository must be scoped to %q", expected)
		}
	}
	return nil
}

func approvedRegistries() map[string]struct{} {
	raw := strings.TrimSpace(envOr("APPROVED_REGISTRIES", ""))
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

func deploymentSummaries(items []appsv1.Deployment) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, deploymentSummary(item))
	}
	return out
}

func deploymentSummary(d appsv1.Deployment) map[string]any {
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	return map[string]any{
		"name":       d.Name,
		"namespace":  d.Namespace,
		"replicas":   replicas,
		"ready":      d.Status.ReadyReplicas,
		"image":      firstContainerImage(d),
		"user_id":    d.Labels[platformUserIDLabel],
		"created_by": d.Labels[createdByLabel],
		"labels":     d.Labels,
		"created_at": d.CreationTimestamp.Time,
	}
}

func firstContainerImage(d appsv1.Deployment) string {
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return d.Spec.Template.Spec.Containers[0].Image
}

func deploymentAuditEvent(r *http.Request, p principal, action, status, name, namespace, image, message string) auditEvent {
	target := strings.Trim(strings.TrimSpace(namespace)+"/"+strings.TrimSpace(name), "/")
	return auditEvent{
		UserID:           p.UserID(),
		Action:           action,
		Resource:         strings.TrimSpace(name),
		Namespace:        strings.TrimSpace(namespace),
		Status:           status,
		Message:          strings.TrimSpace(message),
		ActorIP:          requestIP(r),
		Source:           auditSource(r, p),
		AuthIdentity:     auditIdentityLabel(p),
		ImageRef:         strings.TrimSpace(image),
		ServerName:       strings.TrimSpace(name),
		DeploymentTarget: target,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractNamespaceName(path, prefix string) (string, string, error) {
	trimmed := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected /namespace/name")
	}
	return parts[0], parts[1], nil
}

func intstrFromInt32(v int32) intstr.IntOrString {
	return intstr.FromInt(int(v))
}
