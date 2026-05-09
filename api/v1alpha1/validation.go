package v1alpha1

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	_       admission.Defaulter[*MCPServer]       = mcpServerWebhook{}
	_       admission.Validator[*MCPServer]       = mcpServerWebhook{}
	_       admission.Validator[*MCPAccessGrant]  = mcpAccessGrantValidator{}
	_       admission.Validator[*MCPAgentSession] = mcpAgentSessionValidator{}
	nowFunc                                       = time.Now
)

const (
	defaultImageTag          = "latest"
	defaultReplicas          = int32(1)
	defaultPort              = int32(8088)
	defaultServicePort       = int32(80)
	defaultIngressClass      = "traefik"
	defaultGatewayPort       = int32(8091)
	defaultToolRequiredTrust = "low"

	defaultAuthMode            = AuthModeHeader
	defaultAuthHumanIDHeader   = "X-MCP-Human-ID"
	defaultAuthAgentIDHeader   = "X-MCP-Agent-ID"
	defaultAuthSessionIDHeader = "X-MCP-Agent-Session"
	defaultAuthTokenHeader     = "Authorization"

	defaultPolicyMode      = PolicyModeAllowList
	defaultPolicyDecision  = PolicyDecisionDeny
	defaultPolicyEnforceOn = "call_tool"
	defaultPolicyVersion   = "v1"
	defaultSessionStore    = "kubernetes"
	defaultSessionHeader   = "X-MCP-Agent-Session"
	defaultSessionMaxLife  = "24h"
	defaultSessionIdleTime = "1h"
	defaultSessionUpstream = "Authorization"

	defaultAnalyticsEventType    = "mcp.request"
	defaultRolloutStrategy       = RolloutStrategyRollingUpdate
	defaultRolloutMaxUnavailable = "25%"
	defaultRolloutMaxSurge       = "25%"
)

func defaultIngressPathFromName(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "/" + strings.TrimSpace(name) + "/mcp"
}

func defaultPublicPathPrefixFromName(name string) string {
	return strings.TrimSpace(name)
}

func imageHasTagOrDigest(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}

	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	return lastColon > lastSlash
}

func gatewayEnabled(spec MCPServerSpec) bool {
	return spec.Gateway != nil && spec.Gateway.Enabled
}

