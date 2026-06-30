package usage_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcp-analytics-api/internal/usage"
	"mcp-runtime/pkg/platformauth"
)

func TestServersQueryExtractsTeamIDFromPayload(t *testing.T) {
	t.Parallel()
	query := usage.ServersQuery("mcp", "WHERE timestamp >= ? AND server != ''")
	if !strings.Contains(query, "JSONExtractString(payload, 'team_id') AS team_id") {
		t.Fatalf("query = %q, want team_id extracted from payload", query)
	}
}

func TestWhereClauseScopesByNamespaceAndTeam(t *testing.T) {
	t.Parallel()
	scope := usage.QueryScope{
		Since:      time.Unix(1700000000, 0).UTC(),
		Namespaces: []string{"user-a", "mcp-team-acme"},
		TeamIDs:    []string{"team-acme-id"},
		Server:     "payments",
		Decision:   "deny",
		ToolName:   "refund",
	}
	where, args := usage.WhereClause(scope, "server != ''")
	if !strings.Contains(where, "(namespace IN (?, ?) OR JSONExtractString(payload, 'team_id') IN (?))") {
		t.Fatalf("where = %q, want namespace/team scoped OR filter", where)
	}
	if len(args) != 7 {
		t.Fatalf("args = %#v, want 7 args", args)
	}
}

func TestHandleAdminUsageRejectsNonGET(t *testing.T) {
	t.Parallel()
	svc := &usage.Service{DBName: "mcp"}
	recorder := httptest.NewRecorder()
	svc.HandleAdminUsage(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/analytics/usage", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestPrincipalOwnedScopeExcludesSharedCatalog(t *testing.T) {
	t.Parallel()
	scope := usage.PrincipalOwnedScope(platformauth.Principal{
		Namespace: "user-a",
		Teams: []platformauth.PrincipalTeam{{
			ID:        "team-acme-id",
			Namespace: "mcp-team-acme",
		}},
		AllowedNamespaces: []string{"user-a", "mcp-team-acme", "mcp-servers"},
	})
	if got := strings.Join(scope.Namespaces, ","); got != "user-a,mcp-team-acme" {
		t.Fatalf("namespaces = %q, want user/team only", got)
	}
}

func TestHandleUserUsageRejectsForeignNamespace(t *testing.T) {
	t.Parallel()
	svc := &usage.Service{DBName: "mcp"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/user/analytics/usage?namespace=mcp-team-other", nil)
	request = request.WithContext(platformauth.WithPrincipal(request.Context(), platformauth.Principal{
		Namespace:         "user-a",
		AllowedNamespaces: []string{"user-a"},
	}))
	svc.HandleUserUsage(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "forbidden namespace" {
		t.Fatalf("payload = %#v", payload)
	}
}
