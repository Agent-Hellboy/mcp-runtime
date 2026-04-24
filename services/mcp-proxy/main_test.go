package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"

	policypkg "mcp-runtime/pkg/policy"
)

func TestHandleProxyOAuthProtectedResourceMetadata(t *testing.T) {
	issuer := newTestJWTIssuer(t)
	upstreamCalled := false
	proxy := newTestProxyServer(t, oauthPolicy(issuer.url), func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.example.com/.well-known/oauth-protected-resource/mcp", nil)
	recorder := httptest.NewRecorder()

	proxy.handleProxy(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if upstreamCalled {
		t.Fatal("metadata request should not reach upstream")
	}

	var payload struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Resource != "http://proxy.example.com/mcp" {
		t.Fatalf("resource = %q, want %q", payload.Resource, "http://proxy.example.com/mcp")
	}
	if len(payload.AuthorizationServers) != 1 || payload.AuthorizationServers[0] != issuer.url {
		t.Fatalf("authorization_servers = %#v, want [%q]", payload.AuthorizationServers, issuer.url)
	}
}

func TestHandleProxyOAuthChallengesWithoutBearer(t *testing.T) {
	issuer := newTestJWTIssuer(t)
	upstreamCalled := false
	proxy := newTestProxyServer(t, oauthPolicy(issuer.url), func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.example.com/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"echo"}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.handleProxy(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if upstreamCalled {
		t.Fatal("unauthenticated request should not reach upstream")
	}
	if got := recorder.Header().Get("Www-Authenticate"); !strings.Contains(got, `resource_metadata="http://proxy.example.com/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("WWW-Authenticate = %q, missing resource metadata URL", got)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload["error"] != "missing_bearer_token" {
		t.Fatalf("error = %q, want %q", payload["error"], "missing_bearer_token")
	}
}

func TestHandleProxyOAuthChallengeUsesExternalBaseURL(t *testing.T) {
	issuer := newTestJWTIssuer(t)
	proxy := newTestProxyServer(t, oauthPolicy(issuer.url), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	externalBaseURL, err := parseExternalBaseURL("https://public.example.com/proxy")
	if err != nil {
		t.Fatalf("parseExternalBaseURL() error = %v", err)
	}
	proxy.externalBaseURL = externalBaseURL

	req := httptest.NewRequest(http.MethodPost, "http://proxy.example.com/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"echo"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Host", "evil.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()

	proxy.handleProxy(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Www-Authenticate"); !strings.Contains(got, `resource_metadata="https://public.example.com/proxy/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("WWW-Authenticate = %q, missing external resource metadata URL", got)
	}
}

func TestHandleProxyOAuthValidatesJWTAndAppliesIdentityHeaders(t *testing.T) {
	issuer := newTestJWTIssuer(t)

	var upstreamHeaders http.Header
	proxy := newTestProxyServer(t, oauthPolicy(issuer.url), func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})

	token := issuer.sign(t, jwt.MapClaims{
		"iss": issuer.url,
		"aud": "mcp-runtime",
		"sub": "human-1",
		"azp": "client-1",
		"sid": "session-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"nbf": time.Now().Add(-time.Minute).Unix(),
	})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.example.com/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"echo"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	proxy.handleProxy(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if upstreamHeaders == nil {
		t.Fatal("expected upstream request")
	}
	if got := upstreamHeaders.Get(defaultHumanHeader); got != "human-1" {
		t.Fatalf("%s = %q, want %q", defaultHumanHeader, got, "human-1")
	}
	if got := upstreamHeaders.Get(defaultAgentHeader); got != "client-1" {
		t.Fatalf("%s = %q, want %q", defaultAgentHeader, got, "client-1")
	}
	if got := upstreamHeaders.Get(defaultSessionHeader); got != "session-1" {
		t.Fatalf("%s = %q, want %q", defaultSessionHeader, got, "session-1")
	}
	if got := upstreamHeaders.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
}

func TestApplyIdentityHeadersClearsSpoofedValues(t *testing.T) {
	t.Parallel()

	proxy := &proxyServer{
		defaultHumanHeader:   defaultHumanHeader,
		defaultAgentHeader:   defaultAgentHeader,
		defaultSessionHeader: defaultSessionHeader,
	}
	req := httptest.NewRequest(http.MethodGet, "http://proxy.example.com/mcp", nil)
	req.Header.Set(defaultHumanHeader, "spoofed-human")
	req.Header.Set(defaultAgentHeader, "spoofed-agent")
	req.Header.Set(defaultSessionHeader, "spoofed-session")

	proxy.applyIdentityHeaders(req, oauthPolicy("https://issuer.example.com"), identityContext{
		HumanID: "human-1",
	})

	if got := req.Header.Get(defaultHumanHeader); got != "human-1" {
		t.Fatalf("%s = %q, want %q", defaultHumanHeader, got, "human-1")
	}
	if got := req.Header.Get(defaultAgentHeader); got != "" {
		t.Fatalf("%s = %q, want empty", defaultAgentHeader, got)
	}
	if got := req.Header.Get(defaultSessionHeader); got != "" {
		t.Fatalf("%s = %q, want empty", defaultSessionHeader, got)
	}
}

func TestApplyUpstreamTokenClearsHeaderWhenTokenMissing(t *testing.T) {
	t.Parallel()

	proxy := &proxyServer{}
	req := httptest.NewRequest(http.MethodGet, "http://proxy.example.com/mcp", nil)
	req.Header.Set("Authorization", "Bearer spoofed-token")

	proxy.applyUpstreamToken(req, oauthPolicy("https://issuer.example.com"), "")

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestHandleProxyRewritesUpstreamHostHeader(t *testing.T) {
	t.Parallel()

	var upstreamHost string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstreamServer.Close)

	target, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	proxy := &proxyServer{
		proxy:                 newUpstreamReverseProxy(target),
		httpClient:            &http.Client{Timeout: 2 * time.Second},
		defaultHumanHeader:    defaultHumanHeader,
		defaultAgentHeader:    defaultAgentHeader,
		defaultSessionHeader:  defaultSessionHeader,
		defaultPolicyMode:     defaultPolicyMode,
		defaultPolicyDecision: defaultPolicyDecision,
		defaultPolicyVersion:  "test-policy",
		oauthProviders:        map[string]*oauthProvider{},
	}

	req := httptest.NewRequest(http.MethodGet, "http://policy.example.local/mcp", nil)
	req.Host = "policy.example.local"
	recorder := httptest.NewRecorder()

	proxy.handleProxy(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if upstreamHost != target.Host {
		t.Fatalf("upstream host = %q, want %q", upstreamHost, target.Host)
	}
}

func TestInspectRPCRequestAcceptsChunkedBody(t *testing.T) {
	t.Parallel()

	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`
	req := httptest.NewRequest(http.MethodPost, "http://proxy.example.com/mcp", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1

	inspection := inspectRPCRequest(req)
	if inspection.Indeterminate {
		t.Fatalf("inspection = %#v, want determinate request", inspection)
	}
	if !inspection.ToolCall {
		t.Fatalf("inspection.ToolCall = %v, want true", inspection.ToolCall)
	}
	if inspection.Method != "tools/call" {
		t.Fatalf("inspection.Method = %q, want %q", inspection.Method, "tools/call")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != payload {
		t.Fatalf("request body = %q, want %q", string(body), payload)
	}
}

func TestAuthorizeRequestOptionalSessionDoesNotApplyWithoutSessionHeader(t *testing.T) {
	t.Parallel()

	policy := &policypkg.Document{
		Policy: &policypkg.Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
		Session: &policypkg.Session{
			Required: false,
		},
		Tools: []policypkg.Tool{
			{Name: "upper", RequiredTrust: "medium"},
		},
		Grants: []policypkg.Grant{
			{
				Name:      "grant-1",
				HumanID:   "human-1",
				AgentID:   "agent-1",
				MaxTrust:  "high",
				ToolRules: []policypkg.ToolAccess{{Name: "upper", Decision: "allow"}},
			},
		},
		Sessions: []policypkg.Binding{
			{
				Name:           "session-1",
				HumanID:        "human-1",
				AgentID:        "agent-1",
				ConsentedTrust: "low",
			},
		},
	}

	decision := authorizeRequest(policy, identityContext{
		HumanID: "human-1",
		AgentID: "agent-1",
	}, "tools/call", "upper")

	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed request", decision)
	}
	if decision.ConsentedTrust != "high" || decision.EffectiveTrust != "high" {
		t.Fatalf("decision = %#v, want optional session ignored without header", decision)
	}
}

func TestAuthorizeRequestOptionalSessionRequiresLiveSessionHeader(t *testing.T) {
	t.Parallel()

	basePolicy := &policypkg.Document{
		Policy: &policypkg.Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
		Session: &policypkg.Session{
			Required: false,
		},
		Tools: []policypkg.Tool{
			{Name: "upper", RequiredTrust: "medium"},
		},
		Grants: []policypkg.Grant{
			{
				Name:      "grant-1",
				HumanID:   "human-1",
				AgentID:   "agent-1",
				MaxTrust:  "high",
				ToolRules: []policypkg.ToolAccess{{Name: "upper", Decision: "allow"}},
			},
		},
	}

	liveSessionPolicy := *basePolicy
	liveSessionPolicy.Sessions = []policypkg.Binding{
		{
			Name:           "session-1",
			HumanID:        "human-1",
			AgentID:        "agent-1",
			ConsentedTrust: "low",
		},
	}

	denyDecision := authorizeRequest(&liveSessionPolicy, identityContext{
		HumanID:   "human-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
	}, "tools/call", "upper")

	if denyDecision.Reason != "trust_too_low" {
		t.Fatalf("deny decision = %#v, want trust_too_low", denyDecision)
	}

	revokedSessionPolicy := *basePolicy
	revokedSessionPolicy.Sessions = []policypkg.Binding{
		{
			Name:           "session-1",
			HumanID:        "human-1",
			AgentID:        "agent-1",
			ConsentedTrust: "low",
			Revoked:        true,
		},
	}

	allowDecision := authorizeRequest(&revokedSessionPolicy, identityContext{
		HumanID:   "human-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
	}, "tools/call", "upper")

	if !allowDecision.Allowed {
		t.Fatalf("allow decision = %#v, want revoked optional session ignored", allowDecision)
	}
	if allowDecision.ConsentedTrust != "high" || allowDecision.EffectiveTrust != "high" {
		t.Fatalf("allow decision = %#v, want admin trust when optional session is revoked", allowDecision)
	}
}

func TestAbsoluteRequestURLUsesRequestHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Host = "proxy.example.com"

	if got := absoluteRequestURL(req, "/mcp"); got != "http://proxy.example.com/mcp" {
		t.Fatalf("absoluteRequestURL() = %q, want %q", got, "http://proxy.example.com/mcp")
	}
}

func TestTrimRequestPathPrefixMatchesOnlyPathBoundaries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		value  string
		prefix string
		want   string
		ok     bool
	}{
		{name: "exact match", value: "/mcp", prefix: "/mcp", want: "", ok: true},
		{name: "child path", value: "/mcp/tools", prefix: "/mcp", want: "/tools", ok: true},
		{name: "segment prefix only", value: "/mcp-tools", prefix: "/mcp", want: "/mcp-tools", ok: false},
		{name: "unrelated path", value: "/health", prefix: "/mcp", want: "/health", ok: false},
		{name: "trailing slash in config", value: "/mcp/tools", prefix: "/mcp/", want: "/tools", ok: true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := trimRequestPathPrefix(tc.value, tc.prefix)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("trimRequestPathPrefix(%q, %q) = (%q, %v), want (%q, %v)", tc.value, tc.prefix, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestResolveBaseURLPathPreservesBaseSubpath(t *testing.T) {
	t.Parallel()

	base, err := parseExternalBaseURL("https://public.example.com/proxy")
	if err != nil {
		t.Fatalf("parseExternalBaseURL() error = %v", err)
	}

	if got := resolveBaseURLPath(base, "/.well-known/oauth-protected-resource/mcp"); got != "https://public.example.com/proxy/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("resolveBaseURLPath() = %q, want %q", got, "https://public.example.com/proxy/.well-known/oauth-protected-resource/mcp")
	}
}

func TestAuditPayloadDoesNotPersistRawQueryString(t *testing.T) {
	t.Parallel()

	proxy := &proxyServer{
		serverName:           "example-server",
		serverNamespace:      "mcp-servers",
		clusterName:          "kind",
		defaultPolicyVersion: "test-policy",
	}
	req := httptest.NewRequest(http.MethodGet, "http://proxy.example.com/mcp?code=secret&state=opaque", nil)

	payload := proxy.auditPayload(
		req,
		"/mcp",
		"",
		"",
		identityContext{HumanID: "human-1"},
		nil,
		authzDecision{Allowed: true, Reason: "allowed", PolicyVersion: "test-policy"},
		http.StatusOK,
		12,
		34,
	)

	if _, exists := payload["query"]; exists {
		t.Fatalf("audit payload unexpectedly retained query string: %#v", payload)
	}
}

func TestStartPolicyCacheRequiresConfiguredPolicyFile(t *testing.T) {
	t.Parallel()

	proxy := &proxyServer{
		policyFile:            filepath.Join(t.TempDir(), "missing-policy.json"),
		defaultHumanHeader:    defaultHumanHeader,
		defaultAgentHeader:    defaultAgentHeader,
		defaultSessionHeader:  defaultSessionHeader,
		defaultPolicyMode:     defaultPolicyMode,
		defaultPolicyDecision: defaultPolicyDecision,
		defaultPolicyVersion:  "test-policy",
	}

	if err := proxy.startPolicyCache(); err == nil {
		t.Fatal("startPolicyCache() error = nil, want missing policy file error")
	}
}

func TestEmitIfEnabledDropsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	proxy := &proxyServer{
		analyticsURL:   "http://analytics.example.com",
		analyticsQueue: make(chan analyticsEvent, 1),
	}
	proxy.analyticsQueue <- analyticsEvent{Source: "existing"}

	done := make(chan struct{})
	go func() {
		proxy.emitIfEnabled(analyticsEvent{Source: "dropped"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitIfEnabled() blocked with a full queue")
	}

	select {
	case event := <-proxy.analyticsQueue:
		if event.Source != "existing" {
			t.Fatalf("analytics queue head = %#v, want existing event to remain", event)
		}
	default:
		t.Fatal("analytics queue unexpectedly drained")
	}
}

type testJWTIssuer struct {
	privateKey *rsa.PrivateKey
	server     *httptest.Server
	url        string
}

func newTestJWTIssuer(t *testing.T) *testJWTIssuer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	issuer := &testJWTIssuer{privateKey: privateKey}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"jwks_uri": issuer.server.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{rsaJWK(&privateKey.PublicKey)},
		})
	})

	issuer.server = httptest.NewServer(mux)
	issuer.url = issuer.server.URL
	t.Cleanup(issuer.server.Close)
	return issuer
}

func (i *testJWTIssuer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(i.privateKey)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return signed
}

func newTestProxyServer(t *testing.T, policy *policypkg.Document, upstream http.HandlerFunc) *proxyServer {
	t.Helper()

	upstreamServer := httptest.NewServer(upstream)
	t.Cleanup(upstreamServer.Close)

	target, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	reverseProxy := newUpstreamReverseProxy(target)
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		t.Fatalf("proxy error: %v", err)
	}

	server := &proxyServer{
		proxy:                 reverseProxy,
		httpClient:            &http.Client{Timeout: 2 * time.Second},
		defaultHumanHeader:    defaultHumanHeader,
		defaultAgentHeader:    defaultAgentHeader,
		defaultSessionHeader:  defaultSessionHeader,
		defaultPolicyMode:     defaultPolicyMode,
		defaultPolicyDecision: defaultPolicyDecision,
		defaultPolicyVersion:  "test-policy",
		oauthProviders:        map[string]*oauthProvider{},
	}
	server.snapshotPolicy(policySnapshot{Policy: policy})
	return server
}

func oauthPolicy(issuerURL string) *policypkg.Document {
	return &policypkg.Document{
		Auth: &policypkg.Auth{
			Mode:            "oauth",
			HumanIDHeader:   defaultHumanHeader,
			AgentIDHeader:   defaultAgentHeader,
			SessionIDHeader: defaultSessionHeader,
			TokenHeader:     "Authorization",
			IssuerURL:       issuerURL,
			Audience:        "mcp-runtime",
		},
		Policy: &policypkg.Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
		Session: &policypkg.Session{
			Required:            true,
			UpstreamTokenHeader: "Authorization",
		},
		Tools: []policypkg.Tool{
			{Name: "echo", RequiredTrust: "low"},
		},
		Grants: []policypkg.Grant{
			{
				Name:      "grant-1",
				HumanID:   "human-1",
				AgentID:   "client-1",
				MaxTrust:  "high",
				ToolRules: []policypkg.ToolAccess{{Name: "echo", Decision: "allow"}},
			},
		},
		Sessions: []policypkg.Binding{
			{
				Name:           "session-1",
				HumanID:        "human-1",
				AgentID:        "client-1",
				ConsentedTrust: "high",
			},
		},
	}
}

func rsaJWK(publicKey *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": "test-key",
		"n":   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
	}
}