// +kubebuilder:webhook:path=/mutate-mcpruntime-org-v1alpha1-mcpserver,mutating=true,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=mmcpserver.kb.io,admissionReviewVersions=v1
func (r *MCPServer) Default() {
	if strings.TrimSpace(r.Spec.ImageTag) == "" && !imageHasTagOrDigest(strings.TrimSpace(r.Spec.Image)) {
		r.Spec.ImageTag = defaultImageTag
	}
	if r.Spec.Replicas == nil {
		replicas := defaultReplicas
		r.Spec.Replicas = &replicas
	}
	if r.Spec.Port == 0 {
		r.Spec.Port = defaultPort
	}
	if r.Spec.ServicePort == 0 {
		r.Spec.ServicePort = defaultServicePort
	}
	if strings.TrimSpace(r.Spec.IngressPath) == "" {
		r.Spec.IngressPath = defaultIngressPathFromName(r.Name)
	}
	if strings.TrimSpace(r.Spec.PublicPathPrefix) == "" {
		r.Spec.PublicPathPrefix = defaultPublicPathPrefixFromName(r.Name)
	}
	if strings.TrimSpace(r.Spec.IngressClass) == "" {
		r.Spec.IngressClass = defaultIngressClass
	}

	if gatewayEnabled(r.Spec) {
		if r.Spec.Auth == nil {
			r.Spec.Auth = &AuthConfig{}
		}
		if r.Spec.Policy == nil {
			r.Spec.Policy = &PolicyConfig{}
		}
		if r.Spec.Session == nil {
			r.Spec.Session = &SessionConfig{}
		}
	}

	if r.Spec.Auth != nil {
		if r.Spec.Auth.Mode == "" {
			r.Spec.Auth.Mode = defaultAuthMode
		}
		if strings.TrimSpace(r.Spec.Auth.HumanIDHeader) == "" {
			r.Spec.Auth.HumanIDHeader = defaultAuthHumanIDHeader
		}
		if strings.TrimSpace(r.Spec.Auth.AgentIDHeader) == "" {
			r.Spec.Auth.AgentIDHeader = defaultAuthAgentIDHeader
		}
		if strings.TrimSpace(r.Spec.Auth.SessionIDHeader) == "" {
			r.Spec.Auth.SessionIDHeader = defaultAuthSessionIDHeader
		}
		if strings.TrimSpace(r.Spec.Auth.TokenHeader) == "" {
			r.Spec.Auth.TokenHeader = defaultAuthTokenHeader
		}
	}

	if r.Spec.Policy != nil {
		if strings.TrimSpace(string(r.Spec.Policy.Mode)) == "" {
			r.Spec.Policy.Mode = defaultPolicyMode
		}
		if strings.TrimSpace(string(r.Spec.Policy.DefaultDecision)) == "" {
			r.Spec.Policy.DefaultDecision = defaultPolicyDecision
		}
		if strings.TrimSpace(r.Spec.Policy.EnforceOn) == "" {
			r.Spec.Policy.EnforceOn = defaultPolicyEnforceOn
		}
		if strings.TrimSpace(r.Spec.Policy.PolicyVersion) == "" {
			r.Spec.Policy.PolicyVersion = defaultPolicyVersion
		}
	}

	if r.Spec.Session != nil {
		if strings.TrimSpace(r.Spec.Session.Store) == "" {
			r.Spec.Session.Store = defaultSessionStore
		}
		if strings.TrimSpace(r.Spec.Session.HeaderName) == "" {
			r.Spec.Session.HeaderName = defaultSessionHeader
		}
		if strings.TrimSpace(r.Spec.Session.MaxLifetime) == "" {
			r.Spec.Session.MaxLifetime = defaultSessionMaxLife
		}
		if strings.TrimSpace(r.Spec.Session.IdleTimeout) == "" {
			r.Spec.Session.IdleTimeout = defaultSessionIdleTime
		}
		if strings.TrimSpace(r.Spec.Session.UpstreamTokenHeader) == "" {
			r.Spec.Session.UpstreamTokenHeader = defaultSessionUpstream
		}
	}

	for i := range r.Spec.Tools {
		if strings.TrimSpace(string(r.Spec.Tools[i].RequiredTrust)) == "" {
			r.Spec.Tools[i].RequiredTrust = TrustLevel(defaultToolRequiredTrust)
		}
	}

	if gatewayEnabled(r.Spec) {
		if r.Spec.Gateway.Port == 0 {
			r.Spec.Gateway.Port = defaultGatewayPort
		}
		if strings.TrimSpace(r.Spec.Gateway.UpstreamURL) == "" {
			r.Spec.Gateway.UpstreamURL = fmt.Sprintf("http://127.0.0.1:%d", r.Spec.Port)
		}
	}

	if r.Spec.Analytics != nil && r.Spec.Analytics.Enabled {
		if strings.TrimSpace(r.Spec.Analytics.Source) == "" {
			r.Spec.Analytics.Source = strings.TrimSpace(r.Name)
		}
		if strings.TrimSpace(r.Spec.Analytics.EventType) == "" {
			r.Spec.Analytics.EventType = defaultAnalyticsEventType
		}
	}

	if r.Spec.Rollout != nil {
		if strings.TrimSpace(string(r.Spec.Rollout.Strategy)) == "" {
			r.Spec.Rollout.Strategy = defaultRolloutStrategy
		}
		if strings.TrimSpace(r.Spec.Rollout.MaxUnavailable) == "" {
			r.Spec.Rollout.MaxUnavailable = defaultRolloutMaxUnavailable
		}
		if strings.TrimSpace(r.Spec.Rollout.MaxSurge) == "" {
			r.Spec.Rollout.MaxSurge = defaultRolloutMaxSurge
		}
	}
}

// +kubebuilder:webhook:path=/validate-mcpruntime-org-v1alpha1-mcpserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=vmcpserver.kb.io,admissionReviewVersions=v1
func (r *MCPServer) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).
		WithDefaulter(mcpServerWebhook{}).
		WithValidator(mcpServerWebhook{}).
		Complete()
}

type mcpServerWebhook struct{}

func (mcpServerWebhook) Default(_ context.Context, obj *MCPServer) error {
	obj.Default()
	return nil
}

func (mcpServerWebhook) ValidateCreate(_ context.Context, obj *MCPServer) (admission.Warnings, error) {
	return obj.ValidateCreate()
}

