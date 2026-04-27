package main

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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	platformManagedLabel = "platform.mcpruntime.org/managed"
	platformUserIDLabel  = "platform.mcpruntime.org/user-id"
	createdByLabel       = "created-by"
	defaultDeployPort    = int32(8088)
)

var errPrincipalIdentityRequired = errors.New("authenticated user identity required")

type deployRequest struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Version   string `json:"version"`
	Port      int32  `json:"port"`
	Replicas  int32  `json:"replicas"`
	Namespace string `json:"namespace,omitempty"`
}

func (s *RuntimeServer) handleDeployments(w http.ResponseWriter, r *http.Request) {
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

func (s *RuntimeServer) handleDeploymentItem(w http.ResponseWriter, r *http.Request) {
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
	if p.Role != roleAdmin && ns != p.Namespace {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := client.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete deployment"})
		return
	}
	if err := client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete service"})
		return
	}
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
	namespace := p.Namespace
	if p.Role == roleAdmin {
		namespace = strings.TrimSpace(r.URL.Query().Get("namespace"))
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

func (s *RuntimeServer) handleAdminDeployments(w http.ResponseWriter, r *http.Request) {
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
	listNamespace := metav1.NamespaceAll
	if namespace != "" {
		listNamespace = namespace
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := s.k8sClients.Clientset.AppsV1().Deployments(listNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": deploymentSummaries(list.Items)})
}

func (s *RuntimeServer) handleDeploymentApply(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req deployRequest
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and image are required"})
		return
	}
	namespace := p.Namespace
	if p.Role == roleAdmin && strings.TrimSpace(req.Namespace) != "" {
		namespace = strings.TrimSpace(req.Namespace)
	}
	if namespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace required"})
		return
	}
	if p.Role != roleAdmin && namespace != p.Namespace {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	image := req.Image
	if req.Version != "" && !strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		image += ":" + req.Version
	}
	if err := validateDeployImage(image, namespace, p.Role); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	target := p
	target.Namespace = namespace
	if err := s.ensureUserNamespace(ctx, target); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to ensure namespace"})
		return
	}
	labels := map[string]string{
		"app.kubernetes.io/name":       req.Name,
		"app.kubernetes.io/managed-by": "mcp-runtime",
		platformManagedLabel:           "true",
		platformUserIDLabel:            p.userID(),
		createdByLabel:                 p.userID(),
	}
	dep := desiredDeployment(req.Name, namespace, image, req.Port, req.Replicas, labels)
	applied, err := upsertDeployment(ctx, client, dep)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to apply deployment"})
		return
	}
	svc := desiredService(req.Name, namespace, req.Port, labels)
	if _, err := upsertService(ctx, client, svc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to apply service"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployment": deploymentSummary(*applied)})
}

func (s *RuntimeServer) clientForPrincipal(p principal) (kubernetes.Interface, error) {
	if s.k8sClients == nil {
		return nil, fmt.Errorf("kubernetes not available")
	}
	if p.userID() == "" {
		return nil, errPrincipalIdentityRequired
	}
	if s.k8sClients.Config == nil {
		return s.k8sClients.Clientset, nil
	}
	cfg := rest.CopyConfig(s.k8sClients.Config)
	cfg.Impersonate = rest.ImpersonationConfig{
		UserName: "platform:user:" + p.userID(),
		Groups:   []string{"platform:role:" + p.Role},
	}
	return kubernetes.NewForConfig(cfg)
}

func (s *RuntimeServer) ensureUserNamespace(ctx context.Context, p principal) error {
	if s.k8sClients == nil || p.Namespace == "" {
		return nil
	}
	base := s.k8sClients.Clientset
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: p.Namespace,
		Labels: map[string]string{
			platformManagedLabel:                 "true",
			platformUserIDLabel:                  p.userID(),
			"pod-security.kubernetes.io/enforce": "restricted",
		},
	}}
	if _, err := base.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := ensureResourceQuota(ctx, base, p.Namespace); err != nil {
		return err
	}
	if err := ensureLimitRange(ctx, base, p.Namespace); err != nil {
		return err
	}
	return ensureDefaultDenyNetworkPolicy(ctx, base, p.Namespace)
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

func ensureDefaultDenyNetworkPolicy(ctx context.Context, client kubernetes.Interface, ns string) error {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-default-deny", Namespace: ns},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
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
	if _, err := client.NetworkingV1().NetworkPolicies(ns).Create(ctx, policy, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func desiredDeployment(name, namespace, image string, port, replicas int32, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": name}},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name:  "server",
				Image: image,
				Ports: []corev1.ContainerPort{{ContainerPort: port}},
			}}},
		},
	}}
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

func validateDeployImage(image, namespace, role string) error {
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
	if role != roleAdmin && len(parts) >= 3 && parts[1] != namespace {
		return fmt.Errorf("image repository must be scoped to namespace %q", namespace)
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
	return map[string]any{"name": d.Name, "namespace": d.Namespace, "replicas": replicas, "ready": d.Status.ReadyReplicas, "labels": d.Labels, "created_at": d.CreationTimestamp.Time}
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
