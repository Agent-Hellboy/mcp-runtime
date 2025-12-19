package operator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

// RegistryConfig holds configuration for a provisioned container registry.
type RegistryConfig struct {
	URL        string
	Username   string
	Password   string
	SecretName string
}

// MCPServerReconciler reconciles a MCPServer object
type MCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// DefaultIngressHost is the default ingress host if not specified in the CR.
	DefaultIngressHost string

	// ProvisionedRegistry holds the provisioned registry configuration.
	// If nil or URL is empty, provisioned registry features are disabled.
	ProvisionedRegistry *RegistryConfig
}

const (
	defaultRequestCPU    = "50m"
	defaultRequestMemory = "64Mi"
	defaultLimitCPU      = "500m"
	defaultLimitMemory   = "256Mi"
)

//+kubebuilder:rbac:groups=mcp.agent-hellboy.io,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcp.agent-hellboy.io,resources=mcpservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcp.agent-hellboy.io,resources=mcpservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var mcpServer mcpv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &mcpServer); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{Requeue: false}, nil
		}
		return ctrl.Result{Requeue: false}, err
	}

	logger.Info("Reconciling MCPServer", "name", mcpServer.Name, "namespace", mcpServer.Namespace)

	// Set defaults and update spec only if changed
	original := mcpServer.DeepCopy()
	r.setDefaults(&mcpServer)
	if !reflect.DeepEqual(original.Spec, mcpServer.Spec) {
		if err := r.Update(ctx, &mcpServer); err != nil {
			logger.Error(err, "Failed to update MCPServer spec with defaults")
			return ctrl.Result{Requeue: false}, err
		}
		// Requeue to work with the updated object and avoid stale data
		return ctrl.Result{Requeue: true}, nil
	}

	if mcpServer.Spec.IngressHost == "" {
		err := fmt.Errorf("ingressHost is required; set spec.ingressHost or MCP_DEFAULT_INGRESS_HOST")
		r.updateStatus(ctx, &mcpServer, "Error", err.Error(), false, false, false)
		logger.Error(err, "Missing ingress host")
		return ctrl.Result{Requeue: false}, err
	}

	if mcpServer.Spec.IngressPath == "" {
		err := fmt.Errorf("ingressPath is required; set spec.ingressPath or ensure metadata.name is set")
		r.updateStatus(ctx, &mcpServer, "Error", err.Error(), false, false, false)
		logger.Error(err, "Missing ingress path")
		return ctrl.Result{Requeue: false}, err
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, &mcpServer); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		r.updateStatus(ctx, &mcpServer, "Error", fmt.Sprintf("Failed to reconcile Deployment: %v", err), false, false, false)
		return ctrl.Result{Requeue: false}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, &mcpServer); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		r.updateStatus(ctx, &mcpServer, "Error", fmt.Sprintf("Failed to reconcile Service: %v", err), false, false, false)
		return ctrl.Result{Requeue: false}, err
	}

	// Reconcile Ingress
	if err := r.reconcileIngress(ctx, &mcpServer); err != nil {
		logger.Error(err, "Failed to reconcile Ingress")
		r.updateStatus(ctx, &mcpServer, "Error", fmt.Sprintf("Failed to reconcile Ingress: %v", err), false, false, false)
		return ctrl.Result{Requeue: false}, err
	}

	// Check deployment status
	deploymentReady, err := r.checkDeploymentReady(ctx, &mcpServer)
	if err != nil {
		return ctrl.Result{}, err
	}

	serviceReady, err := r.checkServiceReady(ctx, &mcpServer)
	if err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	ingressReady, err := r.checkIngressReady(ctx, &mcpServer)
	if err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	phase := "Pending"
	allReady := deploymentReady && serviceReady && ingressReady
	if allReady {
		phase = "Ready"
	} else if deploymentReady || serviceReady {
		phase = "PartiallyReady"
	}

	r.updateStatus(ctx, &mcpServer, phase, "All resources reconciled", deploymentReady, serviceReady, ingressReady)

	logger.Info("Successfully reconciled MCPServer", "name", mcpServer.Name, "phase", phase)

	// If not all resources are ready, requeue with a short delay to check again
	if !allReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *MCPServerReconciler) setDefaults(mcpServer *mcpv1alpha1.MCPServer) {
	// Only set a default tag if the image doesn't already contain one.
	if mcpServer.Spec.ImageTag == "" && !strings.Contains(mcpServer.Spec.Image, ":") && !strings.Contains(mcpServer.Spec.Image, "@") {
		mcpServer.Spec.ImageTag = "latest"
	}
	if mcpServer.Spec.Replicas == nil {
		replicas := int32(1)
		mcpServer.Spec.Replicas = &replicas
	}
	if mcpServer.Spec.Port == 0 {
		mcpServer.Spec.Port = 8088
	}
	if mcpServer.Spec.ServicePort == 0 {
		mcpServer.Spec.ServicePort = 80
	}
	if mcpServer.Spec.IngressPath == "" && mcpServer.Name != "" {
		mcpServer.Spec.IngressPath = "/" + mcpServer.Name + "/mcp"
	}
	if mcpServer.Spec.IngressHost == "" && r.DefaultIngressHost != "" {
		mcpServer.Spec.IngressHost = r.DefaultIngressHost
	}
	if mcpServer.Spec.IngressClass == "" {
		mcpServer.Spec.IngressClass = "traefik"
	}
}

