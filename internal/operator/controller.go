/*
Let me share the flow of the code:
1. fetch the MCPServer object
2. apply the defaults if needed
3. validate the ingress config
4. reconcile the resources
5. check the resource readiness
6. determine the phase
7. update the status
8. return the result
*/
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
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

	// GatewayProxyImage is the default image used for the optional MCP gateway sidecar.
	GatewayProxyImage string

	// DefaultAnalyticsIngestURL is the default analytics ingest endpoint used when analytics is enabled.
	DefaultAnalyticsIngestURL string

	// ClusterName is the cluster label attached to policy and audit events.
	ClusterName string
}

// Use constants from constants.go
const (
	defaultRequestCPU    = DefaultRequestCPU
	defaultRequestMemory = DefaultRequestMemory
	defaultLimitCPU      = DefaultLimitCPU
	defaultLimitMemory   = DefaultLimitMemory
	defaultGatewayPort   = DefaultGatewayPort
)

const (
	gatewayPolicyVolumeName = "gateway-policy"
	gatewayPolicyMountDir   = "/var/run/mcp-runtime/policy"
	gatewayPolicyFileName   = "policy.json"
	gatewayPolicyFilePath   = gatewayPolicyMountDir + "/" + gatewayPolicyFileName
)

type resourceReadiness struct {
	Deployment bool
	Service    bool
	Ingress    bool
	Gateway    bool
	Policy     bool
	Canary     bool
}

type gatewayPolicyDocument struct {
	Server   gatewayPolicyServer    `json:"server"`
	Auth     gatewayPolicyAuth      `json:"auth"`
	Policy   gatewayPolicyConfig    `json:"policy"`
	Session  gatewayPolicySession   `json:"session"`
	Tools    []gatewayPolicyTool    `json:"tools,omitempty"`
	Grants   []gatewayPolicyGrant   `json:"grants,omitempty"`
	Sessions []gatewayPolicyBinding `json:"sessions,omitempty"`
}

type gatewayPolicyServer struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Cluster   string `json:"cluster,omitempty"`
}

type gatewayPolicyAuth struct {
	Mode            string `json:"mode,omitempty"`
	HumanIDHeader   string `json:"human_id_header,omitempty"`
	AgentIDHeader   string `json:"agent_id_header,omitempty"`
	SessionIDHeader string `json:"session_id_header,omitempty"`
	TokenHeader     string `json:"token_header,omitempty"`
	IssuerURL       string `json:"issuer_url,omitempty"`
	Audience        string `json:"audience,omitempty"`
}

type gatewayPolicyConfig struct {
	Mode            string `json:"mode,omitempty"`
	DefaultDecision string `json:"default_decision,omitempty"`
	EnforceOn       string `json:"enforce_on,omitempty"`
	PolicyVersion   string `json:"policy_version,omitempty"`
}

type gatewayPolicySession struct {
	Required            bool   `json:"required,omitempty"`
	Store               string `json:"store,omitempty"`
	HeaderName          string `json:"header_name,omitempty"`
	MaxLifetime         string `json:"max_lifetime,omitempty"`
	IdleTimeout         string `json:"idle_timeout,omitempty"`
	UpstreamTokenHeader string `json:"upstream_token_header,omitempty"`
}

type gatewayPolicyTool struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	RequiredTrust string            `json:"required_trust,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type gatewayPolicyGrant struct {
	Name          string              `json:"name"`
	HumanID       string              `json:"human_id,omitempty"`
	AgentID       string              `json:"agent_id,omitempty"`
	MaxTrust      string              `json:"max_trust,omitempty"`
	PolicyVersion string              `json:"policy_version,omitempty"`
	Disabled      bool                `json:"disabled,omitempty"`
	ToolRules     []gatewayToolAccess `json:"tool_rules,omitempty"`
}

type gatewayPolicyBinding struct {
	Name             string `json:"name"`
	HumanID          string `json:"human_id,omitempty"`
	AgentID          string `json:"agent_id,omitempty"`
	ConsentedTrust   string `json:"consented_trust,omitempty"`
	Revoked          bool   `json:"revoked,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	PolicyVersion    string `json:"policy_version,omitempty"`
	UpstreamTokenRef string `json:"upstream_token_ref,omitempty"`
}

type gatewayToolAccess struct {
	Name          string `json:"name"`
	Decision      string `json:"decision,omitempty"`
	RequiredTrust string `json:"required_trust,omitempty"`
}

//+kubebuilder:rbac:groups=mcpruntime.org,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcpruntime.org,resources=mcpservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcpruntime.org,resources=mcpservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=mcpruntime.org,resources=mcpaccessgrants,verbs=get;list;watch
//+kubebuilder:rbac:groups=mcpruntime.org,resources=mcpagentsessions,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	mcpServer, found, err := r.fetchMCPServer(ctx, req)
	if err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	if !found {
		return ctrl.Result{Requeue: false}, nil
	}

	logger.Info("Reconciling MCPServer", "name", mcpServer.Name, "namespace", mcpServer.Namespace)

	// Set defaults and update spec only if changed
	requeue, err := r.applyDefaultsIfNeeded(ctx, mcpServer, logger)
	if err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	if requeue {
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.validateIngressConfig(ctx, mcpServer, logger); err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	if err := r.validateGatewayConfig(ctx, mcpServer, logger); err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	if err := r.reconcileResources(ctx, mcpServer, logger); err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	readiness, err := r.checkResourceReadiness(ctx, mcpServer)
	if err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	phase, allReady := determinePhase(readiness)
	r.updateStatus(ctx, mcpServer, phase, "All resources reconciled", readiness)

	logger.Info("Successfully reconciled MCPServer", "name", mcpServer.Name, "phase", phase)

	// If not all resources are ready, requeue with a short delay to check again
	if !allReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *MCPServerReconciler) fetchMCPServer(ctx context.Context, req ctrl.Request) (*mcpv1alpha1.MCPServer, bool, error) {
	var mcpServer mcpv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &mcpServer); err != nil {
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &mcpServer, true, nil
}

