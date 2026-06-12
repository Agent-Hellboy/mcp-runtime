package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	policypkg "mcp-runtime/pkg/policy"
)

type gatewayMetrics struct {
	requestsTotal          *prometheus.CounterVec
	policyDecisionsTotal   *prometheus.CounterVec
	requestDurationSeconds *prometheus.HistogramVec
	inflightRequests       *prometheus.GaugeVec
	requestBytesTotal      *prometheus.CounterVec
	responseBytesTotal     *prometheus.CounterVec
	policyReloadsTotal     *prometheus.CounterVec
	policyLastReload       *prometheus.GaugeVec
}

type gatewayMetricScope struct {
	Namespace string
	Server    string
	Cluster   string
	TeamID    string
}

func newGatewayMetrics(registerer prometheus.Registerer) *gatewayMetrics {
	m := &gatewayMetrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_requests_total",
			Help: "Total HTTP requests handled by MCP gateway sidecars.",
		}, []string{"namespace", "server", "cluster", "team_id", "method", "rpc_method", "decision", "status"}),
		policyDecisionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_policy_decisions_total",
			Help: "Total policy decisions made by MCP gateway sidecars.",
		}, []string{"namespace", "server", "cluster", "team_id", "decision", "reason", "rpc_method"}),
		requestDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_gateway_request_duration_seconds",
			Help:    "End-to-end request duration observed by MCP gateway sidecars.",
			Buckets: prometheus.DefBuckets,
		}, []string{"namespace", "server", "cluster", "team_id", "method", "rpc_method", "decision", "status"}),
		inflightRequests: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "mcp_gateway_inflight_requests",
			Help: "Current in-flight HTTP requests handled by MCP gateway sidecars.",
		}, []string{"namespace", "server", "cluster", "team_id"}),
		requestBytesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_request_bytes_total",
			Help: "Total request body bytes observed by MCP gateway sidecars.",
		}, []string{"namespace", "server", "cluster", "team_id", "method", "rpc_method", "decision", "status"}),
		responseBytesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_response_bytes_total",
			Help: "Total response body bytes written by MCP gateway sidecars.",
		}, []string{"namespace", "server", "cluster", "team_id", "method", "rpc_method", "decision", "status"}),
		policyReloadsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_policy_reloads_total",
			Help: "Total gateway policy reload attempts.",
		}, []string{"namespace", "server", "cluster", "team_id", "result"}),
		policyLastReload: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "mcp_gateway_policy_last_reload_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful gateway policy reload.",
		}, []string{"namespace", "server", "cluster", "team_id"}),
	}
	if registerer != nil {
		registerer.MustRegister(
			m.requestsTotal,
			m.policyDecisionsTotal,
			m.requestDurationSeconds,
			m.inflightRequests,
			m.requestBytesTotal,
			m.responseBytesTotal,
			m.policyReloadsTotal,
			m.policyLastReload,
		)
	}
	return m
}

func (s *gatewayServer) metricScope(policy *policypkg.Document) gatewayMetricScope {
	return gatewayMetricScope{
		Namespace: policypkg.FirstNonEmpty(policypkg.PolicyServerNamespace(policy), s.serverNamespace),
		Server:    policypkg.FirstNonEmpty(policypkg.PolicyServerName(policy), s.serverName),
		Cluster:   policypkg.FirstNonEmpty(policypkg.PolicyServerCluster(policy), s.clusterName),
		TeamID:    policypkg.PolicyServerTeamID(policy),
	}
}

func (m *gatewayMetrics) trackInflight(scope gatewayMetricScope) func() {
	if m == nil {
		return func() {}
	}
	labels := []string{scope.Namespace, scope.Server, scope.Cluster, scope.TeamID}
	m.inflightRequests.WithLabelValues(labels...).Inc()
	return func() {
		m.inflightRequests.WithLabelValues(labels...).Dec()
	}
}

func (m *gatewayMetrics) recordRequest(
	scope gatewayMetricScope,
	r *http.Request,
	rpcMethod string,
	decision policypkg.Decision,
	status int,
	duration time.Duration,
	requestBytes int64,
	responseBytes int,
) {
	if m == nil || r == nil {
		return
	}
	labels := []string{
		scope.Namespace,
		scope.Server,
		scope.Cluster,
		scope.TeamID,
		metricHTTPMethod(r.Method),
		metricRPCMethod(rpcMethod),
		metricDecision(decision),
		strconv.Itoa(status),
	}
	m.requestsTotal.WithLabelValues(labels...).Inc()
	m.requestDurationSeconds.WithLabelValues(labels...).Observe(duration.Seconds())
	m.requestBytesTotal.WithLabelValues(labels...).Add(float64(maxInt64(requestBytes, 0)))
	m.responseBytesTotal.WithLabelValues(labels...).Add(float64(responseBytes))
}

func (m *gatewayMetrics) recordPolicyDecision(scope gatewayMetricScope, rpcMethod string, decision policypkg.Decision) {
	if m == nil {
		return
	}
	m.policyDecisionsTotal.WithLabelValues(
		scope.Namespace,
		scope.Server,
		scope.Cluster,
		scope.TeamID,
		metricDecision(decision),
		metricReason(decision),
		metricRPCMethod(rpcMethod),
	).Inc()
}

func (m *gatewayMetrics) recordPolicyReload(scope gatewayMetricScope, err error) {
	if m == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	m.policyReloadsTotal.WithLabelValues(scope.Namespace, scope.Server, scope.Cluster, scope.TeamID, result).Inc()
	if err == nil {
		m.policyLastReload.WithLabelValues(scope.Namespace, scope.Server, scope.Cluster, scope.TeamID).Set(float64(time.Now().Unix()))
	}
}

func metricDecision(decision policypkg.Decision) string {
	if decision.Allowed {
		return "allow"
	}
	return "deny"
}

func metricReason(decision policypkg.Decision) string {
	if value := strings.TrimSpace(decision.Reason); value != "" {
		return value
	}
	if decision.Allowed {
		return "allowed"
	}
	return "denied"
}

func metricHTTPMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return strings.ToUpper(strings.TrimSpace(method))
	default:
		return "OTHER"
	}
}

func metricRPCMethod(method string) string {
	switch strings.TrimSpace(method) {
	case "":
		return "none"
	case "initialize",
		"notifications/initialized",
		"ping",
		"tools/list",
		"tools/call",
		"resources/list",
		"resources/read",
		"resources/subscribe",
		"resources/unsubscribe",
		"prompts/list",
		"prompts/get",
		"completion/complete",
		"logging/setLevel":
		return strings.TrimSpace(method)
	default:
		return "other"
	}
}
