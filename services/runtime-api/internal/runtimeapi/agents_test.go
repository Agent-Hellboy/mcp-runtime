package runtimeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-runtime-api/internal/platformclient"
)

type agentIdentityStub struct {
	identityStore
	created bool
}

func (agentIdentityStub) Configured() bool { return true }
func (s *agentIdentityStub) CreateAgent(_ context.Context, slug, name, createdBy string) (platformclient.Agent, error) {
	s.created = true
	return platformclient.Agent{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamID: "team-id", TeamSlug: slug, Name: name, Status: "active", CreatedBy: createdBy}, nil
}
func (agentIdentityStub) ListAgents(_ context.Context, slug, status, query, cursor string, limit int) (platformclient.AgentPage, error) {
	return platformclient.AgentPage{Agents: []platformclient.Agent{{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamSlug: slug, Status: "active"}}}, nil
}
func (agentIdentityStub) GetAgent(context.Context, string) (platformclient.Agent, bool, error) {
	return platformclient.Agent{ID: "agt_01arz3ndektsv4rrffq69g5fav", TeamID: "team-id", TeamSlug: "core", Status: "active"}, true, nil
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
