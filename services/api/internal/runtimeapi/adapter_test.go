package runtimeapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-sentinel-api/internal/platformstore"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// adapterTestFixture sets up an in-memory access manager preloaded with a
// server and a grant so the adapter session handler can be exercised
// end-to-end without a real cluster.
type adapterTestFixture struct {
	server    *RuntimeServer
	principal principal
	scheme    *runtime.Scheme
}

func newAdapterTestFixture(t *testing.T, grants ...mcpv1alpha1.MCPAccessGrant) adapterTestFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	objects := []runtime.Object{
		&mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "mcp-team-acme"},
		},
	}
	for i := range grants {
		objects = append(objects, &grants[i])
	}
	mgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, objects...), nil)
	return adapterTestFixture{
		server: &RuntimeServer{accessMgr: mgr},
		principal: principal{
			Subject:   "user-123",
			Email:     "user@example.org",
			Namespace: "mcp-team-acme",
			Role:      roleUser,
			Teams: []platformstore.PrincipalTeam{
				{ID: "team-acme", Namespace: "mcp-team-acme"},
			},
		},
		scheme: scheme,
	}
}

func adapterRequest(t *testing.T, body adapterSessionRequest) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/runtime/adapter/sessions", bytes.NewReader(raw))
}

func decodeAdapterResponse(t *testing.T, recorder *httptest.ResponseRecorder) adapterSessionResponse {
	t.Helper()
	var resp adapterSessionResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	return resp
}

func TestAdapterSessionIssuesNewSessionFromMatchingGrant(t *testing.T) {
	grant := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "g1",
			Namespace:         "mcp-team-acme",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef:     mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:       mcpv1alpha1.SubjectRef{HumanID: "user-123", AgentID: "ops-agent", TeamID: "team-acme"},
			MaxTrust:      mcpv1alpha1.TrustLevel("high"),
			PolicyVersion: "v3",
		},
	}
	fx := newAdapterTestFixture(t, grant)
	req := adapterRequest(t, adapterSessionRequest{
		ServerName:     "demo",
		Namespace:      "mcp-team-acme",
		AgentID:        "ops-agent",
		RequestedTrust: "medium",
	})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeAdapterResponse(t, w)
	if got.HumanID != "user-123" || got.AgentID != "ops-agent" || got.TeamID != "team-acme" {
		t.Fatalf("identity = %#v, want user-123/ops-agent/team-acme", got)
	}
	if got.ConsentedTrust != "medium" {
		t.Fatalf("consentedTrust = %q, want medium (requested, within max=high)", got.ConsentedTrust)
	}
	if got.PolicyVersion != "v3" {
		t.Fatalf("policyVersion = %q, want v3 (from grant)", got.PolicyVersion)
	}
	if got.Reused {
		t.Fatal("reused = true on first call, want false")
	}
	if !strings.HasPrefix(got.Name, "adapter-") {
		t.Fatalf("name = %q, want adapter-<hash> prefix", got.Name)
	}
	if got.ExpiresAt.Before(time.Now()) {
		t.Fatalf("expiresAt = %v, must be in the future", got.ExpiresAt)
	}
}

func TestAdapterSessionIssuesCrossTeamSessionFromGrantedTeam(t *testing.T) {
	grant := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "globex-to-acme", Namespace: "mcp-team-acme"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{AgentID: "ops-agent", TeamID: "team-globex"},
			MaxTrust:  mcpv1alpha1.TrustLevel("low"),
		},
	}
	fx := newAdapterTestFixture(t, grant)
	fx.principal.Namespace = "mcp-team-globex"
	fx.principal.Teams = []platformstore.PrincipalTeam{
		{ID: "team-globex", Namespace: "mcp-team-globex"},
	}
	req := adapterRequest(t, adapterSessionRequest{
		ServerName: "demo",
		Namespace:  "mcp-team-acme",
		AgentID:    "ops-agent",
	})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeAdapterResponse(t, w)
	if got.TeamID != "team-globex" {
		t.Fatalf("teamID = %q, want team-globex", got.TeamID)
	}
}

func TestAdapterSessionRejectsGrantForUnheldTeam(t *testing.T) {
	grant := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "globex-to-acme", Namespace: "mcp-team-acme"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{AgentID: "ops-agent", TeamID: "team-globex"},
			MaxTrust:  mcpv1alpha1.TrustLevel("low"),
		},
	}
	fx := newAdapterTestFixture(t, grant)
	req := adapterRequest(t, adapterSessionRequest{
		ServerName: "demo",
		Namespace:  "mcp-team-acme",
		AgentID:    "ops-agent",
	})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no enabled MCPAccessGrant") {
		t.Fatalf("body = %q, want 'no enabled MCPAccessGrant'", w.Body.String())
	}
}

func TestAdapterSessionRejectsWhenNoGrantMatches(t *testing.T) {
	fx := newAdapterTestFixture(t) // no grants
	req := adapterRequest(t, adapterSessionRequest{
		ServerName: "demo",
		Namespace:  "mcp-team-acme",
		AgentID:    "ops-agent",
	})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no enabled MCPAccessGrant") {
		t.Fatalf("body = %q, want 'no enabled MCPAccessGrant'", w.Body.String())
	}
}

