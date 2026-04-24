package v1alpha1

import (
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	_       webhook.Validator = &MCPServer{}
	_       webhook.Validator = &MCPAccessGrant{}
	_       webhook.Validator = &MCPAgentSession{}
	nowFunc                   = time.Now
)

// +kubebuilder:webhook:path=/validate-mcpruntime-org-v1alpha1-mcpserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=vmcpserver.kb.io,admissionReviewVersions=v1
func (r *MCPServer) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(r).Complete()
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

	if strings.TrimSpace(r.Spec.Image) == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("image"), "image is required"))
	}
	if r.Spec.Gateway != nil && r.Spec.Gateway.Enabled && r.Spec.Gateway.Port == r.Spec.Port {
		allErrs = append(allErrs, field.Invalid(specPath.Child("gateway", "port"), r.Spec.Gateway.Port, "gateway.port must differ from spec.port"))
	}
	if r.Spec.Gateway == nil || !r.Spec.Gateway.Enabled {
		if r.Spec.Analytics != nil && r.Spec.Analytics.Enabled {
			allErrs = append(allErrs, field.Invalid(specPath.Child("analytics", "enabled"), true, "analytics requires gateway.enabled"))
		}
	}
	if strings.TrimSpace(r.Spec.PublicPathPrefix) != "" {
		trimmed := strings.Trim(strings.TrimSpace(r.Spec.PublicPathPrefix), "/")
		if trimmed == "" {
			allErrs = append(allErrs, field.Invalid(specPath.Child("publicPathPrefix"), r.Spec.PublicPathPrefix, "publicPathPrefix must contain at least one non-slash character"))
		}
	}
	if r.Spec.Rollout != nil && r.Spec.Rollout.Strategy == RolloutStrategyCanary {
		if r.Spec.Rollout.CanaryReplicas == nil || *r.Spec.Rollout.CanaryReplicas <= 0 {
			allErrs = append(allErrs, field.Required(specPath.Child("rollout", "canaryReplicas"), "canaryReplicas must be greater than zero for canary strategy"))
		}
		if r.Spec.Replicas != nil && r.Spec.Rollout.CanaryReplicas != nil && *r.Spec.Rollout.CanaryReplicas >= *r.Spec.Replicas {
			allErrs = append(allErrs, field.Invalid(specPath.Child("rollout", "canaryReplicas"), *r.Spec.Rollout.CanaryReplicas, "must be less than spec.replicas"))
		}
	}

	toolNames := make(map[string]struct{}, len(r.Spec.Tools))
	for i, tool := range r.Spec.Tools {
		toolPath := specPath.Child("tools").Index(i)
		if strings.TrimSpace(tool.Name) == "" {
			allErrs = append(allErrs, field.Required(toolPath.Child("name"), "tool name is required"))
			continue
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

// +kubebuilder:webhook:path=/validate-mcpruntime-org-v1alpha1-mcpaccessgrant,mutating=false,failurePolicy=fail,sideEffects=None,groups=mcpruntime.org,resources=mcpaccessgrants,verbs=create;update,versions=v1alpha1,name=vmcpaccessgrant.kb.io,admissionReviewVersions=v1
func (r *MCPAccessGrant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(r).Complete()
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
	return ctrl.NewWebhookManagedBy(mgr).For(r).Complete()
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

func (r *MCPServer) String() string {
	return fmt.Sprintf("%s/%s", r.Namespace, r.Name)
}