func (r *MCPServerReconciler) reconcileDeployment(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)

	image, err := r.resolveImage(ctx, mcpServer)
	if err != nil {
		return err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		selectorLabels := map[string]string{
			"app": mcpServer.Name,
		}
		templateLabels := map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
		}

		deployment.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
		}

		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: mcpServer.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: templateLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: r.buildImagePullSecrets(ctx, mcpServer),
					Containers:       []corev1.Container{},
				},
			},
		}

		container := corev1.Container{
			Name:            mcpServer.Name,
			Image:           image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: mcpServer.Spec.Port,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			Env: r.buildEnvVars(mcpServer.Spec.EnvVars),
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(mcpServer.Spec.Port)},
				},
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(mcpServer.Spec.Port)},
				},
				InitialDelaySeconds: 3,
				PeriodSeconds:       5,
			},
		}

		if mcpServer.Spec.Resources.Limits != nil && (mcpServer.Spec.Resources.Limits.CPU != "" || mcpServer.Spec.Resources.Limits.Memory != "") {
			container.Resources.Limits = corev1.ResourceList{}
			if mcpServer.Spec.Resources.Limits.CPU != "" {
				cpuLimit, err := resource.ParseQuantity(mcpServer.Spec.Resources.Limits.CPU)
				if err != nil {
					return fmt.Errorf("invalid CPU limit %q: %w", mcpServer.Spec.Resources.Limits.CPU, err)
				}
				container.Resources.Limits[corev1.ResourceCPU] = cpuLimit
			}
			if mcpServer.Spec.Resources.Limits.Memory != "" {
				memLimit, err := resource.ParseQuantity(mcpServer.Spec.Resources.Limits.Memory)
				if err != nil {
					return fmt.Errorf("invalid memory limit %q: %w", mcpServer.Spec.Resources.Limits.Memory, err)
				}
				container.Resources.Limits[corev1.ResourceMemory] = memLimit
			}
		}

		if mcpServer.Spec.Resources.Requests != nil && (mcpServer.Spec.Resources.Requests.CPU != "" || mcpServer.Spec.Resources.Requests.Memory != "") {
			container.Resources.Requests = corev1.ResourceList{}
			if mcpServer.Spec.Resources.Requests.CPU != "" {
				cpuReq, err := resource.ParseQuantity(mcpServer.Spec.Resources.Requests.CPU)
				if err != nil {
					return fmt.Errorf("invalid CPU request %q: %w", mcpServer.Spec.Resources.Requests.CPU, err)
				}
				container.Resources.Requests[corev1.ResourceCPU] = cpuReq
			}
			if mcpServer.Spec.Resources.Requests.Memory != "" {
				memReq, err := resource.ParseQuantity(mcpServer.Spec.Resources.Requests.Memory)
				if err != nil {
					return fmt.Errorf("invalid memory request %q: %w", mcpServer.Spec.Resources.Requests.Memory, err)
				}
				container.Resources.Requests[corev1.ResourceMemory] = memReq
			}
		}

		applyContainerResourceDefaults(&container)

		deployment.Spec.Template.Spec.Containers = []corev1.Container{container}

		if err := ctrl.SetControllerReference(mcpServer, deployment, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		logger.Info("Deployment reconciled", "operation", op, "name", deployment.Name)
	}

	return nil
}

