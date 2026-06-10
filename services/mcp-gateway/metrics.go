package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// policyReloadTotal counts policy reload attempts grouped by result so
	// operators can alert on a rising failure rate.
	policyReloadTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_policy_reload_total",
		Help: "Total gateway policy reload attempts grouped by result (success or failure).",
	}, []string{"result"})

	// policyActiveRevisionInfo exposes the active policy revision and schema
	// version as labels; the value is always 1 for the currently active series.
	policyActiveRevisionInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_gateway_policy_active_revision_info",
		Help: "Active gateway policy revision and schema version (value is always 1).",
	}, []string{"revision", "schema_version"})

	// policyLastSuccessTimestamp records when the last valid policy was loaded.
	policyLastSuccessTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_policy_last_success_timestamp_seconds",
		Help: "Unix timestamp (seconds) of the last successful gateway policy load.",
	})
)

func init() {
	prometheus.MustRegister(policyReloadTotal, policyActiveRevisionInfo, policyLastSuccessTimestamp)
}

// recordPolicyReloadSuccess records a successful reload and republishes the
// active revision so only the live revision reports value 1.
func recordPolicyReloadSuccess(revision, schemaVersion string, at time.Time) {
	policyReloadTotal.WithLabelValues("success").Inc()
	policyActiveRevisionInfo.Reset()
	policyActiveRevisionInfo.WithLabelValues(revision, schemaVersion).Set(1)
	policyLastSuccessTimestamp.Set(float64(at.Unix()))
}

// recordPolicyReloadFailure records a rejected reload. The active revision and
// last-success timestamp are intentionally left unchanged because the previous
// known-good policy remains active.
func recordPolicyReloadFailure() {
	policyReloadTotal.WithLabelValues("failure").Inc()
}