func TestAdapterSessionTrustCappedAtGrantMaxTrust(t *testing.T) {
	grant := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "g1", Namespace: "mcp-team-acme"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{TeamID: "team-acme"}, // team wildcard for humanID/agentID
			MaxTrust:  mcpv1alpha1.TrustLevel("low"),
		},
	}
	fx := newAdapterTestFixture(t, grant)
	req := adapterRequest(t, adapterSessionRequest{
		ServerName:     "demo",
		Namespace:      "mcp-team-acme",
		AgentID:        "ops-agent",
		RequestedTrust: "high",
	})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeAdapterResponse(t, w)
	if got.ConsentedTrust != "low" {
		t.Fatalf("consentedTrust = %q, want low (capped at grant.MaxTrust)", got.ConsentedTrust)
	}
}

func TestAdapterSessionPicksHighestTrustWithDeterministicTiebreak(t *testing.T) {
	older := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "g-older",
			Namespace:         "mcp-team-acme",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
		},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{TeamID: "team-acme"},
			MaxTrust:  mcpv1alpha1.TrustLevel("high"),
		},
	}
	newer := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "g-newer",
			Namespace:         "mcp-team-acme",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{TeamID: "team-acme"},
			MaxTrust:  mcpv1alpha1.TrustLevel("high"),
		},
	}
	low := mcpv1alpha1.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "g-low", Namespace: "mcp-team-acme"},
		Spec: mcpv1alpha1.MCPAccessGrantSpec{
			ServerRef: mcpv1alpha1.ServerReference{Name: "demo", Namespace: "mcp-team-acme"},
			Subject:   mcpv1alpha1.SubjectRef{TeamID: "team-acme"},
			MaxTrust:  mcpv1alpha1.TrustLevel("low"),
		},
	}
	fx := newAdapterTestFixture(t, low, newer, older)
	g, teamID, err := fx.server.selectAdapterGrant(
		t.Context(),
		"mcp-team-acme", "demo",
		"user-123", "ops-agent", []string{"team-acme"}, "team-acme", false,
	)
	if err != nil {
		t.Fatalf("selectAdapterGrant: %v", err)
	}
	if g.Name != "g-older" {
		t.Fatalf("selected = %q, want g-older (highest trust, oldest first for tiebreak)", g.Name)
	}
	if teamID != "team-acme" {
		t.Fatalf("teamID = %q, want team-acme", teamID)
	}
}

func TestAdapterSessionRequiresServerName(t *testing.T) {
	fx := newAdapterTestFixture(t)
	req := adapterRequest(t, adapterSessionRequest{Namespace: "mcp-team-acme", AgentID: "ops-agent"})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestAdapterSessionRequiresResolvedNamespace(t *testing.T) {
	fx := newAdapterTestFixture(t)
	fx.principal.Namespace = ""
	req := adapterRequest(t, adapterSessionRequest{ServerName: "demo", AgentID: "ops-agent"})
	req = req.WithContext(withPrincipal(req.Context(), fx.principal))
	w := httptest.NewRecorder()
	fx.server.HandleAdapterSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "namespace is required") {
		t.Fatalf("body = %q, want namespace validation error", w.Body.String())
	}
}

func TestAdapterSessionRejectsTTLBeyondHardCap(t *testing.T) {
	d, err := parseAdapterTTL("48h")
	if err != nil {
		t.Fatalf("parseAdapterTTL: %v", err)
	}
	if d != adapterSessionMaxTTL {
		t.Fatalf("got %v, want capped at %v", d, adapterSessionMaxTTL)
	}
}

func TestAdapterSessionDeterministicName(t *testing.T) {
	n1 := adapterSessionName("h1", "a1", "t1", "srv")
	n2 := adapterSessionName("h1", "a1", "t1", "srv")
	if n1 != n2 {
		t.Fatalf("name n1 = %q, n2 = %q, want equal for identical inputs", n1, n2)
	}
	if adapterSessionName("h1", "a1", "t1", "srv") == adapterSessionName("h2", "a1", "t1", "srv") {
		t.Fatal("name should differ when humanID differs")
	}
	if !strings.HasPrefix(n1, "adapter-") {
		t.Fatalf("name = %q, want adapter- prefix", n1)
	}
}

func TestAdapterSessionReusableRejectsExpiredOrRevoked(t *testing.T) {
	now := time.Now()
	ok := &sentinelaccess.MCPAgentSession{
		Spec: sentinelaccess.MCPAgentSessionSpec{
			PolicyVersion:  "v1",
			ConsentedTrust: sentinelaccess.TrustLow,
			ExpiresAt:      &metav1.Time{Time: now.Add(time.Hour)},
		},
	}
	revoked := *ok
	revoked.Spec.Revoked = true
	soon := *ok
	soon.Spec.ExpiresAt = &metav1.Time{Time: now.Add(5 * time.Second)} // < refresh buffer
	mismatchPolicy := *ok
	mismatchPolicy.Spec.PolicyVersion = "v2"

	if !adapterSessionReusable(ok, "v1", sentinelaccess.TrustLow) {
		t.Fatal("happy path should be reusable")
	}
	if adapterSessionReusable(&revoked, "v1", sentinelaccess.TrustLow) {
		t.Fatal("revoked session must not be reused")
	}
	if adapterSessionReusable(&soon, "v1", sentinelaccess.TrustLow) {
		t.Fatal("session inside refresh buffer must not be reused")
	}
	if adapterSessionReusable(&mismatchPolicy, "v1", sentinelaccess.TrustLow) {
		t.Fatal("policy-version mismatch must not be reused")
	}
}
