package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	policypkg "mcp-runtime/pkg/policy"
)

// ---- helpers ----------------------------------------------------------------

func newTestExchange(method, target, body string, headers map[string]string) *Exchange {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	r.ContentLength = int64(len(body))
	ex := &Exchange{
		W:            &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK},
		R:            r,
		OriginalPath: r.URL.Path,
		StartTime:    time.Now(),
		Decision: policypkg.Decision{
			Allowed:       true,
			Status:        http.StatusOK,
			Reason:        "allowed",
			PolicyVersion: "test",
		},
	}
	return ex
}

func minimalServer() *gatewayServer {
	return &gatewayServer{
		defaultHumanHeader:    defaultHumanHeader,
		defaultAgentHeader:    defaultAgentHeader,
		defaultTeamHeader:     defaultTeamHeader,
		defaultSessionHeader:  defaultSessionHeader,
		defaultPolicyMode:     defaultPolicyMode,
		defaultPolicyDecision: defaultPolicyDecision,
		defaultPolicyVersion:  "test",
		oauthProviders:        map[string]*oauthProvider{},
	}
}

// ---- stage 1: inspectFilter -------------------------------------------------

func TestInspectFilterAlwaysContinues(t *testing.T) {
	t.Parallel()
	s := minimalServer()

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		ex := newTestExchange(method, "/mcp", "", nil)
		if got := s.inspectFilter(ex); got != Continue {
			t.Errorf("inspectFilter(%s) = %v, want Continue", method, got)
		}
	}
}

func TestInspectFilterSetsInspection(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	body := `{"method":"tools/call","params":{"name":"echo"}}`
	ex := newTestExchange(http.MethodPost, "/mcp", body, map[string]string{"Content-Type": "application/json"})

	s.inspectFilter(ex)

	if ex.Inspection.Method != "tools/call" {
		t.Fatalf("Method = %q, want tools/call", ex.Inspection.Method)
	}
	if ex.Inspection.ToolName != "echo" {
		t.Fatalf("ToolName = %q, want echo", ex.Inspection.ToolName)
	}
	if !ex.Inspection.ToolCall {
		t.Fatal("ToolCall = false, want true")
	}
}

// ---- stage 2: policyFilter --------------------------------------------------

func TestPolicyFilterContinuesForNonOAuthPath(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	s.snapshotPolicy(policySnapshot{Policy: headerPolicy()})
	ex := newTestExchange(http.MethodPost, "/mcp", `{}`, map[string]string{"Content-Type": "application/json"})

	if got := s.policyFilter(ex); got != Continue {
		t.Fatalf("policyFilter = %v, want Continue", got)
	}
	if ex.Policy == nil {
		t.Fatal("Policy not set after policyFilter")
	}
}