func (mcpServerWebhook) ValidateUpdate(_ context.Context, oldObj *MCPServer, newObj *MCPServer) (admission.Warnings, error) {
	return newObj.ValidateUpdate(oldObj)
}

func (mcpServerWebhook) ValidateDelete(_ context.Context, obj *MCPServer) (admission.Warnings, error) {
	return obj.ValidateDelete()
}

func (r *MCPServer) ValidateCreate() (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPServer) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPServer) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (r *MCPServer) validate() error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")
	publicPathPrefix := strings.TrimSpace(r.Spec.PublicPathPrefix)
	ingressPath := strings.TrimSpace(r.Spec.IngressPath)

	if strings.TrimSpace(r.Spec.Image) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("image"), "image is required"))
	}
	if publicPathPrefix != "" {
		trimmed := strings.Trim(publicPathPrefix, "/")
		if trimmed == "" {
			allErrs = append(allErrs, field.Invalid(specPath.Child("publicPathPrefix"), r.Spec.PublicPathPrefix, "publicPathPrefix must contain at least one non-slash character"))
		}
	}
	if publicPathPrefix == "" {
		if ingressPath == "" {
			allErrs = append(allErrs, field.Required(specPath.Child("ingressPath"), "ingressPath is required when ingressHost is used"))
		}
		if strings.TrimSpace(r.Spec.IngressHost) == "" {
			allErrs = append(allErrs, field.Required(specPath.Child("ingressHost"), "ingressHost is required when publicPathPrefix is not set; set spec.ingressHost or MCP_DEFAULT_INGRESS_HOST on the operator, or use spec.publicPathPrefix for hostless routing"))
		}
	}
	if r.Spec.Gateway != nil && r.Spec.Gateway.Enabled && r.Spec.Gateway.Port == r.Spec.Port {
		allErrs = append(allErrs, field.Invalid(specPath.Child("gateway", "port"), r.Spec.Gateway.Port, "gateway.port must differ from spec.port"))
	}
	if gatewayEnabled(r.Spec) && r.Spec.Auth != nil && r.Spec.Auth.Mode == AuthModeOAuth && strings.TrimSpace(r.Spec.Auth.IssuerURL) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("auth", "issuerURL"), "auth.issuerURL is required when auth.mode is oauth"))
	}
	if r.Spec.Gateway == nil || !r.Spec.Gateway.Enabled {
		if r.Spec.Analytics != nil && r.Spec.Analytics.Enabled {
			allErrs = append(allErrs, field.Invalid(specPath.Child("analytics", "enabled"), true, "analytics requires gateway.enabled"))
		}
	}
	if r.Spec.Rollout != nil && r.Spec.Rollout.Strategy == RolloutStrategyCanary {
		if r.Spec.Rollout.CanaryReplicas == nil || *r.Spec.Rollout.CanaryReplicas <= 0 {
			allErrs = append(allErrs, field.Required(specPath.Child("rollout", "canaryReplicas"), "canaryReplicas must be greater than zero for canary strategy"))
		}
		if r.Spec.Replicas == nil {
			allErrs = append(allErrs, field.Required(specPath.Child("replicas"), "spec.replicas is required when rollout.strategy is Canary"))
		}
		if r.Spec.Replicas != nil && r.Spec.Rollout.CanaryReplicas != nil && *r.Spec.Rollout.CanaryReplicas >= *r.Spec.Replicas {
			allErrs = append(allErrs, field.Invalid(specPath.Child("rollout", "canaryReplicas"), *r.Spec.Rollout.CanaryReplicas, "must be less than spec.replicas"))
		}
	}
	if r.Spec.Rollout != nil {
		if err := validateRolloutValue(specPath.Child("rollout", "maxUnavailable"), r.Spec.Rollout.MaxUnavailable); err != nil {
			allErrs = append(allErrs, err)
		}
		if err := validateRolloutValue(specPath.Child("rollout", "maxSurge"), r.Spec.Rollout.MaxSurge); err != nil {
			allErrs = append(allErrs, err)
		}
	}

	if r.Spec.Analytics != nil && r.Spec.Analytics.Enabled {
		if r.Spec.Analytics.APIKeySecretRef != nil {
			if strings.TrimSpace(r.Spec.Analytics.APIKeySecretRef.Name) == "" {
				allErrs = append(allErrs, field.Required(specPath.Child("analytics", "apiKeySecretRef", "name"), "secret name is required"))
			}
			if strings.TrimSpace(r.Spec.Analytics.APIKeySecretRef.Key) == "" {
				allErrs = append(allErrs, field.Required(specPath.Child("analytics", "apiKeySecretRef", "key"), "secret key is required"))
			}
		}
	}

	toolNames := make(map[string]struct{}, len(r.Spec.Tools))
	for i, tool := range r.Spec.Tools {
		toolPath := specPath.Child("tools").Index(i)
		if strings.TrimSpace(tool.Name) == "" {
			allErrs = append(allErrs, field.Required(toolPath.Child("name"), "tool name is required"))
			continue
		}
		if strings.TrimSpace(string(tool.SideEffect)) == "" {
			allErrs = append(allErrs, field.Required(toolPath.Child("sideEffect"), "tool sideEffect is required"))
		}
		if tool.SideEffect != "" && !validToolSideEffect(tool.SideEffect) {
			allErrs = append(allErrs, field.NotSupported(toolPath.Child("sideEffect"), tool.SideEffect, []string{
				string(ToolSideEffectRead),
				string(ToolSideEffectWrite),
				string(ToolSideEffectDestructive),
			}))
		}
		if _, exists := toolNames[tool.Name]; exists {
			allErrs = append(allErrs, field.Duplicate(toolPath.Child("name"), tool.Name))
		}
		toolNames[tool.Name] = struct{}{}
	}

	for i, envVar := range r.Spec.SecretEnvVars {
		envPath := specPath.Child("secretEnvVars").Index(i)
		if strings.TrimSpace(envVar.Name) == "" {
			allErrs = append(allErrs, field.Required(envPath.Child("name"), "secret env name is required"))
		}
		if envVar.SecretKeyRef == nil {
			allErrs = append(allErrs, field.Required(envPath.Child("secretKeyRef"), "secretKeyRef is required"))
			continue
		}
		if strings.TrimSpace(envVar.SecretKeyRef.Name) == "" {
			allErrs = append(allErrs, field.Required(envPath.Child("secretKeyRef", "name"), "secret name is required"))
		}
		if strings.TrimSpace(envVar.SecretKeyRef.Key) == "" {
			allErrs = append(allErrs, field.Required(envPath.Child("secretKeyRef", "key"), "secret key is required"))
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: "MCPServer"}, r.Name, allErrs)
}

