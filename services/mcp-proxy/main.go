package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	policypkg "mcp-runtime/pkg/policy"
	"mcp-runtime/pkg/serviceutil"
)

type analyticsEvent struct {
	Timestamp string         `json:"timestamp"`
	Source    string         `json:"source"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type toolParams struct {
	Name string `json:"name"`
}

type identityContext struct {
	HumanID   string
	AgentID   string
	SessionID string
}

type authzDecision struct {
	Allowed        bool
	Status         int
	Reason         string
	PolicyVersion  string
	RequiredTrust  string
	AdminTrust     string
	ConsentedTrust string
	EffectiveTrust string
}

type policySnapshot struct {
	Policy *policypkg.Document
	Err    error
}

type rpcInspection struct {
	Method        string
	ToolName      string
	ToolCall      bool
	Indeterminate bool
	FailureReason string
}

type oauthProvider struct {
	jwks *keyfunc.JWKS
}

type authServerMetadata struct {
	JWKSURI string `json:"jwks_uri"`
}

type oauthAuthResult struct {
	Allowed  bool
	Status   int
	Reason   string
	Identity identityContext
	Token    string
}

type proxyServer struct {
	proxy                 *httputil.ReverseProxy
	analyticsURL          string
	apiKey                string
	source                string
	eventType             string
	analyticsQueue        chan analyticsEvent
	stripPrefix           string
	externalBaseURL       *url.URL
	httpClient            *http.Client
	policyFile            string
	serverName            string
	serverNamespace       string
	clusterName           string
	defaultHumanHeader    string
	defaultAgentHeader    string
	defaultSessionHeader  string
	defaultPolicyMode     string
	defaultPolicyDecision string
	defaultPolicyVersion  string
	analyticsCancel       context.CancelFunc
	analyticsOnce         sync.Once
	oauthMu               sync.Mutex
	oauthProviders        map[string]*oauthProvider
	policyState           atomic.Value
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

const (
	maxRPCBodyBytes       = 1 << 20
	analyticsQueueSize    = 256
	analyticsWorkerCount  = 4
	defaultHumanHeader    = "X-MCP-Human-ID"
	defaultAgentHeader    = "X-MCP-Agent-ID"
	defaultSessionHeader  = "X-MCP-Agent-Session"
	defaultPolicyMode     = "allow-list"
	defaultPolicyDecision = "deny"
	defaultPolicyVersion  = "v1"
	oauthProtectedPrefix  = "/.well-known/oauth-protected-resource"
	defaultTokenHeader    = "Authorization"
)

// main initializes and starts the MCP Proxy service.
// It acts as a reverse proxy for MCP servers while enforcing simple policy and capturing analytics.
func main() {
	port := serviceutil.EnvOr("PORT", "8091")
	upstream := serviceutil.EnvOr("UPSTREAM_URL", "http://127.0.0.1:8090")
	analyticsURL := strings.TrimSpace(os.Getenv("ANALYTICS_INGEST_URL"))
	apiKey := strings.TrimSpace(os.Getenv("ANALYTICS_API_KEY"))
	source := serviceutil.EnvOr("ANALYTICS_SOURCE", "mcp-proxy")
	eventType := serviceutil.EnvOr("ANALYTICS_EVENT_TYPE", "mcp.request")
	stripPrefix := strings.TrimSpace(os.Getenv("STRIP_PREFIX"))
	externalBaseURL, err := parseExternalBaseURL(strings.TrimSpace(os.Getenv("EXTERNAL_BASE_URL")))
	if err != nil {
		log.Fatalf("invalid EXTERNAL_BASE_URL: %v", err)
	}

	target, err := url.Parse(upstream)
	if err != nil {
		log.Fatalf("invalid UPSTREAM_URL: %v", err)
	}

	proxy := newUpstreamReverseProxy(target)
	proxy.Transport = otelhttp.NewTransport(http.DefaultTransport)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
	}

	analyticsTransport := otelhttp.NewTransport(&http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	})

	sharedClient := &http.Client{
		Timeout:   3 * time.Second,
		Transport: analyticsTransport,
	}

	srv := &proxyServer{
		proxy:                 proxy,
		analyticsURL:          analyticsURL,
		apiKey:                apiKey,
		source:                source,
		eventType:             eventType,
		stripPrefix:           stripPrefix,
		externalBaseURL:       externalBaseURL,
		httpClient:            sharedClient,
		policyFile:            strings.TrimSpace(os.Getenv("POLICY_FILE")),
		serverName:            strings.TrimSpace(os.Getenv("MCP_SERVER_NAME")),
		serverNamespace:       strings.TrimSpace(os.Getenv("MCP_SERVER_NAMESPACE")),
		clusterName:           strings.TrimSpace(os.Getenv("MCP_CLUSTER_NAME")),
		defaultHumanHeader:    serviceutil.EnvOr("HUMAN_ID_HEADER", defaultHumanHeader),
		defaultAgentHeader:    serviceutil.EnvOr("AGENT_ID_HEADER", defaultAgentHeader),
		defaultSessionHeader:  serviceutil.EnvOr("SESSION_ID_HEADER", defaultSessionHeader),
		defaultPolicyMode:     serviceutil.EnvOr("POLICY_MODE", defaultPolicyMode),
		defaultPolicyDecision: serviceutil.EnvOr("POLICY_DEFAULT_DECISION", defaultPolicyDecision),
		defaultPolicyVersion:  serviceutil.EnvOr("POLICY_VERSION", defaultPolicyVersion),
		oauthProviders:        map[string]*oauthProvider{},
	}
	if err := srv.startPolicyCache(); err != nil {
		log.Fatalf("initial policy load failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", srv.handleProxy)

	shutdown, err := initTracer("mcp-proxy")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	log.Printf("mcp-proxy listening on :%s -> %s", port, upstream)
	handler := otelhttp.NewHandler(mux, "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		srv.stopAnalyticsDispatcher()
		log.Fatalf("server failed: %v", err)
	}
}

func newUpstreamReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}
	return proxy
}

// handleProxy handles incoming MCP requests and forwards them to upstream servers.
// It evaluates simple policy for tool invocations and emits audit events on allow/deny.
func (s *proxyServer) handleProxy(w http.ResponseWriter, r *http.Request) {
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
	decision := authzDecision{
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
			decision = deny(
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
			decision = deny(
				http.StatusServiceUnavailable,
				"policy_unavailable",
				policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(policy), s.defaultPolicyVersion),
			)
		case inspection.Indeterminate:
			decision = deny(
				http.StatusForbidden,
				policypkg.FirstNonEmpty(inspection.FailureReason, "rpc_inspection_failed"),
				policypkg.ChoosePolicyVersion(policypkg.PolicyVersion(policy), s.defaultPolicyVersion),
			)
		default:
			decision = authorizeRequest(policy, authCtx, rpcMethod, toolName)
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

	s.emitIfEnabled(analyticsEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    s.source,
		EventType: s.eventType,
		Payload: s.auditPayload(
			r,
			originalPath,
			rpcMethod,
			toolName,
			authCtx,
			policy,
			decision,
			recorder.status,
			time.Since(start).Milliseconds(),
			recorder.bytes,
		),
	})
}

func (s *proxyServer) writeDeniedResponse(
	recorder *statusRecorder,
	r *http.Request,
	originalPath, rpcMethod, toolName string,
	authCtx identityContext,
	policy *policypkg.Document,
	decision authzDecision,
	start time.Time,
) {
	recorder.Header().Set("content-type", "application/json")
	if shouldChallengeOAuth(policy, decision) {
		recorder.Header().Set("www-authenticate", s.oauthAuthenticateHeader(r, originalPath, decision.Reason))
	}
	recorder.WriteHeader(decision.Status)
	_ = json.NewEncoder(recorder).Encode(map[string]any{"error": decision.Reason})
	s.emitIfEnabled(analyticsEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    s.source,
		EventType: s.eventType,
		Payload: s.auditPayload(
			r,
			originalPath,
			rpcMethod,
			toolName,
			authCtx,
			policy,
			decision,
			recorder.status,
			time.Since(start).Milliseconds(),
			recorder.bytes,
		),
	})
}

func (s *proxyServer) auditPayload(
	r *http.Request,
	path, rpcMethod, toolName string,
	authCtx identityContext,
	policy *policypkg.Document,
	decision authzDecision,
	status int,
	latencyMs int64,
	bytesOut int,
) map[string]any {
	payload := map[string]any{
		"method":         r.Method,
		"path":           path,
		"status":         status,
		"latency_ms":     latencyMs,
		"bytes_in":       maxInt64(r.ContentLength, 0),
		"bytes_out":      bytesOut,
		"server":         policypkg.FirstNonEmpty(policypkg.PolicyServerName(policy), s.serverName),
		"namespace":      policypkg.FirstNonEmpty(policypkg.PolicyServerNamespace(policy), s.serverNamespace),
		"cluster":        policypkg.FirstNonEmpty(policypkg.PolicyServerCluster(policy), s.clusterName),
		"human_id":       authCtx.HumanID,
		"agent_id":       authCtx.AgentID,
		"session_id":     authCtx.SessionID,
		"decision":       ternary(decision.Allowed, "allow", "deny"),
		"reason":         decision.Reason,
		"policy_version": policypkg.FirstNonEmpty(decision.PolicyVersion, s.defaultPolicyVersion),
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

func (s *proxyServer) startPolicyCache() error {
	s.snapshotPolicy(policySnapshot{Policy: s.defaultPolicyDocument()})
	if err := s.reloadPolicy(); err != nil {
		return err
	}
	if s.policyFile == "" {
		return nil
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.reloadPolicy(); err != nil {
				log.Printf("policy reload failed: %v", err)
			}
		}
	}()

	return nil
}

func (s *proxyServer) reloadPolicy() error {
	doc, err := s.loadPolicy()
	if err != nil {
		current := s.loadPolicySnapshot()
		fallback := current.Policy
		if fallback == nil {
			fallback = s.defaultPolicyDocument()
		}
		s.snapshotPolicy(policySnapshot{Policy: fallback, Err: err})
		return err
	}
	s.snapshotPolicy(policySnapshot{Policy: doc})
	return nil
}

func (s *proxyServer) currentPolicy() (*policypkg.Document, error) {
	snapshot := s.loadPolicySnapshot()
	if snapshot.Policy == nil {
		return s.defaultPolicyDocument(), snapshot.Err
	}
	return snapshot.Policy, snapshot.Err
}

func (s *proxyServer) loadPolicySnapshot() policySnapshot {
	if value := s.policyState.Load(); value != nil {
		return value.(policySnapshot)
	}
	return policySnapshot{Policy: s.defaultPolicyDocument()}
}

func (s *proxyServer) snapshotPolicy(snapshot policySnapshot) {
	s.policyState.Store(snapshot)
}

func (s *proxyServer) loadPolicy() (*policypkg.Document, error) {
	doc := &policypkg.Document{}
	if s.policyFile != "" {
		data, err := os.ReadFile(s.policyFile)
		if err != nil {
			return nil, err
		} else if len(data) > 0 {
			if err := json.Unmarshal(data, doc); err != nil {
				return nil, err
			}
		}
	}

	if doc.Server.Name == "" {
		doc.Server.Name = s.serverName
	}
	if doc.Server.Namespace == "" {
		doc.Server.Namespace = s.serverNamespace
	}
	if doc.Server.Cluster == "" {
		doc.Server.Cluster = s.clusterName
	}
	if doc.Auth.Mode == "" {
		doc.Auth.Mode = "header"
	}
	if doc.Auth.HumanIDHeader == "" {
		doc.Auth.HumanIDHeader = s.defaultHumanHeader
	}
	if doc.Auth.AgentIDHeader == "" {
		doc.Auth.AgentIDHeader = s.defaultAgentHeader
	}
	if doc.Auth.SessionIDHeader == "" {
		doc.Auth.SessionIDHeader = s.defaultSessionHeader
	}
	if strings.EqualFold(doc.Auth.Mode, "oauth") && doc.Auth.TokenHeader == "" {
		doc.Auth.TokenHeader = defaultTokenHeader
	}
	if doc.Policy.Mode == "" {
		doc.Policy.Mode = s.defaultPolicyMode
	}
	if doc.Policy.DefaultDecision == "" {
		doc.Policy.DefaultDecision = s.defaultPolicyDecision
	}
	if doc.Policy.PolicyVersion == "" {
		doc.Policy.PolicyVersion = s.defaultPolicyVersion
	}
	return doc, nil
}

func (s *proxyServer) extractIdentity(r *http.Request, policy *policypkg.Document) identityContext {
	humanHeader, agentHeader, sessionHeader := s.identityHeaderNames(policy)
	return identityContext{
		HumanID:   strings.TrimSpace(r.Header.Get(humanHeader)),
		AgentID:   strings.TrimSpace(r.Header.Get(agentHeader)),
		SessionID: strings.TrimSpace(r.Header.Get(sessionHeader)),
	}
}

func (s *proxyServer) defaultPolicyDocument() *policypkg.Document {
	return &policypkg.Document{
		Server: policypkg.Server{
			Name:      s.serverName,
			Namespace: s.serverNamespace,
			Cluster:   s.clusterName,
		},
		Auth: &policypkg.Auth{
			Mode:            "header",
			HumanIDHeader:   s.defaultHumanHeader,
			AgentIDHeader:   s.defaultAgentHeader,
			SessionIDHeader: s.defaultSessionHeader,
			TokenHeader:     defaultTokenHeader,
		},
		Policy: &policypkg.Config{
			Mode:            s.defaultPolicyMode,
			DefaultDecision: s.defaultPolicyDecision,
			PolicyVersion:   s.defaultPolicyVersion,
		},
	}
}

func (s *proxyServer) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request, policy *policypkg.Document) bool {
	if !isOAuthProtectedMetadataPath(r.URL.Path) {
		return false
	}
	if !policypkg.PolicyUsesOAuth(policy) {
		http.NotFound(w, r)
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}

	resourcePath := oauthResourcePath(r.URL.Path)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return true
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resource":                 s.publicRequestURL(r, resourcePath),
		"authorization_servers":    []string{strings.TrimSpace(policy.Auth.IssuerURL)},
		"bearer_methods_supported": []string{"header"},
	})
	return true
}

func (s *proxyServer) authenticateOAuth(r *http.Request, policy *policypkg.Document) oauthAuthResult {
	headerIdentity := s.extractIdentity(r, policy)
	result := oauthAuthResult{
		Allowed:  true,
		Status:   http.StatusOK,
		Identity: identityContext{SessionID: headerIdentity.SessionID},
	}
	if !policypkg.PolicyUsesOAuth(policy) {
		result.Identity = headerIdentity
		return result
	}

	if policy.Auth == nil {
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_config_missing",
			Identity: result.Identity,
		}
	}

	issuerURL := strings.TrimSpace(policy.Auth.IssuerURL)
	if issuerURL == "" {
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_issuer_missing",
			Identity: result.Identity,
		}
	}

	tokenHeader := oauthTokenHeader(policy)
	token := extractToken(tokenHeader, r.Header.Get(tokenHeader))
	if token == "" {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "missing_bearer_token",
			Identity: result.Identity,
		}
	}

	provider, err := s.oauthProviderForIssuer(r.Context(), issuerURL)
	if err != nil {
		log.Printf("oauth provider lookup failed for %s: %v", issuerURL, err)
		return oauthAuthResult{
			Status:   http.StatusServiceUnavailable,
			Reason:   "oauth_provider_unavailable",
			Identity: result.Identity,
		}
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}))
	parsed, err := parser.ParseWithClaims(token, claims, provider.jwks.Keyfunc)
	if err != nil || !parsed.Valid {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}
	if !claims.VerifyIssuer(issuerURL, true) {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}
	if audience := strings.TrimSpace(policy.Auth.Audience); audience != "" && !serviceutil.AudienceMatches(claims["aud"], audience) {
		return oauthAuthResult{
			Status:   http.StatusUnauthorized,
			Reason:   "invalid_token",
			Identity: result.Identity,
		}
	}

	return oauthAuthResult{
		Allowed: true,
		Status:  http.StatusOK,
		Token:   token,
		Identity: identityContext{
			HumanID:   stringClaim(claims, "sub"),
			AgentID:   policypkg.FirstNonEmpty(stringClaim(claims, "azp"), stringClaim(claims, "client_id")),
			SessionID: policypkg.FirstNonEmpty(stringClaim(claims, "sid"), headerIdentity.SessionID),
		},
	}
}

func (s *proxyServer) oauthProviderForIssuer(ctx context.Context, issuerURL string) (*oauthProvider, error) {
	issuerURL = strings.TrimSpace(issuerURL)
	if issuerURL == "" {
		return nil, errors.New("issuer URL is required")
	}

	s.oauthMu.Lock()
	provider, ok := s.oauthProviders[issuerURL]
	s.oauthMu.Unlock()
	if ok {
		return provider, nil
	}

	metadata, err := s.fetchAuthServerMetadata(ctx, issuerURL)
	if err != nil {
		return nil, err
	}
	jwks, err := keyfunc.Get(metadata.JWKSURI, keyfunc.Options{RefreshInterval: 10 * time.Minute})
	if err != nil {
		return nil, err
	}

	provider = &oauthProvider{jwks: jwks}
	s.oauthMu.Lock()
	if existing, ok := s.oauthProviders[issuerURL]; ok {
		s.oauthMu.Unlock()
		return existing, nil
	}
	s.oauthProviders[issuerURL] = provider
	s.oauthMu.Unlock()
	return provider, nil
}

func (s *proxyServer) fetchAuthServerMetadata(ctx context.Context, issuerURL string) (*authServerMetadata, error) {
	var lastErr error
	for _, endpoint := range authServerMetadataCandidates(issuerURL) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s returned status %d", endpoint, resp.StatusCode)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		var metadata authServerMetadata
		if err := json.Unmarshal(body, &metadata); err != nil {
			lastErr = err
			continue
		}
		if strings.TrimSpace(metadata.JWKSURI) == "" {
			lastErr = fmt.Errorf("%s missing jwks_uri", endpoint)
			continue
		}
		return &metadata, nil
	}
	if lastErr == nil {
		lastErr = errors.New("authorization server metadata lookup failed")
	}
	return nil, lastErr
}

func (s *proxyServer) applyIdentityHeaders(r *http.Request, policy *policypkg.Document, identity identityContext) {
	humanHeader, agentHeader, sessionHeader := s.identityHeaderNames(policy)
	if humanHeader != "" {
		r.Header.Del(humanHeader)
		if identity.HumanID != "" {
			r.Header.Set(humanHeader, identity.HumanID)
		}
	}
	if agentHeader != "" {
		r.Header.Del(agentHeader)
		if identity.AgentID != "" {
			r.Header.Set(agentHeader, identity.AgentID)
		}
	}
	if sessionHeader != "" {
		r.Header.Del(sessionHeader)
		if identity.SessionID != "" {
			r.Header.Set(sessionHeader, identity.SessionID)
		}
	}
}

func (s *proxyServer) applyUpstreamToken(r *http.Request, policy *policypkg.Document, token string) {
	if policy == nil || policy.Session == nil {
		return
	}
	headerName := strings.TrimSpace(policy.Session.UpstreamTokenHeader)
	if headerName == "" {
		return
	}
	r.Header.Del(headerName)
	if token == "" {
		return
	}
	r.Header.Set(headerName, serviceutil.FormatTokenHeaderValue(headerName, token))
}

func (s *proxyServer) identityHeaderNames(policy *policypkg.Document) (string, string, string) {
	humanHeader := s.defaultHumanHeader
	agentHeader := s.defaultAgentHeader
	sessionHeader := s.defaultSessionHeader
	if policy != nil && policy.Auth != nil {
		if policy.Auth.HumanIDHeader != "" {
			humanHeader = policy.Auth.HumanIDHeader
		}
		if policy.Auth.AgentIDHeader != "" {
			agentHeader = policy.Auth.AgentIDHeader
		}
		if policy.Auth.SessionIDHeader != "" {
			sessionHeader = policy.Auth.SessionIDHeader
		}
	}
	return humanHeader, agentHeader, sessionHeader
}

func authorizeRequest(policy *policypkg.Document, identity identityContext, rpcMethod, toolName string) authzDecision {
	decision := authzDecision{
		Allowed:       true,
		Status:        http.StatusOK,
		Reason:        "allowed",
		PolicyVersion: policyVersionOrDefault(policy, ""),
	}
	if !policypkg.IsToolCallMethod(rpcMethod) {
		return decision
	}
	if policyModeObserve(policy) {
		return decision
	}
	if identity.HumanID == "" && identity.AgentID == "" {
		return deny(http.StatusUnauthorized, "missing_identity", policyVersionOrDefault(policy, ""))
	}
	if sessionRequired(policy) && identity.SessionID == "" {
		return deny(http.StatusUnauthorized, "missing_session", policyVersionOrDefault(policy, ""))
	}

	session, sessionFound := findSession(policy.Sessions, identity)
	if sessionRequired(policy) {
		if !sessionFound {
			return deny(http.StatusUnauthorized, "session_not_found", policyVersionOrDefault(policy, ""))
		}
		if session.Revoked {
			return deny(http.StatusUnauthorized, "session_revoked", policypkg.ChoosePolicyVersion(session.PolicyVersion, policyVersionOrDefault(policy, "")))
		}
		if isExpired(session.ExpiresAt) {
			return deny(http.StatusUnauthorized, "session_expired", policypkg.ChoosePolicyVersion(session.PolicyVersion, policyVersionOrDefault(policy, "")))
		}
	} else if identity.SessionID == "" || !sessionFound || session.Revoked || isExpired(session.ExpiresAt) {
		session = policypkg.Binding{}
		sessionFound = false
	}

	requiredTrust := resolveToolTrust(policy.Tools, toolName)
	requiredRank := policypkg.TrustRank(requiredTrust)

	matchingGrants := matchingGrants(policy.Grants, identity)
	if len(matchingGrants) == 0 {
		return decideByDefault(policy, "no_matching_grant")
	}

	bestAdminRank := 0
	toolAllowed := false
	policyVersion := policyVersionOrDefault(policy, "")
	for _, grant := range matchingGrants {
		if grant.Disabled {
			continue
		}
		if grant.PolicyVersion != "" {
			policyVersion = grant.PolicyVersion
		}
		adminRank := policypkg.TrustRank(grant.MaxTrust)
		if len(grant.ToolRules) == 0 {
			toolAllowed = true
			if adminRank > bestAdminRank {
				bestAdminRank = adminRank
			}
			continue
		}
		for _, rule := range grant.ToolRules {
			if rule.Name != toolName {
				continue
			}
			if strings.EqualFold(rule.Decision, "deny") {
				return deny(http.StatusForbidden, "tool_denied", policypkg.ChoosePolicyVersion(grant.PolicyVersion, policyVersionOrDefault(policy, "")))
			}
			toolAllowed = true
			ruleRank := policypkg.TrustRank(rule.RequiredTrust)
			if ruleRank > requiredRank {
				requiredRank = ruleRank
				requiredTrust = policypkg.NormalizeTrust(rule.RequiredTrust)
			}
			if adminRank > bestAdminRank {
				bestAdminRank = adminRank
			}
		}
	}

	if !toolAllowed {
		return decideByDefault(policy, "tool_not_granted")
	}
	if bestAdminRank == 0 {
		return decideByDefault(policy, "grant_without_trust")
	}

	consentedRank := bestAdminRank
	consentedTrust := policypkg.RankToTrust(bestAdminRank)
	if sessionFound && session.ConsentedTrust != "" {
		consentedRank = policypkg.TrustRank(session.ConsentedTrust)
		consentedTrust = policypkg.RankToTrust(consentedRank)
	}
	effectiveRank := minInt(bestAdminRank, consentedRank)
	if effectiveRank < requiredRank {
		return authzDecision{
			Status:         http.StatusForbidden,
			Reason:         "trust_too_low",
			PolicyVersion:  policyVersion,
			RequiredTrust:  requiredTrust,
			AdminTrust:     policypkg.RankToTrust(bestAdminRank),
			ConsentedTrust: consentedTrust,
			EffectiveTrust: policypkg.RankToTrust(effectiveRank),
		}
	}

	return authzDecision{
		Allowed:        true,
		Status:         http.StatusOK,
		Reason:         "allowed",
		PolicyVersion:  policyVersion,
		RequiredTrust:  requiredTrust,
		AdminTrust:     policypkg.RankToTrust(bestAdminRank),
		ConsentedTrust: consentedTrust,
		EffectiveTrust: policypkg.RankToTrust(effectiveRank),
	}
}

func decideByDefault(policy *policypkg.Document, reason string) authzDecision {
	policyVersion := policyVersionOrDefault(policy, "")
	if defaultDecisionAllow(policy) {
		return authzDecision{
			Allowed:       true,
			Status:        http.StatusOK,
			Reason:        reason,
			PolicyVersion: policyVersion,
		}
	}
	return deny(http.StatusForbidden, reason, policyVersion)
}

func matchingGrants(grants []policypkg.Grant, identity identityContext) []policypkg.Grant {
	var matched []policypkg.Grant
	for _, grant := range grants {
		if subjectMatches(grant.HumanID, grant.AgentID, identity) {
			matched = append(matched, grant)
		}
	}
	return matched
}

func findSession(sessions []policypkg.Binding, identity identityContext) (policypkg.Binding, bool) {
	if identity.SessionID != "" {
		for _, session := range sessions {
			if session.Name == identity.SessionID && subjectMatches(session.HumanID, session.AgentID, identity) {
				return session, true
			}
		}
		return policypkg.Binding{}, false
	}
	for _, session := range sessions {
		if subjectMatches(session.HumanID, session.AgentID, identity) {
			return session, true
		}
	}
	return policypkg.Binding{}, false
}

func subjectMatches(humanID, agentID string, identity identityContext) bool {
	if humanID != "" && humanID != identity.HumanID {
		return false
	}
	if agentID != "" && agentID != identity.AgentID {
		return false
	}
	return humanID != "" || agentID != ""
}

func resolveToolTrust(tools []policypkg.Tool, toolName string) string {
	for _, tool := range tools {
		if tool.Name == toolName && tool.RequiredTrust != "" {
			return policypkg.NormalizeTrust(tool.RequiredTrust)
		}
	}
	return "low"
}

func isOAuthProtectedMetadataPath(value string) bool {
	return value == oauthProtectedPrefix || strings.HasPrefix(value, oauthProtectedPrefix+"/")
}

func oauthResourcePath(value string) string {
	if !isOAuthProtectedMetadataPath(value) {
		return "/"
	}
	suffix := strings.TrimPrefix(value, oauthProtectedPrefix)
	if suffix == "" {
		return "/"
	}
	return normalizeURLPath(suffix)
}

func oauthMetadataPath(value string) string {
	value = normalizeURLPath(value)
	if value == "/" {
		return oauthProtectedPrefix
	}
	return oauthProtectedPrefix + value
}

func oauthTokenHeader(policy *policypkg.Document) string {
	if policy != nil && policy.Auth != nil && strings.TrimSpace(policy.Auth.TokenHeader) != "" {
		return strings.TrimSpace(policy.Auth.TokenHeader)
	}
	return defaultTokenHeader
}

// policyVersionOrDefault safely retrieves the policy version with a default
func policyVersionOrDefault(policy *policypkg.Document, def string) string {
	if policy != nil && policy.Policy != nil && policy.Policy.PolicyVersion != "" {
		return policy.Policy.PolicyVersion
	}
	return def
}

// sessionRequired safely checks if session is required
func sessionRequired(policy *policypkg.Document) bool {
	return policy != nil && policy.Session != nil && policy.Session.Required
}

// policyModeObserve safely checks if policy mode is observe
func policyModeObserve(policy *policypkg.Document) bool {
	return policy != nil && policy.Policy != nil && strings.EqualFold(policy.Policy.Mode, "observe")
}

// defaultDecisionAllow safely checks if default decision is allow
func defaultDecisionAllow(policy *policypkg.Document) bool {
	return policy != nil && policy.Policy != nil && strings.EqualFold(policy.Policy.DefaultDecision, "allow")
}

func shouldChallengeOAuth(policy *policypkg.Document, decision authzDecision) bool {
	if !policypkg.PolicyUsesOAuth(policy) || decision.Status != http.StatusUnauthorized {
		return false
	}
	switch decision.Reason {
	case "missing_bearer_token", "invalid_token":
		return true
	default:
		return false
	}
}

func (s *proxyServer) oauthAuthenticateHeader(r *http.Request, originalPath, reason string) string {
	values := []string{
		`realm="mcp-runtime"`,
		fmt.Sprintf(`resource_metadata="%s"`, s.publicRequestURL(r, oauthMetadataPath(originalPath))),
	}
	if reason == "invalid_token" {
		values = append(values, `error="invalid_token"`)
	}
	return "Bearer " + strings.Join(values, ", ")
}

func deny(status int, reason, policyVersion string) authzDecision {
	return authzDecision{
		Status:        status,
		Reason:        reason,
		PolicyVersion: policyVersion,
	}
}

func isExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return true
	}
	return time.Now().After(expiresAt)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func stringClaim(claims jwt.MapClaims, key string) string {
	if raw, ok := claims[key]; ok {
		if value, ok := raw.(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractToken(headerName, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(headerName), "authorization") {
		return serviceutil.ExtractBearer(value)
	}
	if token := serviceutil.ExtractBearer(value); token != "" {
		return token
	}
	return value
}

func authServerMetadataCandidates(issuerURL string) []string {
	issuerURL = strings.TrimSpace(issuerURL)
	if issuerURL == "" {
		return nil
	}

	var candidates []string
	addCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	trimmed := strings.TrimRight(issuerURL, "/")
	addCandidate(trimmed + "/.well-known/oauth-authorization-server")
	addCandidate(trimmed + "/.well-known/openid-configuration")

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return candidates
	}
	issuerPath := strings.Trim(parsed.EscapedPath(), "/")
	if issuerPath == "" {
		return candidates
	}
	base := url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	addCandidate(base.String() + "/.well-known/oauth-authorization-server/" + issuerPath)
	addCandidate(base.String() + "/.well-known/openid-configuration/" + issuerPath)
	return candidates
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

func (s *proxyServer) publicRequestURL(r *http.Request, requestPath string) string {
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

func (s *proxyServer) startAnalyticsDispatcher() {
	if s.analyticsURL == "" {
		return
	}
	s.analyticsOnce.Do(func() {
		if s.analyticsQueue == nil {
			s.analyticsQueue = make(chan analyticsEvent, analyticsQueueSize)
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.analyticsCancel = cancel
		for i := 0; i < analyticsWorkerCount; i++ {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case event, ok := <-s.analyticsQueue:
						if !ok {
							return
						}
						s.emit(ctx, event)
					}
				}
			}()
		}
	})
}

func (s *proxyServer) stopAnalyticsDispatcher() {
	if s.analyticsCancel != nil {
		s.analyticsCancel()
	}
}

func (s *proxyServer) emitIfEnabled(event analyticsEvent) {
	if s.analyticsURL == "" {
		return
	}
	if s.analyticsQueue == nil {
		s.startAnalyticsDispatcher()
	}
	if s.analyticsQueue == nil {
		return
	}
	select {
	case s.analyticsQueue <- event:
	default:
	}
}

// emit sends analytics events to the ingest service.
func (s *proxyServer) emit(ctx context.Context, event analyticsEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.analyticsURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("content-type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("failed to emit proxy analytics event to %s: %v", s.analyticsURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("proxy analytics emission failed with status %d to %s", resp.StatusCode, s.analyticsURL)
		_, _ = io.Copy(io.Discard, resp.Body)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}

// WriteHeader records the HTTP response status code.
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

// inspectRPCRequest extracts MCP RPC details or marks a POST request as indeterminate.
func inspectRPCRequest(r *http.Request) rpcInspection {
	if r.Method != http.MethodPost {
		return rpcInspection{}
	}
	contentType := strings.ToLower(r.Header.Get("content-type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}
	if r.Body == nil || r.ContentLength == 0 || r.ContentLength > maxRPCBodyBytes {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRPCBodyBytes+1))
	if err != nil {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}
	if len(body) == 0 || len(body) > maxRPCBodyBytes {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}
	if strings.TrimSpace(req.Method) == "" {
		return rpcInspection{Indeterminate: true, FailureReason: "rpc_inspection_failed"}
	}

	var toolName string
	if len(req.Params) > 0 {
		var params toolParams
		if err := json.Unmarshal(req.Params, &params); err == nil {
			toolName = params.Name
		}
	}

	return rpcInspection{
		Method:   req.Method,
		ToolName: toolName,
		ToolCall: policypkg.IsToolCallMethod(req.Method),
	}
}

// maxInt64 returns the maximum of two int64 values.
func maxInt64(value, fallback int64) int64 {
	if value < 0 {
		return fallback
	}
	return value
}

// initTracer initializes OpenTelemetry tracing for the service.
func initTracer(serviceName string) (func(context.Context) error, error) {
	if envName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); envName != "" {
		serviceName = envName
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	opts := serviceutil.OTLPTraceOptions(endpoint)
	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