func (r *MCPServerReconciler) applyDefaultsIfNeeded(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger) (bool, error) {
	original := mcpServer.DeepCopy()
	r.setDefaults(mcpServer)
	if reflect.DeepEqual(original.Spec, mcpServer.Spec) {
		return false, nil
	}
	if err := r.Update(ctx, mcpServer); err != nil {
		logger.Error(err, "Failed to update MCPServer spec with defaults")
		return false, err
	}
	// Requeue to work with the updated object and avoid stale data
	return true, nil
}

func (r *MCPServerReconciler) validateIngressConfig(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger) error {
	if err := r.requireSpecField(ctx, mcpServer, logger, "ingress host", mcpServer.Spec.IngressHost,
		"ingressHost is required; set spec.ingressHost or MCP_DEFAULT_INGRESS_HOST"); err != nil {
		return err
	}
	if err := r.requireSpecField(ctx, mcpServer, logger, "ingress path", mcpServer.Spec.IngressPath,
		"ingressPath is required; set spec.ingressPath or ensure metadata.name is set"); err != nil {
		return err
	}
	return nil
}

func (r *MCPServerReconciler) validateGatewayConfig(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger) error {
	if gatewayEnabled(mcpServer) {
		if mcpServer.Spec.Gateway.Port == mcpServer.Spec.Port {
			contextMap := map[string]any{
				"mcpServer":    mcpServer.Name,
				"namespace":    mcpServer.Namespace,
				"gatewayPort":  mcpServer.Spec.Gateway.Port,
				"serverPort":   mcpServer.Spec.Port,
				"gatewayImage": mcpServer.Spec.Gateway.Image,
			}
			err := newOperatorError("gateway.port must be different from spec.port", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid gateway port configuration")
			return err
		}
		if _, err := r.resolveGatewayImage(mcpServer); err != nil {
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Missing gateway image")
			return err
		}
	}

	for _, tool := range mcpServer.Spec.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("tools[].name is required", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid tool definition")
			return err
		}
	}

	for _, secretEnv := range mcpServer.Spec.SecretEnvVars {
		if strings.TrimSpace(secretEnv.Name) == "" || secretEnv.SecretKeyRef == nil ||
			strings.TrimSpace(secretEnv.SecretKeyRef.Name) == "" || strings.TrimSpace(secretEnv.SecretKeyRef.Key) == "" {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("secretEnvVars require name and secretKeyRef.name/key", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid secret-backed env var")
			return err
		}
	}

	if canaryEnabled(mcpServer) {
		if mcpServer.Spec.Rollout.CanaryReplicas == nil || *mcpServer.Spec.Rollout.CanaryReplicas <= 0 {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("rollout.canaryReplicas must be greater than zero when rollout.strategy=Canary", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid canary rollout")
			return err
		}
		if mcpServer.Spec.Replicas == nil {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("spec.replicas must be set when rollout.strategy=Canary", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Missing replicas for canary rollout")
			return err
		}
		if *mcpServer.Spec.Rollout.CanaryReplicas >= *mcpServer.Spec.Replicas {
			contextMap := map[string]any{
				"mcpServer":      mcpServer.Name,
				"namespace":      mcpServer.Namespace,
				"replicas":       *mcpServer.Spec.Replicas,
				"canaryReplicas": *mcpServer.Spec.Rollout.CanaryReplicas,
			}
			err := newOperatorError("rollout.canaryReplicas must be less than spec.replicas", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid canary replica split")
			return err
		}
	}

	if rollout := mcpServer.Spec.Rollout; rollout != nil {
		if err := r.validateRolloutValue(ctx, mcpServer, logger, "rollout.maxUnavailable", rollout.MaxUnavailable); err != nil {
			return err
		}
		if err := r.validateRolloutValue(ctx, mcpServer, logger, "rollout.maxSurge", rollout.MaxSurge); err != nil {
			return err
		}
	}

	if analyticsEnabled(mcpServer) {
		if !gatewayEnabled(mcpServer) {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("analytics.enabled requires gateway.enabled", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Analytics requires gateway")
			return err
		}
		if err := r.requireSpecField(ctx, mcpServer, logger, "analytics ingest URL", mcpServer.Spec.Analytics.IngestURL,
			"analytics.ingestURL is required when analytics.enabled is true"); err != nil {
			return err
		}
		if ref := mcpServer.Spec.Analytics.APIKeySecretRef; ref != nil && (strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Key) == "") {
			contextMap := map[string]any{
				"mcpServer": mcpServer.Name,
				"namespace": mcpServer.Namespace,
			}
			err := newOperatorError("analytics.apiKeySecretRef requires both name and key", contextMap)
			r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
			logOperatorError(logger, err, "Invalid analytics secret reference")
			return err
		}
	}

	return nil
}

func (r *MCPServerReconciler) validateRolloutValue(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger, fieldName, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	numeric := strings.TrimSuffix(trimmed, "%")
	if numeric == "" {
		contextMap := map[string]any{
			"mcpServer": mcpServer.Name,
			"namespace": mcpServer.Namespace,
			"field":     fieldName,
			"value":     trimmed,
		}
		err := newOperatorError(fieldName+" must be an integer or percentage", contextMap)
		r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
		logOperatorError(logger, err, "Invalid rollout value")
		return err
	}

	parsed, err := strconv.Atoi(numeric)
	if err != nil || parsed < 0 {
		contextMap := map[string]any{
			"mcpServer": mcpServer.Name,
			"namespace": mcpServer.Namespace,
			"field":     fieldName,
			"value":     trimmed,
		}
		validationErr := newOperatorError(fieldName+" must be an integer or percentage", contextMap)
		r.updateStatus(ctx, mcpServer, "Error", validationErr.Error(), resourceReadiness{})
		logOperatorError(logger, validationErr, "Invalid rollout value")
		return validationErr
	}

	return nil
}

func (r *MCPServerReconciler) requireSpecField(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger, field, value, message string) error {
	if value != "" {
		return nil
	}
	contextMap := map[string]any{
		"mcpServer": mcpServer.Name,
		"namespace": mcpServer.Namespace,
		"field":     field,
	}
	err := newOperatorError(message, contextMap)
	r.updateStatus(ctx, mcpServer, "Error", err.Error(), resourceReadiness{})
	logOperatorError(logger, err, "Missing "+field)
	return err
}

func (r *MCPServerReconciler) reconcileResources(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, logger logr.Logger) error {
	contextMap := map[string]any{
		"mcpServer": mcpServer.Name,
		"namespace": mcpServer.Namespace,
	}

	if err := r.reconcilePolicyConfigMap(ctx, mcpServer); err != nil {
		contextMap["resource"] = "configmap"
		wrappedErr := wrapOperatorError(err, "Failed to reconcile policy ConfigMap", contextMap)
		logOperatorError(logger, wrappedErr, "Failed to reconcile policy ConfigMap")
		r.updateStatus(ctx, mcpServer, "Error", fmt.Sprintf("Failed to reconcile policy ConfigMap: %v", err), resourceReadiness{})
		return wrappedErr
	}
	if err := r.reconcileDeployment(ctx, mcpServer); err != nil {
		contextMap["resource"] = "deployment"
		wrappedErr := wrapOperatorError(err, "Failed to reconcile Deployment", contextMap)
		logOperatorError(logger, wrappedErr, "Failed to reconcile Deployment")
		r.updateStatus(ctx, mcpServer, "Error", fmt.Sprintf("Failed to reconcile Deployment: %v", err), resourceReadiness{})
		return wrappedErr
	}
	if err := r.reconcileCanaryDeployment(ctx, mcpServer); err != nil {
		contextMap["resource"] = "canary-deployment"
		wrappedErr := wrapOperatorError(err, "Failed to reconcile canary Deployment", contextMap)
		logOperatorError(logger, wrappedErr, "Failed to reconcile canary Deployment")
		r.updateStatus(ctx, mcpServer, "Error", fmt.Sprintf("Failed to reconcile canary Deployment: %v", err), resourceReadiness{})
		return wrappedErr
	}
	if err := r.reconcileService(ctx, mcpServer); err != nil {
		contextMap["resource"] = "service"
		wrappedErr := wrapOperatorError(err, "Failed to reconcile Service", contextMap)
		logOperatorError(logger, wrappedErr, "Failed to reconcile Service")
		r.updateStatus(ctx, mcpServer, "Error", fmt.Sprintf("Failed to reconcile Service: %v", err), resourceReadiness{})
		return wrappedErr
	}
	if err := r.reconcileIngress(ctx, mcpServer); err != nil {
		contextMap["resource"] = "ingress"
		wrappedErr := wrapOperatorError(err, "Failed to reconcile Ingress", contextMap)
		logOperatorError(logger, wrappedErr, "Failed to reconcile Ingress")
		r.updateStatus(ctx, mcpServer, "Error", fmt.Sprintf("Failed to reconcile Ingress: %v", err), resourceReadiness{})
		return wrappedErr
	}
	return nil
}

func (r *MCPServerReconciler) checkResourceReadiness(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (resourceReadiness, error) {
	deploymentReady, err := r.checkDeploymentReady(ctx, mcpServer)
	if err != nil {
		return resourceReadiness{}, err
	}
	serviceReady, err := r.checkServiceReady(ctx, mcpServer)
	if err != nil {
		return resourceReadiness{}, err
	}
	ingressReady, err := r.checkIngressReady(ctx, mcpServer)
	if err != nil {
		return resourceReadiness{}, err
	}
	policyReady, err := r.checkPolicyConfigMapReady(ctx, mcpServer)
	if err != nil {
		return resourceReadiness{}, err
	}
	canaryReady, err := r.checkCanaryDeploymentReady(ctx, mcpServer)
	if err != nil {
		return resourceReadiness{}, err
	}

	gatewayReady := true
	if gatewayEnabled(mcpServer) {
		gatewayReady = deploymentReady
	}

	return resourceReadiness{
		Deployment: deploymentReady,
		Service:    serviceReady,
		Ingress:    ingressReady,
		Gateway:    gatewayReady,
		Policy:     policyReady,
		Canary:     canaryReady,
	}, nil
}

func determinePhase(readiness resourceReadiness) (string, bool) {
	allReady := readiness.Deployment && readiness.Service && readiness.Ingress && readiness.Gateway && readiness.Policy && readiness.Canary
	if allReady {
		return "Ready", true
	}
	if readiness.Deployment || readiness.Service || readiness.Ingress || readiness.Gateway || readiness.Policy || readiness.Canary {
		return "PartiallyReady", false
	}
	return "Pending", false
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
		mcpServer.Spec.Port = DefaultPort
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
	if gatewayEnabled(mcpServer) {
		if mcpServer.Spec.Auth == nil {
			mcpServer.Spec.Auth = &mcpv1alpha1.AuthConfig{}
		}
		if mcpServer.Spec.Policy == nil {
			mcpServer.Spec.Policy = &mcpv1alpha1.PolicyConfig{}
		}
		if mcpServer.Spec.Session == nil {
			mcpServer.Spec.Session = &mcpv1alpha1.SessionConfig{}
		}
	}
	if mcpServer.Spec.Auth != nil {
		if mcpServer.Spec.Auth.Mode == "" {
			mcpServer.Spec.Auth.Mode = mcpv1alpha1.AuthModeHeader
		}
		if mcpServer.Spec.Auth.HumanIDHeader == "" {
			mcpServer.Spec.Auth.HumanIDHeader = "X-MCP-Human-ID"
		}
		if mcpServer.Spec.Auth.AgentIDHeader == "" {
			mcpServer.Spec.Auth.AgentIDHeader = "X-MCP-Agent-ID"
		}
		if mcpServer.Spec.Auth.SessionIDHeader == "" {
			mcpServer.Spec.Auth.SessionIDHeader = "X-MCP-Agent-Session"
		}
		if mcpServer.Spec.Auth.TokenHeader == "" {
			mcpServer.Spec.Auth.TokenHeader = "Authorization"
		}
	}
	if mcpServer.Spec.Policy != nil {
		if mcpServer.Spec.Policy.Mode == "" {
			mcpServer.Spec.Policy.Mode = mcpv1alpha1.PolicyModeAllowList
		}
		if mcpServer.Spec.Policy.DefaultDecision == "" {
			mcpServer.Spec.Policy.DefaultDecision = mcpv1alpha1.PolicyDecisionDeny
		}
		if mcpServer.Spec.Policy.EnforceOn == "" {
			mcpServer.Spec.Policy.EnforceOn = "call_tool"
		}
		if mcpServer.Spec.Policy.PolicyVersion == "" {
			mcpServer.Spec.Policy.PolicyVersion = "v1"
		}
	}
	if mcpServer.Spec.Session != nil {
		if mcpServer.Spec.Session.Store == "" {
			mcpServer.Spec.Session.Store = "kubernetes"
		}
		if mcpServer.Spec.Session.HeaderName == "" {
			mcpServer.Spec.Session.HeaderName = "X-MCP-Agent-Session"
		}
		if mcpServer.Spec.Session.MaxLifetime == "" {
			mcpServer.Spec.Session.MaxLifetime = "24h"
		}
		if mcpServer.Spec.Session.IdleTimeout == "" {
			mcpServer.Spec.Session.IdleTimeout = "1h"
		}
		if mcpServer.Spec.Session.UpstreamTokenHeader == "" {
			mcpServer.Spec.Session.UpstreamTokenHeader = "Authorization"
		}
	}
	for i := range mcpServer.Spec.Tools {
		if mcpServer.Spec.Tools[i].RequiredTrust == "" {
			mcpServer.Spec.Tools[i].RequiredTrust = mcpv1alpha1.TrustLevelLow
		}
	}
	if gatewayEnabled(mcpServer) {
		if mcpServer.Spec.Gateway.Port == 0 {
			mcpServer.Spec.Gateway.Port = defaultGatewayPort
		}
		if mcpServer.Spec.Gateway.UpstreamURL == "" {
			mcpServer.Spec.Gateway.UpstreamURL = fmt.Sprintf("http://127.0.0.1:%d", mcpServer.Spec.Port)
		}
	}
	if analyticsEnabled(mcpServer) {
		if mcpServer.Spec.Analytics.IngestURL == "" && r.DefaultAnalyticsIngestURL != "" {
			mcpServer.Spec.Analytics.IngestURL = r.DefaultAnalyticsIngestURL
		}
		if mcpServer.Spec.Analytics.Source == "" && mcpServer.Name != "" {
			mcpServer.Spec.Analytics.Source = mcpServer.Name
		}
		if mcpServer.Spec.Analytics.EventType == "" {
			mcpServer.Spec.Analytics.EventType = "mcp.request"
		}
	}
	if mcpServer.Spec.Rollout != nil {
		if mcpServer.Spec.Rollout.Strategy == "" {
			mcpServer.Spec.Rollout.Strategy = mcpv1alpha1.RolloutStrategyRollingUpdate
		}
		if mcpServer.Spec.Rollout.MaxUnavailable == "" {
			mcpServer.Spec.Rollout.MaxUnavailable = "25%"
		}
		if mcpServer.Spec.Rollout.MaxSurge == "" {
			mcpServer.Spec.Rollout.MaxSurge = "25%"
		}
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
			"app":                          mcpServer.Name,
			"mcpruntime.org/rollout-track": "stable",
		}
		templateLabels := map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
			"mcpruntime.org/rollout-track": "stable",
		}
		replicas := desiredStableReplicas(mcpServer)

		deployment.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
			"mcpruntime.org/rollout-track": "stable",
		}

		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Strategy: deploymentStrategy(mcpServer),
		}
		deployment.Spec.Template.ObjectMeta.Labels = templateLabels

		containers, volumes, err := r.buildDeploymentContainers(mcpServer, image)
		if err != nil {
			return err
		}
		deployment.Spec.Template.Spec = corev1.PodSpec{
			ImagePullSecrets: r.buildImagePullSecrets(mcpServer),
			Containers:       containers,
			Volumes:          volumes,
		}

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

func (r *MCPServerReconciler) reconcileCanaryDeployment(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	if !canaryEnabled(mcpServer) {
		existing := &appsv1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{Name: canaryDeploymentName(mcpServer.Name), Namespace: mcpServer.Namespace}, existing)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.Delete(ctx, existing)
	}

	logger := log.FromContext(ctx)
	image, err := r.resolveImage(ctx, mcpServer)
	if err != nil {
		return err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      canaryDeploymentName(mcpServer.Name),
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		replicas := int32(0)
		if mcpServer.Spec.Rollout != nil && mcpServer.Spec.Rollout.CanaryReplicas != nil {
			replicas = *mcpServer.Spec.Rollout.CanaryReplicas
		}
		selectorLabels := map[string]string{
			"app":                          mcpServer.Name,
			"mcpruntime.org/rollout-track": "canary",
		}
		deployment.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
			"mcpruntime.org/rollout-track": "canary",
		}
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Strategy: deploymentStrategy(mcpServer),
		}
		deployment.Spec.Template.ObjectMeta.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
			"mcpruntime.org/rollout-track": "canary",
		}
		containers, volumes, err := r.buildDeploymentContainers(mcpServer, image)
		if err != nil {
			return err
		}
		deployment.Spec.Template.Spec = corev1.PodSpec{
			ImagePullSecrets: r.buildImagePullSecrets(mcpServer),
			Containers:       containers,
			Volumes:          volumes,
		}
		if err := ctrl.SetControllerReference(mcpServer, deployment, r.Scheme); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Canary deployment reconciled", "operation", op, "name", deployment.Name)
	}
	return nil
}

