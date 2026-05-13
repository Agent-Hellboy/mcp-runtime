package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnalyticsServersQueryExtractsTeamIDFromPayload(t *testing.T) {
	t.Parallel()

	query := analyticsServersQuery("mcp", "WHERE timestamp >= ? AND server != ''")
	if !strings.Contains(query, "JSONExtractString(payload, 'team_id') AS team_id") {
		t.Fatalf("query = %q, want team_id extracted from payload", query)
	}
	if strings.Contains(query, "SELECT server, namespace, team_id") {
		t.Fatalf("query = %q, should not require materialized team_id column in SELECT", query)
	}
	if !strings.Contains(query, "GROUP BY server, namespace, team_id") {
		t.Fatalf("query = %q, want stable grouping by team_id alias", query)
	}
}

func TestAnalyticsWhereClauseScopesByNamespaceAndTeam(t *testing.T) {
	t.Parallel()

	scope := analyticsQueryScope{
		Since:      time.Unix(1700000000, 0).UTC(),
		Namespaces: []string{"user-a", "mcp-team-acme"},
		TeamIDs:    []string{"team-acme-id"},
		Server:     "payments",
		Decision:   "deny",
		ToolName:   "refund",
	}
	where, args := analyticsWhereClause(scope, "server != ''")
	if !strings.Contains(where, "timestamp >= ?") {
		t.Fatalf("where = %q, want timestamp filter", where)
	}
	if !strings.Contains(where, "(namespace IN (?, ?) OR JSONExtractString(payload, 'team_id') IN (?))") {
		t.Fatalf("where = %q, want namespace/team scoped OR filter", where)
	}
	if !strings.Contains(where, "server = ?") || !strings.Contains(where, "decision = ?") || !strings.Contains(where, "tool_name = ?") {
		t.Fatalf("where = %q, want request filters", where)
	}
	if len(args) != 7 {
		t.Fatalf("args = %#v, want 7 args", args)
	}
}

func TestAnalyticsPrincipalOwnedScopeExcludesSharedCatalog(t *testing.T) {
	t.Parallel()

	scope := analyticsPrincipalOwnedScope(principal{
		Role:      roleUser,
		Namespace: "user-a",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Namespace: "mcp-team-acme",
		}},
		AllowedNamespaces: []string{"user-a", "mcp-team-acme", sharedCatalogNamespace},
	})
	if got := strings.Join(scope.Namespaces, ","); got != "user-a,mcp-team-acme" {
		t.Fatalf("namespaces = %q, want user/team only", got)
	}
	if got := strings.Join(scope.TeamIDs, ","); got != "team-acme-id" {
		t.Fatalf("teamIDs = %q, want team-acme-id", got)
	}
}

func TestAnalyticsPrincipalScopeForNamespaceRejectsForeignAndShared(t *testing.T) {
	t.Parallel()

	p := principal{
		Role:              roleUser,
		Namespace:         "user-a",
		AllowedNamespaces: []string{"user-a", sharedCatalogNamespace},
	}
	if _, ok := analyticsPrincipalScopeForNamespace(p, "user-a"); !ok {
		t.Fatal("expected user namespace to be allowed")
	}
	if _, ok := analyticsPrincipalScopeForNamespace(p, sharedCatalogNamespace); ok {
		t.Fatal("shared catalog should not be allowed for user analytics")
	}
	if _, ok := analyticsPrincipalScopeForNamespace(p, "mcp-team-other"); ok {
		t.Fatal("foreign namespace should not be allowed")
	}
}

func TestHandleUserAnalyticsUsageRejectsForeignNamespaceBeforeQuery(t *testing.T) {
	t.Parallel()

	server := &apiServer{dbName: "mcp"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/user/analytics/usage?namespace=mcp-team-other", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:              roleUser,
		Namespace:         "user-a",
		AllowedNamespaces: []string{"user-a", sharedCatalogNamespace},
	}))

	server.handleUserAnalyticsUsage(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "forbidden namespace" {
		t.Fatalf("payload = %#v, want forbidden namespace", payload)
	}
}
