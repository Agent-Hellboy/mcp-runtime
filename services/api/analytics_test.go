package main

import (
	"strings"
	"testing"
)

func TestAnalyticsServersQueryExtractsTeamIDFromPayload(t *testing.T) {
	t.Parallel()

	query := analyticsServersQuery("mcp")
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
