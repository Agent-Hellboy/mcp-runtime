package policy

import (
	"net/http"
	"testing"
	"time"
)

func TestAuthorizeAllowsNonToolCalls(t *testing.T) {
	t.Parallel()

	decision := Authorize(&Document{
		Policy: &Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
	}, Request{RPCMethod: "initialize"}, time.Time{})

	if !decision.Allowed || decision.Status != http.StatusOK {
		t.Fatalf("decision = %#v, want allowed non-tool call", decision)
	}
}

func TestAuthorizeObserveModeAllowsWithoutIdentity(t *testing.T) {
	t.Parallel()

	decision := Authorize(&Document{
		Policy: &Config{
			Mode:            "observe",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
	}, Request{RPCMethod: "tools/call", ToolName: "upper"}, time.Time{})

	if !decision.Allowed {
		t.Fatalf("decision = %#v, want observe-mode allow", decision)
	}
}

func TestAuthorizeDefaultDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		policy    *Document
		wantAllow bool
		wantCode  int
	}{
		{
			name: "default deny",
			policy: &Document{
				Policy: &Config{
					Mode:            "allow-list",
					DefaultDecision: "deny",
					PolicyVersion:   "test-policy",
				},
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "default allow",
			policy: &Document{
				Policy: &Config{
					Mode:            "allow-list",
					DefaultDecision: "allow",
					PolicyVersion:   "test-policy",
				},
			},
			wantAllow: true,
			wantCode:  http.StatusOK,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decision := Authorize(tc.policy, Request{
				Identity:  Identity{HumanID: "human-1"},
				RPCMethod: "tools/call",
				ToolName:  "upper",
			}, time.Time{})

			if decision.Allowed != tc.wantAllow || decision.Status != tc.wantCode || decision.Reason != "no_matching_grant" {
				t.Fatalf("decision = %#v, want allowed=%v status=%d reason=no_matching_grant", decision, tc.wantAllow, tc.wantCode)
			}
		})
	}
}

func TestAuthorizeOptionalSessionDoesNotApplyWithoutSessionHeader(t *testing.T) {
	t.Parallel()

	policy := testPolicyWithGrant()
	policy.Sessions = []Binding{
		{
			Name:           "session-1",
			HumanID:        "human-1",
			AgentID:        "agent-1",
			ConsentedTrust: "low",
		},
	}

	decision := Authorize(policy, Request{
		Identity:  Identity{HumanID: "human-1", AgentID: "agent-1"},
		RPCMethod: "tools/call",
		ToolName:  "upper",
	}, time.Time{})

	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed request", decision)
	}
	if decision.ConsentedTrust != "high" || decision.EffectiveTrust != "high" {
		t.Fatalf("decision = %#v, want optional session ignored without header", decision)
	}
}

func TestAuthorizeOptionalSessionRequiresLiveSessionHeader(t *testing.T) {
	t.Parallel()

	liveSessionPolicy := testPolicyWithGrant()
	liveSessionPolicy.Sessions = []Binding{
		{
			Name:           "session-1",
			HumanID:        "human-1",
			AgentID:        "agent-1",
			ConsentedTrust: "low",
		},
	}

	denyDecision := Authorize(liveSessionPolicy, Request{
		Identity:  Identity{HumanID: "human-1", AgentID: "agent-1", SessionID: "session-1"},
		RPCMethod: "tools/call",
		ToolName:  "upper",
	}, time.Time{})

	if denyDecision.Reason != "trust_too_low" {
		t.Fatalf("deny decision = %#v, want trust_too_low", denyDecision)
	}

	revokedSessionPolicy := testPolicyWithGrant()
	revokedSessionPolicy.Sessions = []Binding{
		{
			Name:           "session-1",
			HumanID:        "human-1",
			AgentID:        "agent-1",
			ConsentedTrust: "low",
			Revoked:        true,
		},
	}

	allowDecision := Authorize(revokedSessionPolicy, Request{
		Identity:  Identity{HumanID: "human-1", AgentID: "agent-1", SessionID: "session-1"},
		RPCMethod: "tools/call",
		ToolName:  "upper",
	}, time.Time{})

	if !allowDecision.Allowed {
		t.Fatalf("allow decision = %#v, want revoked optional session ignored", allowDecision)
	}
	if allowDecision.ConsentedTrust != "high" || allowDecision.EffectiveTrust != "high" {
		t.Fatalf("allow decision = %#v, want admin trust when optional session is revoked", allowDecision)
	}
}

func TestAuthorizeRequiredSessionFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		identity Identity
		sessions []Binding
		reason   string
		status   int
	}{
		{
			name:     "missing session header",
			identity: Identity{HumanID: "human-1", AgentID: "agent-1"},
			reason:   "missing_session",
			status:   http.StatusUnauthorized,
		},
		{
			name:     "session not found",
			identity: Identity{HumanID: "human-1", AgentID: "agent-1", SessionID: "missing"},
			sessions: []Binding{{Name: "session-1", HumanID: "human-1", AgentID: "agent-1", ConsentedTrust: "high"}},
			reason:   "session_not_found",
			status:   http.StatusUnauthorized,
		},
		{
			name:     "revoked session",
			identity: Identity{HumanID: "human-1", AgentID: "agent-1", SessionID: "session-1"},
			sessions: []Binding{{Name: "session-1", HumanID: "human-1", AgentID: "agent-1", ConsentedTrust: "high", Revoked: true}},
			reason:   "session_revoked",
			status:   http.StatusUnauthorized,
		},
		{
			name:     "expired session",
			identity: Identity{HumanID: "human-1", AgentID: "agent-1", SessionID: "session-1"},
			sessions: []Binding{{Name: "session-1", HumanID: "human-1", AgentID: "agent-1", ConsentedTrust: "high", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)}},
			reason:   "session_expired",
			status:   http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy := testPolicyWithGrant()
			policy.Session.Required = true
			policy.Sessions = tc.sessions

			decision := Authorize(policy, Request{
				Identity:  tc.identity,
				RPCMethod: "tools/call",
				ToolName:  "upper",
			}, now)

			if decision.Allowed || decision.Status != tc.status || decision.Reason != tc.reason {
				t.Fatalf("decision = %#v, want deny status=%d reason=%s", decision, tc.status, tc.reason)
			}
		})
	}
}

func TestAuthorizeTrustTooLow(t *testing.T) {
	t.Parallel()

	policy := testPolicyWithGrant()
	policy.Grants[0].MaxTrust = "low"

	decision := Authorize(policy, Request{
		Identity:  Identity{HumanID: "human-1", AgentID: "agent-1"},
		RPCMethod: "tools/call",
		ToolName:  "upper",
	}, time.Time{})

	if decision.Allowed || decision.Reason != "trust_too_low" {
		t.Fatalf("decision = %#v, want trust_too_low denial", decision)
	}
	if decision.RequiredTrust != "medium" || decision.AdminTrust != "low" || decision.EffectiveTrust != "low" {
		t.Fatalf("decision = %#v, want trust context included", decision)
	}
}

func testPolicyWithGrant() *Document {
	return &Document{
		Policy: &Config{
			Mode:            "allow-list",
			DefaultDecision: "deny",
			PolicyVersion:   "test-policy",
		},
		Session: &Session{
			Required: false,
		},
		Tools: []Tool{
			{Name: "upper", RequiredTrust: "medium"},
		},
		Grants: []Grant{
			{
				Name:      "grant-1",
				HumanID:   "human-1",
				AgentID:   "agent-1",
				MaxTrust:  "high",
				ToolRules: []ToolAccess{{Name: "upper", Decision: "allow"}},
			},
		},
	}
}