func applyContainerResourceDefaults(container *corev1.Container) {
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if _, ok := container.Resources.Requests[corev1.ResourceCPU]; !ok {
		container.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(defaultRequestCPU)
	}
	if _, ok := container.Resources.Requests[corev1.ResourceMemory]; !ok {
		container.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(defaultRequestMemory)
	}

	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}
	if _, ok := container.Resources.Limits[corev1.ResourceCPU]; !ok {
		container.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(defaultLimitCPU)
	}
	if _, ok := container.Resources.Limits[corev1.ResourceMemory]; !ok {
		container.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(defaultLimitMemory)
	}
}

func (r *MCPServerReconciler) resolveImage(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (string, error) {
	logger := log.FromContext(ctx)

	image := mcpServer.Spec.Image
	// Append tag only if the image does not already include a tag or digest.
	if mcpServer.Spec.ImageTag != "" && !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		image = fmt.Sprintf("%s:%s", image, mcpServer.Spec.ImageTag)
	}

	regOverride := mcpServer.Spec.RegistryOverride
	if mcpServer.Spec.UseProvisionedRegistry {
		if r.ProvisionedRegistry != nil && r.ProvisionedRegistry.URL != "" {
			regOverride = r.ProvisionedRegistry.URL
		} else if regOverride == "" {
			// Fallback to internal registry service if not configured
			regOverride = "registry.registry.svc.cluster.local:5000"
			logger.Info("useProvisionedRegistry set without ProvisionedRegistry config; falling back to internal registry service", "mcpServer", mcpServer.Name, "registry", regOverride)
		}
	}
	if regOverride != "" {
		image = rewriteRegistry(image, regOverride)
	}

	return image, nil
}

func rewriteRegistry(image, registry string) string {
	if registry == "" {
		return image
	}
	parts := strings.Split(image, "/")
	if len(parts) == 1 {
		return fmt.Sprintf("%s/%s", registry, image)
	}

	// If first part looks like a registry (contains . or : or is localhost), drop it.
	first := parts[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		parts = parts[1:]
	}
	return fmt.Sprintf("%s/%s", registry, strings.Join(parts, "/"))
}

func (r *MCPServerReconciler) buildImagePullSecrets(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) []corev1.LocalObjectReference {
	// If user specified pull secrets, honor them.
	if len(mcpServer.Spec.ImagePullSecrets) > 0 {
		out := make([]corev1.LocalObjectReference, 0, len(mcpServer.Spec.ImagePullSecrets))
		for _, s := range mcpServer.Spec.ImagePullSecrets {
			if s == "" {
				continue
			}
			out = append(out, corev1.LocalObjectReference{Name: s})
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	// Otherwise, optionally auto-create/use a pull secret from provisioned registry creds.
	if r.ProvisionedRegistry == nil || r.ProvisionedRegistry.URL == "" ||
		r.ProvisionedRegistry.Username == "" || r.ProvisionedRegistry.Password == "" {
		return nil
	}

	secretName := r.ProvisionedRegistry.SecretName
	if secretName == "" {
		secretName = "mcp-runtime-registry-creds"
	}

	if err := r.ensureRegistryPullSecret(ctx, mcpServer.Namespace, secretName,
		r.ProvisionedRegistry.URL, r.ProvisionedRegistry.Username, r.ProvisionedRegistry.Password); err != nil {
		// Best-effort; log but do not fail reconcile.
		logger := log.FromContext(ctx)
		logger.Error(err, "failed to ensure registry pull secret", "secret", secretName, "namespace", mcpServer.Namespace)
		return nil
	}

	return []corev1.LocalObjectReference{{Name: secretName}}
}

func (r *MCPServerReconciler) ensureRegistryPullSecret(ctx context.Context, namespace, name, registry, username, password string) error {
	dockerCfg := map[string]any{
		"auths": map[string]any{
			registry: map[string]string{
				"username": username,
				"password": password,
				"auth":     base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password))),
			},
		},
	}
	raw, err := json.Marshal(dockerCfg)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: name, Namespace: namespace}
	err = r.Get(ctx, secretKey, secret)
	if err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{".dockerconfigjson": raw},
			}
			return r.Create(ctx, secret)
		}
		return err
	}

	// update if changed
	if secret.Type != corev1.SecretTypeDockerConfigJson || string(secret.Data[".dockerconfigjson"]) != string(raw) {
		secret.Type = corev1.SecretTypeDockerConfigJson
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data[".dockerconfigjson"] = raw
		return r.Update(ctx, secret)
	}
	return nil
}

