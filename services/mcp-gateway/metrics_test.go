package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPolicyReloadMetrics(t *testing.T) {
	failBefore := testutil.ToFloat64(policyReloadTotal.WithLabelValues("failure"))
	recordPolicyReloadFailure()
	if got := testutil.ToFloat64(policyReloadTotal.WithLabelValues("failure")) - failBefore; got != 1 {
		t.Fatalf("failure counter delta = %v, want 1", got)
	}

	successBefore := testutil.ToFloat64(policyReloadTotal.WithLabelValues("success"))
	at := time.Unix(1_700_000_000, 0)
	recordPolicyReloadSuccess("sha256:revA", "v1", at)
	if got := testutil.ToFloat64(policyReloadTotal.WithLabelValues("success")) - successBefore; got != 1 {
		t.Fatalf("success counter delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(policyLastSuccessTimestamp); got != float64(at.Unix()) {
		t.Fatalf("last success timestamp = %v, want %v", got, at.Unix())
	}
	if got := testutil.ToFloat64(policyActiveRevisionInfo.WithLabelValues("sha256:revA", "v1")); got != 1 {
		t.Fatalf("active revision info = %v, want 1", got)
	}

	// A new active revision replaces the prior series (Reset on success).
	recordPolicyReloadSuccess("sha256:revB", "v1", at)
	if got := testutil.ToFloat64(policyActiveRevisionInfo.WithLabelValues("sha256:revA", "v1")); got != 0 {
		t.Fatalf("stale revision info = %v, want 0 after new revision activated", got)
	}
}