func validateRolloutValue(fieldPath *field.Path, value string) *field.Error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	numeric := strings.TrimSuffix(trimmed, "%")
	if numeric == "" || strings.Contains(numeric, "%") {
		return field.Invalid(fieldPath, trimmed, "rollout value must be an integer or percentage")
	}

	parsed, err := strconv.Atoi(numeric)
	if err != nil || parsed < 0 {
		return field.Invalid(fieldPath, trimmed, "rollout value must be an integer or percentage")
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-mcpruntime-org-v1alpha1-mcpaccessgrant,mutating=false,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpaccessgrants,verbs=create;update,versions=v1alpha1,name=vmcpaccessgrant.kb.io,admissionReviewVersions=v1
func (r *MCPAccessGrant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).
		WithValidator(mcpAccessGrantValidator{}).
		Complete()
}

type mcpAccessGrantValidator struct{}

func (mcpAccessGrantValidator) ValidateCreate(_ context.Context, obj *MCPAccessGrant) (admission.Warnings, error) {
	return obj.ValidateCreate()
}

func (mcpAccessGrantValidator) ValidateUpdate(_ context.Context, oldObj *MCPAccessGrant, newObj *MCPAccessGrant) (admission.Warnings, error) {
	return newObj.ValidateUpdate(oldObj)
}

func (mcpAccessGrantValidator) ValidateDelete(_ context.Context, obj *MCPAccessGrant) (admission.Warnings, error) {
	return obj.ValidateDelete()
}