func (r *MCPServerReconciler) reconcileService(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, service, func() error {
		labels := map[string]string{
			"app": mcpServer.Name,
		}

		service.Spec = corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       mcpServer.Spec.ServicePort,
					TargetPort: intstr.FromInt32(mcpServer.Spec.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		}

		if err := ctrl.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		logger.Info("Service reconciled", "operation", op, "name", service.Name)
	}

	return nil
}

func (r *MCPServerReconciler) reconcileIngress(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		pathType := networkingv1.PathTypePrefix
		ingressClassName := mcpServer.Spec.IngressClass
		if ingressClassName == "" {
			ingressClassName = "traefik" // Default to traefik
		}

		ingress.Spec = networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: mcpServer.Spec.IngressHost,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     mcpServer.Spec.IngressPath,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: mcpServer.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: mcpServer.Spec.ServicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		// Build annotations based on ingress class
		annotations := r.buildIngressAnnotations(mcpServer)
		ingress.Annotations = annotations

		if err := ctrl.SetControllerReference(mcpServer, ingress, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		logger.Info("Ingress reconciled", "operation", op, "name", ingress.Name)
	}

	return nil
}

func (r *MCPServerReconciler) checkDeploymentReady(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (bool, error) {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, deployment); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	return deployment.Status.ReadyReplicas == desiredReplicas, nil
}

func (r *MCPServerReconciler) checkServiceReady(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (bool, error) {
	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, service); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return service.Spec.ClusterIP != "", nil
}

func (r *MCPServerReconciler) checkIngressReady(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (bool, error) {
	ingress := &networkingv1.Ingress{}
	if err := r.Get(ctx, types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, ingress); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		return true, nil
	}
	return false, nil
}

func (r *MCPServerReconciler) updateStatus(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, phase, message string, deploymentReady, serviceReady, ingressReady bool) {
	mcpServer.Status.Phase = phase
	mcpServer.Status.Message = message
	mcpServer.Status.DeploymentReady = deploymentReady
	mcpServer.Status.ServiceReady = serviceReady
	mcpServer.Status.IngressReady = ingressReady

	if err := r.Status().Update(ctx, mcpServer); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update MCPServer status")
	}
}

func (r *MCPServerReconciler) buildEnvVars(envVars []mcpv1alpha1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, len(envVars))
	for i, ev := range envVars {
		result[i] = corev1.EnvVar{
			Name:  ev.Name,
			Value: ev.Value,
		}
	}
	return result
}

func (r *MCPServerReconciler) buildIngressAnnotations(mcpServer *mcpv1alpha1.MCPServer) map[string]string {
	annotations := make(map[string]string)

	// Start with user-provided annotations
	if mcpServer.Spec.IngressAnnotations != nil {
		for k, v := range mcpServer.Spec.IngressAnnotations {
			annotations[k] = v
		}
	}

	// Add controller-specific annotations based on ingress class
	ingressClass := mcpServer.Spec.IngressClass
	if ingressClass == "" {
		ingressClass = "traefik" // Default to traefik
	}

	switch ingressClass {
	case "traefik":
		// Traefik Ingress Controller annotations
		if _, exists := annotations["traefik.ingress.kubernetes.io/router.entrypoints"]; !exists {
			annotations["traefik.ingress.kubernetes.io/router.entrypoints"] = "web"
		}

	case "nginx":
		// Nginx Ingress Controller annotations
		if _, exists := annotations["nginx.ingress.kubernetes.io/rewrite-target"]; !exists {
			annotations["nginx.ingress.kubernetes.io/rewrite-target"] = "/"
		}
		if _, exists := annotations["nginx.ingress.kubernetes.io/ssl-redirect"]; !exists {
			annotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "false"
		}

	case "istio":
		// Istio Gateway/VirtualService annotations (Istio uses different approach)
		// For Istio, you typically use Gateway and VirtualService CRDs instead
		// This is a placeholder - Istio integration would need separate CRDs
		if _, exists := annotations["kubernetes.io/ingress.class"]; !exists {
			annotations["kubernetes.io/ingress.class"] = "istio"
		}

	default:
		// Generic ingress annotations for unknown controllers
		if _, exists := annotations["ingress.kubernetes.io/rewrite-target"]; !exists {
			annotations["ingress.kubernetes.io/rewrite-target"] = "/"
		}
	}

	return annotations
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
