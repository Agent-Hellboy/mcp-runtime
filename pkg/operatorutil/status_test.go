package operatorutil

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetConditionAppendsNewCondition(t *testing.T) {
	t.Parallel()

	var conditions []metav1.Condition
	SetCondition(&conditions, DeploymentReady, true, "Ready", "deployment is ready", 7)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Type != string(DeploymentReady) {
		t.Fatalf("unexpected type: %s", conditions[0].Type)
	}
	if conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("unexpected status: %s", conditions[0].Status)
	}
	if conditions[0].ObservedGeneration != 7 {
		t.Fatalf("unexpected generation: %d", conditions[0].ObservedGeneration)
	}
}

func TestSetConditionPreservesTransitionTimeWhenStatusUnchanged(t *testing.T) {
	t.Parallel()

	originalTime := metav1.NewTime(time.Unix(1_700_000_000, 0).UTC())
	conditions := []metav1.Condition{{
		Type:               string(ServiceReady),
		Status:             metav1.ConditionTrue,
		Reason:             "OldReason",
		Message:            "old message",
		LastTransitionTime: originalTime,
		ObservedGeneration: 1,
	}}

	SetCondition(&conditions, ServiceReady, true, "NewReason", "new message", 2)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if !conditions[0].LastTransitionTime.Equal(&originalTime) {
		t.Fatalf("expected transition time to be preserved: got %v want %v", conditions[0].LastTransitionTime, originalTime)
	}
	if conditions[0].Reason != "NewReason" {
		t.Fatalf("unexpected reason: %s", conditions[0].Reason)
	}
	if conditions[0].ObservedGeneration != 2 {
		t.Fatalf("unexpected generation: %d", conditions[0].ObservedGeneration)
	}
}