func (r *MCPServerReconciler) buildDeploymentContainers(mcpServer *mcpv1alpha1.MCPServer, image string) ([]corev1.Container, []corev1.Volume, error) {
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
		Env: r.buildEnvVars(mcpServer.Spec.EnvVars, mcpServer.Spec.SecretEnvVars),
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

	if err := applyContainerResources(&container, mcpServer.Spec.Resources); err != nil {
		return nil, nil, err
	}

	containers := []corev1.Container{container}
	var volumes []corev1.Volume
	if gatewayEnabled(mcpServer) {
		gatewayContainer, err := r.buildGatewayContainer(mcpServer)
		if err != nil {
			return nil, nil, err
		}
		containers = append(containers, gatewayContainer)
		volumes = append(volumes, corev1.Volume{
			Name: gatewayPolicyVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: gatewayPolicyConfigMapName(mcpServer.Name)},
				},
			},
		})
	}

	return containers, volumes, nil
}

// applyContainerResources sets container resource requests and limits.
// It applies defaults first, then overrides with user-specified values.
func applyContainerResources(container *corev1.Container, resources mcpv1alpha1.ResourceRequirements) error {
	// Initialize maps
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	// Apply defaults
	container.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(defaultRequestCPU)
	container.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(defaultRequestMemory)
	container.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(defaultLimitCPU)
	container.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(defaultLimitMemory)

	// Override with user-specified values
	if resources.Requests != nil {
		if resources.Requests.CPU != "" {
			cpu, err := resource.ParseQuantity(resources.Requests.CPU)
			if err != nil {
				contextMap := map[string]any{
					"resource": "cpu",
					"type":     "request",
					"value":    resources.Requests.CPU,
				}
				return wrapOperatorError(err, fmt.Sprintf("invalid CPU request %q", resources.Requests.CPU), contextMap)
			}
			container.Resources.Requests[corev1.ResourceCPU] = cpu
		}
		if resources.Requests.Memory != "" {
			mem, err := resource.ParseQuantity(resources.Requests.Memory)
			if err != nil {
				contextMap := map[string]any{
					"resource": "memory",
					"type":     "request",
					"value":    resources.Requests.Memory,
				}
				return wrapOperatorError(err, fmt.Sprintf("invalid memory request %q", resources.Requests.Memory), contextMap)
			}
			container.Resources.Requests[corev1.ResourceMemory] = mem
		}
	}

	if resources.Limits != nil {
		if resources.Limits.CPU != "" {
			cpu, err := resource.ParseQuantity(resources.Limits.CPU)
			if err != nil {
				contextMap := map[string]any{
					"resource": "cpu",
					"type":     "limit",
					"value":    resources.Limits.CPU,
				}
				return wrapOperatorError(err, fmt.Sprintf("invalid CPU limit %q", resources.Limits.CPU), contextMap)
			}
			container.Resources.Limits[corev1.ResourceCPU] = cpu
		}
		if resources.Limits.Memory != "" {
			mem, err := resource.ParseQuantity(resources.Limits.Memory)
			if err != nil {
				contextMap := map[string]any{
					"resource": "memory",
					"type":     "limit",
					"value":    resources.Limits.Memory,
				}
				return wrapOperatorError(err, fmt.Sprintf("invalid memory limit %q", resources.Limits.Memory), contextMap)
			}
			container.Resources.Limits[corev1.ResourceMemory] = mem
		}
	}

	return nil
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

func (r *MCPServerReconciler) resolveGatewayImage(mcpServer *mcpv1alpha1.MCPServer) (string, error) {
	if !gatewayEnabled(mcpServer) {
		return "", nil
	}

	image := strings.TrimSpace(mcpServer.Spec.Gateway.Image)
	if image == "" {
		image = strings.TrimSpace(r.GatewayProxyImage)
	}
	if image != "" {
		return image, nil
	}

	contextMap := map[string]any{
		"mcpServer": mcpServer.Name,
		"namespace": mcpServer.Namespace,
	}
	return "", newOperatorError("gateway.image is required when gateway.enabled is true (set spec.gateway.image or MCP_GATEWAY_PROXY_IMAGE on the operator)", contextMap)
}

func (r *MCPServerReconciler) buildGatewayContainer(mcpServer *mcpv1alpha1.MCPServer) (corev1.Container, error) {
	image, err := r.resolveGatewayImage(mcpServer)
	if err != nil {
		return corev1.Container{}, err
	}

	port := mcpServer.Spec.Gateway.Port
	envVars := []corev1.EnvVar{
		{Name: "PORT", Value: strconv.Itoa(int(port))},
		{Name: "UPSTREAM_URL", Value: mcpServer.Spec.Gateway.UpstreamURL},
		{Name: "POLICY_FILE", Value: gatewayPolicyFilePath},
		{Name: "MCP_SERVER_NAME", Value: mcpServer.Name},
		{Name: "MCP_SERVER_NAMESPACE", Value: mcpServer.Namespace},
		{Name: "MCP_CLUSTER_NAME", Value: strings.TrimSpace(r.ClusterName)},
	}
	if mcpServer.Spec.Policy != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "POLICY_MODE", Value: string(mcpServer.Spec.Policy.Mode)},
			corev1.EnvVar{Name: "POLICY_DEFAULT_DECISION", Value: string(mcpServer.Spec.Policy.DefaultDecision)},
			corev1.EnvVar{Name: "POLICY_VERSION", Value: mcpServer.Spec.Policy.PolicyVersion},
		)
	}
	if mcpServer.Spec.Auth != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "HUMAN_ID_HEADER", Value: mcpServer.Spec.Auth.HumanIDHeader},
			corev1.EnvVar{Name: "AGENT_ID_HEADER", Value: mcpServer.Spec.Auth.AgentIDHeader},
			corev1.EnvVar{Name: "SESSION_ID_HEADER", Value: mcpServer.Spec.Auth.SessionIDHeader},
			corev1.EnvVar{Name: "AUTH_MODE", Value: string(mcpServer.Spec.Auth.Mode)},
		)
	}
	if mcpServer.Spec.Gateway.StripPrefix != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "STRIP_PREFIX", Value: mcpServer.Spec.Gateway.StripPrefix})
	}
	if analyticsEnabled(mcpServer) {
		envVars = append(envVars,
			corev1.EnvVar{Name: "ANALYTICS_INGEST_URL", Value: mcpServer.Spec.Analytics.IngestURL},
			corev1.EnvVar{Name: "ANALYTICS_SOURCE", Value: mcpServer.Spec.Analytics.Source},
			corev1.EnvVar{Name: "ANALYTICS_EVENT_TYPE", Value: mcpServer.Spec.Analytics.EventType},
		)
		if ref := mcpServer.Spec.Analytics.APIKeySecretRef; ref != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name: "ANALYTICS_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
						Key:                  ref.Key,
					},
				},
			})
		}
	}

	container := corev1.Container{
		Name:            "mcp-gateway",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				Name:          "gateway",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: envVars,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      gatewayPolicyVolumeName,
				MountPath: gatewayPolicyMountDir,
				ReadOnly:  true,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       5,
		},
	}
	if err := applyContainerResources(&container, mcpv1alpha1.ResourceRequirements{}); err != nil {
		return corev1.Container{}, err
	}
	return container, nil
}

