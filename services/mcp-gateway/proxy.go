package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"mcp-runtime/pkg/events"
	policypkg "mcp-runtime/pkg/policy"
)

func newUpstreamReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.Host = target.Host
			req.SetXForwarded()
		},
	}
	return proxy
}

// handleGateway handles incoming MCP requests and forwards them to upstream servers.
// It evaluates simple policy for tool invocations and emits audit events on allow/deny.
func (s *gatewayServer) handleGateway(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	originalPath := r.URL.Path
	inspection := inspectRPCRequest(r)
	rpcMethod, toolName := inspection.Method, inspection.ToolName

	policy, policyErr := s.currentPolicy()
	if s.handleOAuthProtectedResource(recorder, r, policy) {
		return
	}

	authCtx := s.extractIdentity(r, policy)
	decision := policypkg.Decision{
		Allowed:       true,
		Status:        http.StatusOK,
		Reason:        "allowed",
		PolicyVersion: s.defaultPolicyVersion,
	}
	oauthResult := oauthAuthResult{
		Allowed:  true,
		Status:   http.StatusOK,
		Identity: authCtx,
	}

	if policypkg.PolicyUsesOAuth(policy) {
		oauthResult = s.authenticateOAuth(r, policy)
		authCtx = oauthResult.Identity
		if !oauthResult.Allowed {
			decision = policypkg.Deny(
				oauthResult.Status,
				oauthResult.Reason,
				policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(policy), s.defaultPolicyVersion),
			)
			s.writeDeniedResponse(recorder, r, originalPath, rpcMethod, toolName, authCtx, policy, decision, start)
			return
		}
	}

	if inspection.ToolCall || inspection.Indeterminate {
		switch {
		case policyErr != nil:
			decision = policypkg.Deny(
				http.StatusServiceUnavailable,
				"policy_unavailable",
				policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(policy), s.defaultPolicyVersion),
			)
		case inspection.Indeterminate:
			decision = policypkg.Deny(
				http.StatusForbidden,
				policypkg.FirstNonEmpty(inspection.FailureReason, "rpc_inspection_failed"),
				policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(policy), s.defaultPolicyVersion),
			)
		default:
			decision = policypkg.Authorize(policy, policypkg.Request{
				Identity:  policyIdentity(authCtx),
				RPCMethod: rpcMethod,
				ToolName:  toolName,
			}, time.Now())
		}
	}

	if !decision.Allowed {
		s.writeDeniedResponse(recorder, r, originalPath, rpcMethod, toolName, authCtx, policy, decision, start)
		return
	}

	s.applyIdentityHeaders(r, policy, authCtx)
	s.applyUpstreamToken(r, policy, oauthResult.Token)

	if trimmedPath, ok := trimRequestPathPrefix(r.URL.Path, s.stripPrefix); ok {
		r.URL.Path = trimmedPath
		if trimmedRawPath, rawPathTrimmed := trimRequestPathPrefix(r.URL.RawPath, s.stripPrefix); rawPathTrimmed {
			r.URL.RawPath = trimmedRawPath
		}
		if r.URL.Path == "" {
			r.URL.Path = "/"
			if r.URL.RawPath != "" {
				r.URL.RawPath = "/"
			}
		}
	}

	s.proxy.ServeHTTP(recorder, r)

	if decision.PolicyVersion == "" {
		decision.PolicyVersion = s.defaultPolicyVersion
	}

	s.emitAuditEvent(r, originalPath, rpcMethod, toolName, authCtx, policy, decision, recorder.status, time.Since(start).Milliseconds(), recorder.bytes)
}

func (s *gatewayServer) writeDeniedResponse(
	recorder *statusRecorder,
	r *http.Request,
	originalPath, rpcMethod, toolName string,
	authCtx identityContext,
	policy *policypkg.Document,
	decision policypkg.Decision,
	start time.Time,
) {
	recorder.Header().Set("content-type", "application/json")
	if shouldChallengeOAuth(policy, decision) {
		recorder.Header().Set("www-authenticate", s.oauthAuthenticateHeader(r, originalPath, decision.Reason))
	}
	recorder.WriteHeader(decision.Status)
	_ = json.NewEncoder(recorder).Encode(map[string]any{"error": decision.Reason})
	s.emitAuditEvent(r, originalPath, rpcMethod, toolName, authCtx, policy, decision, recorder.status, time.Since(start).Milliseconds(), recorder.bytes)
}

