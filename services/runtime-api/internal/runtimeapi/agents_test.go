package runtimeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"mcp-runtime-api/internal/platformclient"
	sentinelaccess "mcp-runtime/pkg/access"
)

type agentIdentityStub struct {
	identityStore
	created bool
	agent   platformclient.Agent
}

type missingAgentIdentityStub struct{ agentIdentityStub }

func (missingAgentIdentityStub) GetAgent(context.Context, string) (platformclient.Agent, bool, error) {
	return platformclient.Agent{}, false, nil
}

func (agentIdentityStub) Configured() bool { return true }
func (s *agentIdentityStub) CreateAgent(_ context.Context, slug, name, createdBy string) (platformclient.Agent, error) {
	s.created = true
	return platformclient.Agent{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamID: "team-id", TeamSlug: slug, Name: name, Status: "active", CreatedBy: createdBy}, nil
}
func (agentIdentityStub) ListAgents(_ context.Context, slug, status, query, cursor string, limit int) (platformclient.AgentPage, error) {
	return platformclient.AgentPage{Agents: []platformclient.Agent{{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamSlug: slug, Status: "active"}}}, nil
}
func (s *agentIdentityStub) GetAgent(_ context.Context, id string) (platformclient.Agent, bool, error) {
	if s.agent.ID == "" {
		return platformclient.Agent{ID: id, TeamID: "team-id", TeamSlug: "core", Status: "active"}, true, nil
	}
	return s.agent, true, nil
}