func (r *MCPServerReconciler) reconcilePolicyConfigMap(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	name := gatewayPolicyConfigMapName(mcpServer.Name)
	existing := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: name, Namespace: mcpServer.Namespace}

	if !gatewayEnabled(mcpServer) {
		if err := r.Get(ctx, key, existing); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return r.Delete(ctx, existing)
	}

	doc, err := r.renderGatewayPolicy(ctx, mcpServer)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: mcpServer.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Labels = map[string]string{
			"app":                          mcpServer.Name,
			"app.kubernetes.io/managed-by": "mcp-runtime",
		}
		configMap.Data = map[string]string{
			gatewayPolicyFileName: string(data),
		}
		return ctrl.SetControllerReference(mcpServer, configMap, r.Scheme)
	})
	return err
}

func (r *MCPServerReconciler) renderGatewayPolicy(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (*gatewayPolicyDocument, error) {
	doc := &gatewayPolicyDocument{
		Server: gatewayPolicyServer{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Cluster:   strings.TrimSpace(r.ClusterName),
		},
	}

	if mcpServer.Spec.Auth != nil {
		doc.Auth = gatewayPolicyAuth{
			Mode:            string(mcpServer.Spec.Auth.Mode),
			HumanIDHeader:   mcpServer.Spec.Auth.HumanIDHeader,
			AgentIDHeader:   mcpServer.Spec.Auth.AgentIDHeader,
			SessionIDHeader: mcpServer.Spec.Auth.SessionIDHeader,
			TokenHeader:     mcpServer.Spec.Auth.TokenHeader,
			IssuerURL:       mcpServer.Spec.Auth.IssuerURL,
			Audience:        mcpServer.Spec.Auth.Audience,
		}
	}
	if mcpServer.Spec.Policy != nil {
		doc.Policy = gatewayPolicyConfig{
			Mode:            string(mcpServer.Spec.Policy.Mode),
			DefaultDecision: string(mcpServer.Spec.Policy.DefaultDecision),
			EnforceOn:       mcpServer.Spec.Policy.EnforceOn,
			PolicyVersion:   mcpServer.Spec.Policy.PolicyVersion,
		}
	}
	if mcpServer.Spec.Session != nil {
		doc.Session = gatewayPolicySession{
			Required:            mcpServer.Spec.Session.Required,
			Store:               mcpServer.Spec.Session.Store,
			HeaderName:          mcpServer.Spec.Session.HeaderName,
			MaxLifetime:         mcpServer.Spec.Session.MaxLifetime,
			IdleTimeout:         mcpServer.Spec.Session.IdleTimeout,
			UpstreamTokenHeader: mcpServer.Spec.Session.UpstreamTokenHeader,
		}
	}
	if len(mcpServer.Spec.Tools) > 0 {
		doc.Tools = make([]gatewayPolicyTool, 0, len(mcpServer.Spec.Tools))
		for _, tool := range mcpServer.Spec.Tools {
			rendered := gatewayPolicyTool{
				Name:          tool.Name,
				Description:   tool.Description,
				RequiredTrust: string(tool.RequiredTrust),
			}
			if len(tool.Labels) > 0 {
				rendered.Labels = make(map[string]string, len(tool.Labels))
				for k, v := range tool.Labels {
					rendered.Labels[k] = v
				}
			}
			doc.Tools = append(doc.Tools, rendered)
		}
	}

	var grants mcpv1alpha1.MCPAccessGrantList
	if err := r.List(ctx, &grants); err != nil {
		return nil, err
	}
	for _, grant := range grants.Items {
		if !serverReferenceMatches(grant.Namespace, grant.Spec.ServerRef, mcpServer) {
			continue
		}
		rendered := gatewayPolicyGrant{
			Name:          grant.Name,
			HumanID:       grant.Spec.Subject.HumanID,
			AgentID:       grant.Spec.Subject.AgentID,
			MaxTrust:      string(defaultTrust(grant.Spec.MaxTrust)),
			PolicyVersion: grant.Spec.PolicyVersion,
			Disabled:      grant.Spec.Disabled,
		}
		for _, rule := range grant.Spec.ToolRules {
			rendered.ToolRules = append(rendered.ToolRules, gatewayToolAccess{
				Name:          rule.Name,
				Decision:      string(defaultDecision(rule.Decision)),
				RequiredTrust: string(defaultTrust(rule.RequiredTrust)),
			})
		}
		doc.Grants = append(doc.Grants, rendered)
	}

	var sessions mcpv1alpha1.MCPAgentSessionList
	if err := r.List(ctx, &sessions); err != nil {
		return nil, err
	}
	for _, session := range sessions.Items {
		if !serverReferenceMatches(session.Namespace, session.Spec.ServerRef, mcpServer) {
			continue
		}
		rendered := gatewayPolicyBinding{
			Name:           session.Name,
			HumanID:        session.Spec.Subject.HumanID,
			AgentID:        session.Spec.Subject.AgentID,
			ConsentedTrust: string(defaultTrust(session.Spec.ConsentedTrust)),
			Revoked:        session.Spec.Revoked,
			PolicyVersion:  session.Spec.PolicyVersion,
		}
		if session.Spec.ExpiresAt != nil {
			rendered.ExpiresAt = session.Spec.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if session.Spec.UpstreamTokenSecretRef != nil {
			rendered.UpstreamTokenRef = fmt.Sprintf("%s/%s", session.Spec.UpstreamTokenSecretRef.Name, session.Spec.UpstreamTokenSecretRef.Key)
		}
		doc.Sessions = append(doc.Sessions, rendered)
	}

	return doc, nil
}

func serverReferenceMatches(objectNamespace string, ref mcpv1alpha1.ServerReference, server *mcpv1alpha1.MCPServer) bool {
	namespace := strings.TrimSpace(ref.Namespace)
	if namespace == "" {
		namespace = objectNamespace
	}
	return ref.Name == server.Name && namespace == server.Namespace
}

func gatewayPolicyConfigMapName(serverName string) string {
	return serverName + "-gateway-policy"
}

func defaultTrust(trust mcpv1alpha1.TrustLevel) mcpv1alpha1.TrustLevel {
	if trust == "" {
		return mcpv1alpha1.TrustLevelLow
	}
	return trust
}

func defaultDecision(decision mcpv1alpha1.PolicyDecision) mcpv1alpha1.PolicyDecision {
	if decision == "" {
		return mcpv1alpha1.PolicyDecisionAllow
	}
	return decision
}

func desiredStableReplicas(mcpServer *mcpv1alpha1.MCPServer) int32 {
	if mcpServer.Spec.Replicas == nil {
		return 1
	}
	replicas := *mcpServer.Spec.Replicas
	if canaryEnabled(mcpServer) && mcpServer.Spec.Rollout != nil && mcpServer.Spec.Rollout.CanaryReplicas != nil {
		replicas -= *mcpServer.Spec.Rollout.CanaryReplicas
	}
	if replicas < 0 {
		return 0
	}
	return replicas
}

func deploymentStrategy(mcpServer *mcpv1alpha1.MCPServer) appsv1.DeploymentStrategy {
	if mcpServer.Spec.Rollout == nil || mcpServer.Spec.Rollout.Strategy == "" || mcpServer.Spec.Rollout.Strategy == mcpv1alpha1.RolloutStrategyRollingUpdate || mcpServer.Spec.Rollout.Strategy == mcpv1alpha1.RolloutStrategyCanary {
		maxUnavailable := intstr.FromString("25%")
		maxSurge := intstr.FromString("25%")
		if mcpServer.Spec.Rollout != nil {
			if mcpServer.Spec.Rollout.MaxUnavailable != "" {
				maxUnavailable = intstr.Parse(mcpServer.Spec.Rollout.MaxUnavailable)
			}
			if mcpServer.Spec.Rollout.MaxSurge != "" {
				maxSurge = intstr.Parse(mcpServer.Spec.Rollout.MaxSurge)
			}
		}
		return appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &maxUnavailable,
				MaxSurge:       &maxSurge,
			},
		}
	}
	return appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
}