func TestPolicyFilterRespondsForOAuthMetadataPath(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	issuer := newTestJWTIssuer(t)
	s.snapshotPolicy(policySnapshot{Policy: oauthPolicy(issuer.url)})
	ex := newTestExchange(http.MethodGet, "/.well-known/oauth-protected-resource", "", nil)

	if got := s.policyFilter(ex); got != Respond {
		t.Fatalf("policyFilter for OAuth metadata path = %v, want Respond", got)
	}
	recorder := ex.W.ResponseWriter.(*httptest.ResponseRecorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestPolicyFilterSetsSkipAuditForOAuthMetadataPath(t *testing.T) {
	t.Parallel()
	// Verify that an OAuth metadata early-exit sets SkipAudit so the
	// orchestrator does not emit a spurious audit event with no identity.
	s := minimalServer()
	issuer := newTestJWTIssuer(t)
	s.snapshotPolicy(policySnapshot{Policy: oauthPolicy(issuer.url)})
	ex := newTestExchange(http.MethodGet, "/.well-known/oauth-protected-resource", "", nil)

	s.policyFilter(ex)

	if !ex.SkipAudit {
		t.Fatal("SkipAudit = false after OAuth metadata early-exit, want true")
	}
}

func TestPolicyFilterPolicySnapshotIsImmutableForExchange(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	snap := headerPolicy()
	s.snapshotPolicy(policySnapshot{Policy: snap})
	ex := newTestExchange(http.MethodPost, "/mcp", "", nil)

	s.policyFilter(ex)
	captured := ex.Policy

	// Replace the gateway snapshot mid-exchange — the Exchange copy must be unaffected.
	s.snapshotPolicy(policySnapshot{Policy: &policypkg.Document{Server: policypkg.Server{Name: "other"}}})
	if ex.Policy != captured {
		t.Fatal("Exchange.Policy changed after snapshot was replaced — not immutable for exchange lifetime")
	}
}

// ---- stage 3: authFilter ----------------------------------------------------

func TestAuthFilterContinuesForHeaderMode(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	ex := newTestExchange(http.MethodPost, "/mcp", "", map[string]string{
		defaultHumanHeader: "user-1",
		defaultAgentHeader: "agent-1",
	})
	ex.Policy = headerPolicy()

	if got := s.authFilter(ex); got != Continue {
		t.Fatalf("authFilter header mode = %v, want Continue", got)
	}
	if ex.Identity.HumanID != "user-1" {
		t.Fatalf("Identity.HumanID = %q, want user-1", ex.Identity.HumanID)
	}
}

func TestAuthFilterRejectsOAuthMissingBearer(t *testing.T) {
	t.Parallel()
	issuer := newTestJWTIssuer(t)
	s := minimalServer()
	ex := newTestExchange(http.MethodPost, "/mcp", `{}`, map[string]string{"Content-Type": "application/json"})
	ex.Policy = oauthPolicy(issuer.url)

	if got := s.authFilter(ex); got != Reject {
		t.Fatalf("authFilter OAuth no bearer = %v, want Reject", got)
	}
	recorder := ex.W.ResponseWriter.(*httptest.ResponseRecorder)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if ex.Decision.Allowed {
		t.Fatal("Decision.Allowed = true after auth rejection, want false")
	}
}

func TestAuthFilterAlwaysRunsBeforeAuthz(t *testing.T) {
	t.Parallel()
	// Prove ordering by verifying that when authFilter Rejects, authzFilter
	// is never called. Use a sentinel filter after auth in a custom pipeline.
	authzCalled := false
	s := minimalServer()

	issuer := newTestJWTIssuer(t)
	s.snapshotPolicy(policySnapshot{Policy: oauthPolicy(issuer.url)})

	pipeline := []Filter{
		FilterFunc(s.inspectFilter),
		FilterFunc(s.policyFilter),
		FilterFunc(s.authFilter),
		FilterFunc(func(_ *Exchange) Result {
			authzCalled = true
			return Continue
		}),
	}

	ex := newExchange(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil), "test")
	for _, f := range pipeline {
		if f.Handle(ex) != Continue {
			break
		}
	}

	if authzCalled {
		t.Fatal("authz stage was called despite authFilter rejecting — ordering violated")
	}
}

// ---- stage 4: authzFilter ---------------------------------------------------

func TestAuthzFilterContinuesForNonToolCall(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	ex := newTestExchange(http.MethodPost, "/mcp", `{"method":"tools/list"}`, map[string]string{"Content-Type": "application/json"})
	s.inspectFilter(ex)
	ex.Policy = headerPolicy()
	ex.Identity = identityContext{HumanID: "human-1", AgentID: "client-1", TeamID: "team-acme"}

	if got := s.authzFilter(ex); got != Continue {
		t.Fatalf("authzFilter tools/list = %v, want Continue", got)
	}
	if !ex.Decision.Allowed {
		t.Fatalf("Decision.Allowed = false for non-tool-call, want true")
	}
}

func TestAuthzFilterRejectsDeniedToolCall(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	body := `{"method":"tools/call","params":{"name":"echo"}}`
	ex := newTestExchange(http.MethodPost, "/mcp", body, map[string]string{"Content-Type": "application/json"})
	s.inspectFilter(ex)
	// Use a session-optional allow-list policy so an unrecognised identity
	// reaches grant evaluation and gets no_matching_grant → 403.
	ex.Policy = &policypkg.Document{
		Auth: &policypkg.Auth{Mode: "header"},
		Policy: &policypkg.Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test",
		},
		Tools: []policypkg.Tool{
			{Name: "echo", RequiredTrust: "low", SideEffect: "read"},
		},
		Grants: []policypkg.Grant{
			{Name: "g1", HumanID: "human-1", AgentID: "agent-1",
				MaxTrust: "high", AllowedSideEffects: []string{"read"},
				ToolRules: []policypkg.ToolAccess{{Name: "echo", Decision: "allow"}}},
		},
	}
	// No matching grant → default deny.
	ex.Identity = identityContext{HumanID: "unknown", AgentID: "unknown"}

	if got := s.authzFilter(ex); got != Reject {
		t.Fatalf("authzFilter denied tool call = %v, want Reject", got)
	}
	if ex.Decision.Allowed {
		t.Fatal("Decision.Allowed = true after authz rejection, want false")
	}
	recorder := ex.W.ResponseWriter.(*httptest.ResponseRecorder)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestAuthzFilterAuthorizationInputsUnchangedAfterDecision(t *testing.T) {
	t.Parallel()
	// Prove that upstreamFilter (stage 5) receives the same Policy and Identity
	// pointers that authzFilter read, confirming no mutation happened between
	// stages 4 and 5.
	s := minimalServer()

	upstreamCalled := false
	var policyAtUpstream *policypkg.Document
	var identityAtUpstream identityContext

	target, _ := url.Parse("http://127.0.0.1:1") // unreachable; we intercept before
	s.proxy = newUpstreamReverseProxy(target)

	pipeline := []Filter{
		FilterFunc(s.authzFilter),
		FilterFunc(func(ex *Exchange) Result {
			upstreamCalled = true
			policyAtUpstream = ex.Policy
			identityAtUpstream = ex.Identity
			return Respond
		}),
	}

	body := `{"method":"tools/list"}`
	ex := newTestExchange(http.MethodPost, "/mcp", body, map[string]string{"Content-Type": "application/json"})
	s.inspectFilter(ex) // sets ToolCall=false, so authz skips evaluation
	pol := headerPolicy()
	ex.Policy = pol
	ident := identityContext{HumanID: "human-1", AgentID: "client-1", TeamID: "team-acme"}
	ex.Identity = ident

	for _, f := range pipeline {
		if f.Handle(ex) != Continue {
			break
		}
	}

	if !upstreamCalled {
		t.Fatal("upstream stage was not reached")
	}
	if policyAtUpstream != pol {
		t.Fatal("Policy was replaced between authz and upstream stages")
	}
	if identityAtUpstream != ident {
		t.Fatal("Identity was mutated between authz and upstream stages")
	}
}

func TestAuthzFilterPolicyUnavailableDeniesWith503(t *testing.T) {
	t.Parallel()
	s := minimalServer()
	body := `{"method":"tools/call","params":{"name":"echo"}}`
	ex := newTestExchange(http.MethodPost, "/mcp", body, map[string]string{"Content-Type": "application/json"})
	s.inspectFilter(ex)
	ex.Policy = &policypkg.Document{Policy: &policypkg.Config{}}
	ex.PolicyErr = errPolicyUnavailable

	if got := s.authzFilter(ex); got != Reject {
		t.Fatalf("authzFilter policy_unavailable = %v, want Reject", got)
	}
	recorder := ex.W.ResponseWriter.(*httptest.ResponseRecorder)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var payload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	if payload["error"] != "policy_unavailable" {
		t.Fatalf("error = %q, want policy_unavailable", payload["error"])
	}
}

// ---- stage 5: upstreamFilter ------------------------------------------------

func TestUpstreamFilterAlwaysReturnsRespond(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)

	s := minimalServer()
	s.proxy = newUpstreamReverseProxy(target)
	ex := newTestExchange(http.MethodGet, "/mcp", "", nil)
	ex.Policy = headerPolicy()

	if got := s.upstreamFilter(ex); got != Respond {
		t.Fatalf("upstreamFilter = %v, want Respond", got)
	}
}

// ---- errPolicyUnavailable sentinel ------------------------------------------

func TestErrPolicyUnavailableIsSentinel(t *testing.T) {
	// Verify that errPolicyUnavailable is a distinct error value that can be
	// identified by callers checking the policy unavailable condition.
	if errPolicyUnavailable == nil {
		t.Fatal("errPolicyUnavailable is nil")
	}
	if errPolicyUnavailable.Error() == "" {
		t.Fatal("errPolicyUnavailable.Error() is empty")
	}
}