func (r *MCPAccessGrant) ValidateCreate() (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPAccessGrant) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPAccessGrant) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (r *MCPAccessGrant) validate() error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if strings.TrimSpace(r.Spec.ServerRef.Name) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("serverRef", "name"), "serverRef.name is required"))
	}
	if strings.TrimSpace(r.Spec.Subject.HumanID) == "" && strings.TrimSpace(r.Spec.Subject.AgentID) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("subject"), "either subject.humanID or subject.agentID is required"))
	}

	sideEffects := make(map[ToolSideEffect]struct{}, len(r.Spec.AllowedSideEffects))
	for i, sideEffect := range r.Spec.AllowedSideEffects {
		effectPath := specPath.Child("allowedSideEffects").Index(i)
		if strings.TrimSpace(string(sideEffect)) == "" {
			allErrs = append(allErrs, field.Required(effectPath, "allowed side effect is required"))
			continue
		}
		if !validToolSideEffect(sideEffect) {
			allErrs = append(allErrs, field.NotSupported(effectPath, sideEffect, []string{
				string(ToolSideEffectRead),
				string(ToolSideEffectWrite),
				string(ToolSideEffectDestructive),
			}))
			continue
		}
		if _, exists := sideEffects[sideEffect]; exists {
			allErrs = append(allErrs, field.Duplicate(effectPath, sideEffect))
		}
		sideEffects[sideEffect] = struct{}{}
	}

	toolNames := make(map[string]struct{}, len(r.Spec.ToolRules))
	for i, rule := range r.Spec.ToolRules {
		rulePath := specPath.Child("toolRules").Index(i)
		if strings.TrimSpace(rule.Name) == "" {
			allErrs = append(allErrs, field.Required(rulePath.Child("name"), "tool rule name is required"))
			continue
		}
		if strings.TrimSpace(string(rule.Decision)) == "" {
			allErrs = append(allErrs, field.Required(rulePath.Child("decision"), "tool rule decision is required"))
		}
		if _, exists := toolNames[rule.Name]; exists {
			allErrs = append(allErrs, field.Duplicate(rulePath.Child("name"), rule.Name))
		}
		toolNames[rule.Name] = struct{}{}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: "MCPAccessGrant"}, r.Name, allErrs)
}

// +kubebuilder:webhook:path=/validate-mcpruntime-org-v1alpha1-mcpagentsession,mutating=false,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpagentsessions,verbs=create;update,versions=v1alpha1,name=vmcpagentsession.kb.io,admissionReviewVersions=v1
func (r *MCPAgentSession) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).
		WithValidator(mcpAgentSessionValidator{}).
		Complete()
}

type mcpAgentSessionValidator struct{}

func (mcpAgentSessionValidator) ValidateCreate(_ context.Context, obj *MCPAgentSession) (admission.Warnings, error) {
	return obj.ValidateCreate()
}

func (mcpAgentSessionValidator) ValidateUpdate(_ context.Context, oldObj *MCPAgentSession, newObj *MCPAgentSession) (admission.Warnings, error) {
	return newObj.ValidateUpdate(oldObj)
}

func (mcpAgentSessionValidator) ValidateDelete(_ context.Context, obj *MCPAgentSession) (admission.Warnings, error) {
	return obj.ValidateDelete()
}

func (r *MCPAgentSession) ValidateCreate() (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPAgentSession) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	return nil, r.validate()
}

func (r *MCPAgentSession) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (r *MCPAgentSession) validate() error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if strings.TrimSpace(r.Spec.ServerRef.Name) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("serverRef", "name"), "serverRef.name is required"))
	}
	if strings.TrimSpace(r.Spec.Subject.HumanID) == "" && strings.TrimSpace(r.Spec.Subject.AgentID) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("subject"), "either subject.humanID or subject.agentID is required"))
	}
	now := nowFunc().UTC()
	if r.Spec.ExpiresAt != nil && !r.Spec.ExpiresAt.Time.After(now) {
		allErrs = append(allErrs, field.Invalid(specPath.Child("expiresAt"), r.Spec.ExpiresAt.Time.Format(time.RFC3339), "expiresAt must be in the future"))
	}
	if ref := r.Spec.UpstreamTokenSecretRef; ref != nil {
		if strings.TrimSpace(ref.Name) == "" {
			allErrs = append(allErrs, field.Required(specPath.Child("upstreamTokenSecretRef", "name"), "secret name is required"))
		}
		if strings.TrimSpace(ref.Key) == "" {
			allErrs = append(allErrs, field.Required(specPath.Child("upstreamTokenSecretRef", "key"), "secret key is required"))
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: "MCPAgentSession"}, r.Name, allErrs)
}

func validToolSideEffect(sideEffect ToolSideEffect) bool {
	switch sideEffect {
	case ToolSideEffectRead, ToolSideEffectWrite, ToolSideEffectDestructive:
		return true
	default:
		return false
	}
}

func (r *MCPServer) String() string {
	return fmt.Sprintf("%s/%s", r.Namespace, r.Name)
}
