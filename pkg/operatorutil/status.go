// Package operatorutil provides shared utilities for the MCP operator.
package operatorutil

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ConditionType represents the type of status condition.
type ConditionType string

const (
	// DeploymentReady indicates the deployment is ready.
	DeploymentReady ConditionType = "DeploymentReady"
	// ServiceReady indicates the service is ready.
	ServiceReady ConditionType = "ServiceReady"
	// IngressReady indicates the ingress is ready.
	IngressReady ConditionType = "IngressReady"
	// GatewayReady indicates the gateway configuration and sidecar are ready.
	GatewayReady ConditionType = "GatewayReady"
	// PolicyReady indicates policy data for the gateway has been generated.
	PolicyReady ConditionType = "PolicyReady"
	// CanaryReady indicates the canary deployment, when configured, is ready.
	CanaryReady ConditionType = "CanaryReady"
)

// ResourceReadiness tracks the readiness of different resource types.
type ResourceReadiness struct {
	Deployment bool
	Service    bool
	Ingress    bool
	Gateway    bool
	Policy     bool
	Canary     bool
}

// UpdateMCPServerStatus updates the status of an MCPServer object with consistent condition handling.
// It fetches the latest object, updates the status fields and conditions, and applies the update.
func UpdateMCPServerStatus(ctx context.Context, c client.Client, obj client.Object, phase, message string, readiness ResourceReadiness) error {
	logger := log.FromContext(ctx)

	// Re-fetch the object to get the latest resourceVersion
	key := client.ObjectKeyFromObject(obj)
	latest := obj.DeepCopyObject().(client.Object)

	if err := c.Get(ctx, key, latest); err != nil {
		// If object not found, it may have been deleted - skip status update
		if client.IgnoreNotFound(err) == nil {
			logger.V(1).Info("Object not found, skipping status update (may have been deleted)", "key", key)
			return nil
		}
		// For other errors, log but try to update with the original object as fallback
		logger.Error(err, "Failed to fetch object for status update, using original object", "key", key)
		latest = obj
	}

	mcpServer, ok := latest.(*mcpv1alpha1.MCPServer)
	if !ok {
		return fmt.Errorf("unsupported status object type %T", latest)
	}

	// Update status fields
	mcpServer.Status.Phase = phase
	mcpServer.Status.Message = message
	mcpServer.Status.DeploymentReady = readiness.Deployment
	mcpServer.Status.ServiceReady = readiness.Service
	mcpServer.Status.IngressReady = readiness.Ingress
	mcpServer.Status.GatewayReady = readiness.Gateway
	mcpServer.Status.PolicyReady = readiness.Policy
	mcpServer.Status.CanaryReady = readiness.Canary

	// Update conditions
	conditions := &mcpServer.Status.Conditions

	SetCondition(conditions, DeploymentReady, readiness.Deployment, phase, message, latest.GetGeneration())
	SetCondition(conditions, ServiceReady, readiness.Service, phase, message, latest.GetGeneration())
	SetCondition(conditions, IngressReady, readiness.Ingress, phase, message, latest.GetGeneration())
	SetCondition(conditions, GatewayReady, readiness.Gateway, phase, message, latest.GetGeneration())
	SetCondition(conditions, PolicyReady, readiness.Policy, phase, message, latest.GetGeneration())
	SetCondition(conditions, CanaryReady, readiness.Canary, phase, message, latest.GetGeneration())

	// Use Status().Update() which only updates the status subresource
	if err := c.Status().Update(ctx, latest); err != nil {
		// If it's a conflict error, that's expected in concurrent scenarios - log at debug level
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Status update conflict (expected in concurrent reconciles), will retry on next reconcile", "key", key)
		} else {
			logger.Error(err, "Failed to update status", "key", key)
		}
		return err
	}

	return nil
}

// SetCondition sets a condition in the conditions slice.
// It updates an existing condition if present, or appends a new one.
// LastTransitionTime is only updated when the condition's Status actually flips.
func SetCondition(conditions *[]metav1.Condition, condType ConditionType, ready bool, reason, message string, generation int64) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}

	newCond := metav1.Condition{
		Type:               string(condType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: generation,
	}

	// Find existing condition
	existingIdx := -1
	for i, c := range *conditions {
		if c.Type == string(condType) {
			existingIdx = i
			break
		}
	}

	if existingIdx >= 0 {
		// Preserve LastTransitionTime if Status hasn't changed
		existingCond := (*conditions)[existingIdx]
		if existingCond.Status == newCond.Status {
			newCond.LastTransitionTime = existingCond.LastTransitionTime
		}
		(*conditions)[existingIdx] = newCond
	} else {
		*conditions = append(*conditions, newCond)
	}
}