func (s *gatewayServer) emitAuditEvent(
	r *http.Request,
	path, rpcMethod, toolName string,
	authCtx identityContext,
	policy *policypkg.Document,
	decision policypkg.Decision,
	status int,
	latencyMs int64,
	bytesOut int,
) {
	envelope, err := events.NewEnvelope(
		s.source,
		s.eventType,
		s.auditPayload(r, path, rpcMethod, toolName, authCtx, policy, decision, status, latencyMs, bytesOut),
		time.Now().UTC(),
	)
	if err != nil {
		return
	}
	s.emitIfEnabled(r.Context(), envelope)
}

func (s *gatewayServer) auditPayload(
	r *http.Request,
	path, rpcMethod, toolName string,
	authCtx identityContext,
	policy *policypkg.Document,
	decision policypkg.Decision,
	status int,
	latencyMs int64,
	bytesOut int,
) map[string]any {
	payload := map[string]any{
		"method":          r.Method,
		"path":            path,
		"status":          status,
		"latency_ms":      latencyMs,
		"bytes_in":        maxInt64(r.ContentLength, 0),
		"bytes_out":       bytesOut,
		"server":          policypkg.FirstNonEmpty(policypkg.PolicyServerName(policy), s.serverName),
		"namespace":       policypkg.FirstNonEmpty(policypkg.PolicyServerNamespace(policy), s.serverNamespace),
		"team_id":         policypkg.PolicyServerTeamID(policy),
		"cluster":         policypkg.FirstNonEmpty(policypkg.PolicyServerCluster(policy), s.clusterName),
		"human_id":        authCtx.HumanID,
		"agent_id":        authCtx.AgentID,
		"subject_team_id": authCtx.TeamID,
		"session_id":      authCtx.SessionID,
		"decision":        ternary(decision.Allowed, "allow", "deny"),
		"reason":          decision.Reason,
		"policy_version":  policypkg.FirstNonEmpty(decision.PolicyVersion, s.defaultPolicyVersion),
	}
	if rpcMethod != "" {
		payload["rpc_method"] = rpcMethod
	}
	if toolName != "" {
		payload["tool_name"] = toolName
	}
	if decision.RequiredTrust != "" {
		payload["required_trust"] = decision.RequiredTrust
	}
	if decision.RequiredSideEffect != "" {
		payload["required_side_effect"] = decision.RequiredSideEffect
	}
	if decision.AdminTrust != "" {
		payload["admin_trust"] = decision.AdminTrust
	}
	if decision.ConsentedTrust != "" {
		payload["consented_trust"] = decision.ConsentedTrust
	}
	if decision.EffectiveTrust != "" {
		payload["effective_trust"] = decision.EffectiveTrust
	}
	return payload
}
func absoluteRequestURL(r *http.Request, requestPath string) string {
	path := normalizeURLPath(requestPath)
	if r == nil {
		return path
	}

	host := ""
	if strings.TrimSpace(r.Host) != "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" && r.URL != nil && strings.TrimSpace(r.URL.Host) != "" {
		host = strings.TrimSpace(r.URL.Host)
	}
	if host == "" {
		return path
	}

	scheme := "http"
	if r.URL != nil && r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	} else if r.TLS != nil {
		scheme = "https"
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   path,
	}).String()
}

func parseExternalBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("must be an absolute URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (s *gatewayServer) publicRequestURL(r *http.Request, requestPath string) string {
	if s.externalBaseURL != nil {
		return resolveBaseURLPath(s.externalBaseURL, requestPath)
	}
	return absoluteRequestURL(r, requestPath)
}

func resolveBaseURLPath(base *url.URL, requestPath string) string {
	if base == nil {
		return normalizeURLPath(requestPath)
	}
	resolved := *base
	resolved.Path = path.Join(strings.TrimRight(base.Path, "/"), normalizeURLPath(requestPath))
	if !strings.HasPrefix(resolved.Path, "/") {
		resolved.Path = "/" + resolved.Path
	}
	resolved.RawPath = ""
	return resolved.String()
}

func normalizeURLPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func ternary(condition bool, truthy, falsy string) string {
	if condition {
		return truthy
	}
	return falsy
}

func trimRequestPathPrefix(value, prefix string) (string, bool) {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return value, false
	}
	if value != prefix && !strings.HasPrefix(value, prefix+"/") {
		return value, false
	}
	return strings.TrimPrefix(value, prefix), true
}
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write records response data and updates byte count.
func (r *statusRecorder) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

// Flush forwards flush calls to the underlying ResponseWriter.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards hijack calls to the underlying ResponseWriter.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push calls to the underlying ResponseWriter.
func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func maxInt64(value, fallback int64) int64 {
	if value < 0 {
		return fallback
	}
	return value
}