func TestRequireActiveAgent(t *testing.T) {
	t.Setenv(agentDirectoryEnforcementEnv, "enforce")
	store := &agentIdentityStub{}
	cases := []struct {
		name, id, team string
		store          identityStore
		want           error
	}{
		{name: "empty subject", store: store},
		{name: "active matching", id: "agt_01arz3ndektsv4rrffq69g5fav", team: "team-id", store: store},
		{name: "wrong team", id: "agt_01arz3ndektsv4rrffq69g5fav", team: "other", store: store, want: errAgentNotActive},
		{name: "directory unavailable", id: "agt_01arz3ndektsv4rrffq69g5fav", team: "team-id", want: errAgentDirectoryUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireActiveAgent(t.Context(), tc.store, tc.id, tc.team)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	store.agent = platformclient.Agent{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamID: "team-id", Status: "inactive"}
	if err := requireActiveAgent(t.Context(), store, store.agent.ID, store.agent.TeamID); !errors.Is(err, errAgentNotActive) {
		t.Fatalf("inactive agent error = %v, want %v", err, errAgentNotActive)
	}
}

func TestAgentDirectoryEnforcementModeDefaultsAndFailsClosed(t *testing.T) {
	t.Setenv(agentDirectoryEnforcementEnv, "")
	if got := AgentDirectoryEnforcementMode(); got != "warn" {
		t.Fatalf("default enforcement mode = %q, want warn", got)
	}
	t.Setenv(agentDirectoryEnforcementEnv, "WARN")
	if got := AgentDirectoryEnforcementMode(); got != "warn" {
		t.Fatalf("case-insensitive enforcement mode = %q, want warn", got)
	}
	t.Setenv(agentDirectoryEnforcementEnv, "unexpected")
	if got := AgentDirectoryEnforcementMode(); got != "enforce" {
		t.Fatalf("invalid enforcement mode = %q, want fail-closed enforce", got)
	}
}

func TestRequireActiveAgentCompatibilityModes(t *testing.T) {
	unknown := &missingAgentIdentityStub{}
	t.Setenv(agentDirectoryEnforcementEnv, "warn")
	if err := requireActiveAgent(t.Context(), unknown, "legacy-agent", "team-id"); err != nil {
		t.Fatalf("warn mode should allow an unknown legacy ID, got %v", err)
	}
	if err := requireActiveAgent(t.Context(), nil, "legacy-agent", "team-id"); err != nil {
		t.Fatalf("warn mode should allow a temporarily unavailable directory, got %v", err)
	}

	t.Setenv(agentDirectoryEnforcementEnv, "enforce")
	if err := requireActiveAgent(t.Context(), unknown, "legacy-agent", "team-id"); !errors.Is(err, errAgentNotActive) {
		t.Fatalf("enforce mode error = %v, want unknown-agent error", err)
	}
	if err := requireActiveAgent(t.Context(), nil, "legacy-agent", "team-id"); !errors.Is(err, errAgentDirectoryUnavailable) {
		t.Fatalf("enforce mode error = %v, want directory-unavailable error", err)
	}

	t.Setenv(agentDirectoryEnforcementEnv, "off")
	if err := requireActiveAgent(t.Context(), nil, "legacy-agent", "team-id"); err != nil {
		t.Fatalf("off mode should skip directory lookup, got %v", err)
	}
	inactive := &agentIdentityStub{agent: platformclient.Agent{ID: "known-agent", TeamID: "team-id", Status: "inactive"}}
	if err := requireActiveAgent(t.Context(), inactive, inactive.agent.ID, inactive.agent.TeamID); !errors.Is(err, errAgentNotActive) {
		t.Fatalf("off mode must still reject known inactive agents, got %v", err)
	}
}

func TestRuntimeAgentDirectoryConfigRequiresAuthentication(t *testing.T) {
	t.Setenv(agentDirectoryEnforcementEnv, "warn")
	server := &RuntimeServer{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/agents/config", nil)
	recorder := httptest.NewRecorder()
	server.HandleRuntimeAgentPath(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config status = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/agents/config", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleUser, Subject: "user-id"}))
	recorder = httptest.NewRecorder()
	server.HandleRuntimeAgentPath(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"enforcement":"warn"`) {
		t.Fatalf("authenticated config response = %d %s, want warn", recorder.Code, recorder.Body.String())
	}
}

type auditCollector struct{ events []auditEvent }

func (a *auditCollector) WriteAudit(_ context.Context, event auditEvent) {
	a.events = append(a.events, event)
}

func TestDeactivateAgentRevokesOnlyMatchingSessionsAndAuditsEach(t *testing.T) {
	newSession := func(name, namespace, agentID, teamID string, revoked bool) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "mcpruntime.org/v1alpha1", "kind": "MCPAgentSession",
			"metadata": map[string]any{"name": name, "namespace": namespace},
			"spec":     map[string]any{"subject": map[string]any{"agentID": agentID, "teamID": teamID}, "revoked": revoked},
		}}
	}
	objects := []runtime.Object{
		newSession("match", "ns-a", "agent-a", "team-a", false),
		newSession("other-team", "ns-b", "agent-a", "team-b", false),
		newSession("other-agent", "ns-a", "agent-b", "team-a", false),
		newSession("already-revoked", "ns-a", "agent-a", "team-a", true),
	}
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	audit := &auditCollector{}
	server := &RuntimeServer{accessMgr: sentinelaccess.NewManager(dyn, nil), audit: audit}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/agents/agent-a/deactivate", nil)
	if err := server.revokeAgentSessions(request.Context(), request, principal{Role: roleAdmin, Subject: "admin-id"}, platformclient.Agent{ID: "agent-a", TeamID: "team-a"}); err != nil {
		t.Fatal(err)
	}
	match, err := server.accessMgr.GetSession(request.Context(), "match", "ns-a")
	if err != nil || !match.Spec.Revoked {
		t.Fatalf("matching session revoked=%v err=%v", match != nil && match.Spec.Revoked, err)
	}
	otherTeam, _ := server.accessMgr.GetSession(request.Context(), "other-team", "ns-b")
	otherAgent, _ := server.accessMgr.GetSession(request.Context(), "other-agent", "ns-a")
	if otherTeam.Spec.Revoked || otherAgent.Spec.Revoked {
		t.Fatal("deactivation revoked a session outside the agent and team")
	}
	if len(audit.events) != 1 || audit.events[0].Action != "agent.session_revoked" || audit.events[0].AgentID != "agent-a" || audit.events[0].Namespace != "ns-a" {
		t.Fatalf("audit events = %#v, want one matching session revocation", audit.events)
	}
}

func TestAgentDirectoryTeamAuthorization(t *testing.T) {
	store := &agentIdentityStub{}
	server := &RuntimeServer{identity: store}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/teams/core/agents", strings.NewReader(`{"name":"release planner"}`))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeTeamAgents(recorder, request, principal{Role: roleUser, Subject: "member-id", Teams: []principalTeam{{Slug: "core", Role: teamRoleMember}}}, "core")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("member create status = %d, want 403", recorder.Code)
	}
	if store.created {
		t.Fatal("unauthorized request reached the identity store")
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/runtime/teams/core/agents", strings.NewReader(`{"name":"release planner"}`))
	recorder = httptest.NewRecorder()
	server.HandleRuntimeTeamAgents(recorder, request, principal{Role: roleUser, Subject: "owner-id", Teams: []principalTeam{{Slug: "core", Role: teamRoleOwner}}}, "core")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("owner create status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !store.created {
		t.Fatal("authorized create did not reach identity store")
	}
}

func TestAgentDirectoryTeamMemberCanList(t *testing.T) {
	server := &RuntimeServer{identity: &agentIdentityStub{}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/teams/core/agents?status=active&limit=10", nil)
	recorder := httptest.NewRecorder()
	server.HandleRuntimeTeamAgents(recorder, request, principal{Role: roleUser, Subject: "member-id", Teams: []principalTeam{{Slug: "core", Role: teamRoleMember}}}, "core")
	if recorder.Code != http.StatusOK {
		t.Fatalf("member list status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