func canaryDeploymentName(serverName string) string {
	return serverName + "-canary"
}

func canaryEnabled(mcpServer *mcpv1alpha1.MCPServer) bool {
	return mcpServer != nil &&
		mcpServer.Spec.Rollout != nil &&
		mcpServer.Spec.Rollout.Strategy == mcpv1alpha1.RolloutStrategyCanary
}

func (r *MCPServerReconciler) checkPolicyConfigMapReady(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (bool, error) {
	if !gatewayEnabled(mcpServer) {
		return true, nil
	}
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: gatewayPolicyConfigMapName(mcpServer.Name), Namespace: mcpServer.Namespace}, configMap); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	_, ok := configMap.Data[gatewayPolicyFileName]
	return ok, nil
}

func (r *MCPServerReconciler) checkCanaryDeploymentReady(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (bool, error) {
	if !canaryEnabled(mcpServer) {
		return true, nil
	}
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: canaryDeploymentName(mcpServer.Name), Namespace: mcpServer.Namespace}, deployment); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	desiredReplicas := int32(0)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	return deployment.Status.ReadyReplicas == desiredReplicas, nil
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

func (r *MCPServerReconciler) buildImagePullSecrets(mcpServer *mcpv1alpha1.MCPServer) []corev1.LocalObjectReference {
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

	// Otherwise, use the provisioned registry secret if configured.
	// The secret is created during setup (mcp-runtime setup), not during reconciliation.
	if r.ProvisionedRegistry == nil || r.ProvisionedRegistry.URL == "" ||
		r.ProvisionedRegistry.Username == "" || r.ProvisionedRegistry.Password == "" {
		return nil
	}

	secretName := r.ProvisionedRegistry.SecretName
	if secretName == "" {
		secretName = "mcp-runtime-registry-creds" // #nosec G101 -- default secret name, not a credential.
	}

	return []corev1.LocalObjectReference{{Name: secretName}}
}

func (r *MCPServerReconciler) reconcileService(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)
	targetPort := mcpServer.Spec.Port
	if gatewayEnabled(mcpServer) {
		targetPort = mcpServer.Spec.Gateway.Port
	}

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
					TargetPort: intstr.FromInt32(targetPort),
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

	// The presence of an IngressClass alone is not a readiness signal. Some local/dev
	// controllers will accept ingress resources before publishing any admitted status, but
	// marking the route ready here can hide real traffic-programming delays in production.
	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName != "" && len(ingress.Spec.Rules) > 0 {
		ingressClass := &networkingv1.IngressClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: *ingress.Spec.IngressClassName}, ingressClass); err != nil && !errors.IsNotFound(err) {
			return false, err
		}
	}

	return false, nil
}

func (r *MCPServerReconciler) updateStatus(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, phase, message string, readiness resourceReadiness) {
	// Re-fetch the object to get the latest resourceVersion before updating status.
	// This prevents "object has been modified" errors due to optimistic concurrency control.
	// The object might have been updated by:
	// 1. applyDefaultsIfNeeded() updating the spec
	// 2. Another reconcile loop running concurrently
	// 3. External modifications
	latest := &mcpv1alpha1.MCPServer{}
	if err := r.Get(ctx, types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, latest); err != nil {
		// If object not found, it may have been deleted - skip status update
		if errors.IsNotFound(err) {
			log.FromContext(ctx).V(1).Info("MCPServer not found, skipping status update (may have been deleted)")
			return
		}
		// For other errors, log but try to update with the original object as fallback
		log.FromContext(ctx).Error(err, "Failed to fetch MCPServer for status update, using original object")
		latest = mcpServer
	}

	// Update status fields
	latest.Status.Phase = phase
	latest.Status.Message = message
	latest.Status.DeploymentReady = readiness.Deployment
	latest.Status.ServiceReady = readiness.Service
	latest.Status.IngressReady = readiness.Ingress
	latest.Status.GatewayReady = readiness.Gateway
	latest.Status.PolicyReady = readiness.Policy
	latest.Status.CanaryReady = readiness.Canary

	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "DeploymentReady",
		Status:             conditionStatus(readiness.Deployment),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "ServiceReady",
		Status:             conditionStatus(readiness.Service),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "IngressReady",
		Status:             conditionStatus(readiness.Ingress),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "GatewayReady",
		Status:             conditionStatus(readiness.Gateway),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "PolicyReady",
		Status:             conditionStatus(readiness.Policy),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "CanaryReady",
		Status:             conditionStatus(readiness.Canary),
		Reason:             phase,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: latest.Generation,
	})

	// Use Status().Update() which only updates the status subresource
	// This is safer than Update() which would update the entire object
	if err := r.Status().Update(ctx, latest); err != nil {
		// If it's a conflict error, that's expected in concurrent scenarios - log at debug level
		if errors.IsConflict(err) {
			log.FromContext(ctx).V(1).Info("Status update conflict (expected in concurrent reconciles), will retry on next reconcile", "resourceVersion", latest.ResourceVersion)
		} else {
			// Log other errors but don't fail the reconcile - status update failures are non-fatal
			// The next reconcile will retry the status update
			log.FromContext(ctx).Error(err, "Failed to update MCPServer status", "resourceVersion", latest.ResourceVersion)
		}
	}
}

func (r *MCPServerReconciler) buildEnvVars(envVars []mcpv1alpha1.EnvVar, secretEnvVars []mcpv1alpha1.SecretEnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, 0, len(envVars)+len(secretEnvVars))
	for _, ev := range envVars {
		result = append(result, corev1.EnvVar{
			Name:  ev.Name,
			Value: ev.Value,
		})
	}
	for _, ev := range secretEnvVars {
		if ev.SecretKeyRef == nil {
			continue
		}
		result = append(result, corev1.EnvVar{
			Name: ev.Name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ev.SecretKeyRef.Name},
					Key:                  ev.SecretKeyRef.Key,
				},
			},
		})
	}
	return result
}

func conditionStatus(ready bool) metav1.ConditionStatus {
	if ready {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
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

func gatewayEnabled(mcpServer *mcpv1alpha1.MCPServer) bool {
	return mcpServer != nil && mcpServer.Spec.Gateway != nil && mcpServer.Spec.Gateway.Enabled
}

func analyticsEnabled(mcpServer *mcpv1alpha1.MCPServer) bool {
	return mcpServer != nil && mcpServer.Spec.Analytics != nil && mcpServer.Spec.Analytics.Enabled
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&mcpv1alpha1.MCPAccessGrant{}, handler.EnqueueRequestsFromMapFunc(r.requestsForReferencedServer)).
		Watches(&mcpv1alpha1.MCPAgentSession{}, handler.EnqueueRequestsFromMapFunc(r.requestsForReferencedServer)).
		Complete(r)
}

func (r *MCPServerReconciler) requestsForReferencedServer(_ context.Context, obj client.Object) []ctrl.Request {
	switch resource := obj.(type) {
	case *mcpv1alpha1.MCPAccessGrant:
		namespace := resource.Spec.ServerRef.Namespace
		if namespace == "" {
			namespace = resource.Namespace
		}
		if resource.Spec.ServerRef.Name == "" {
			return nil
		}
		return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: resource.Spec.ServerRef.Name, Namespace: namespace}}}
	case *mcpv1alpha1.MCPAgentSession:
		namespace := resource.Spec.ServerRef.Namespace
		if namespace == "" {
			namespace = resource.Namespace
		}
		if resource.Spec.ServerRef.Name == "" {
			return nil
		}
		return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: resource.Spec.ServerRef.Name, Namespace: namespace}}}
	default:
		return nil
	}
}
