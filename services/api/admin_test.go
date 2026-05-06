package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminOperationsFilterFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/operations?user=alice@example.com&since=2026-05-01&until=2026-05-02&limit=999", nil)
	filter, err := adminOperationsFilterFromRequest(req)
	if err != nil {
		t.Fatalf("adminOperationsFilterFromRequest() error = %v", err)
	}
	if filter.User != "alice@example.com" {
		t.Fatalf("user = %q, want alice@example.com", filter.User)
	}
	if filter.UserSearch != "alice@example.com" {
		t.Fatalf("user search = %q, want alice@example.com", filter.UserSearch)
	}
	if filter.Limit != 200 {
		t.Fatalf("limit = %d, want clamp to 200", filter.Limit)
	}
	if filter.Since.Format("2006-01-02T15:04:05Z07:00") != "2026-05-01T00:00:00Z" {
		t.Fatalf("since = %s", filter.Since.Format(time.RFC3339Nano))
	}
	if filter.Until.Format("2006-01-02") != "2026-05-02" || filter.Until.Hour() != 23 {
		t.Fatalf("until = %s, want end of day", filter.Until.Format(time.RFC3339Nano))
	}
}

func TestAdminOperationsFilterRejectsInvalidRange(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/operations?since=2026-05-03T00:00:00Z&until=2026-05-02T00:00:00Z", nil)
	if _, err := adminOperationsFilterFromRequest(req); err == nil {
		t.Fatal("expected invalid range error")
	}
}

func TestMergeDeploymentImageActivityAppliesUserFilter(t *testing.T) {
	createdAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	items := mergeDeploymentImageActivity(nil, []map[string]any{
		{
			"name":       "demo",
			"namespace":  "tenant-a",
			"user_id":    "user-1",
			"image":      "registry.example.com/tenant-a/demo:v1",
			"created_at": createdAt,
		},
		{
			"name":       "other",
			"namespace":  "tenant-b",
			"user_id":    "user-2",
			"image":      "registry.example.com/tenant-b/other:v1",
			"created_at": createdAt,
		},
	}, adminOperationsFilter{User: "tenant-a"})
	if len(items) != 1 {
		t.Fatalf("image activity count = %d, want 1", len(items))
	}
	if items[0].DeploymentTarget != "tenant-a/demo" || items[0].Action != "deployment_current" {
		t.Fatalf("image activity = %#v", items[0])
	}
}

func TestMergeDeploymentImageActivityAppliesTargetFilterAndLimit(t *testing.T) {
	createdAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	items := mergeDeploymentImageActivity([]platformImageActivity{{
		UserID:           "user-1",
		Namespace:        "tenant-a",
		ImageRef:         "registry.example.com/tenant-a/existing:v1",
		DeploymentTarget: "tenant-a/existing",
		Action:           "image_publish",
		CreatedAt:        createdAt,
	}}, []map[string]any{
		{
			"name":       "demo",
			"namespace":  "tenant-a",
			"user_id":    "user-1",
			"image":      "registry.example.com/tenant-a/demo:v1",
			"created_at": createdAt,
		},
		{
			"name":       "other",
			"namespace":  "tenant-a",
			"user_id":    "user-1",
			"image":      "registry.example.com/tenant-a/other:v1",
			"created_at": createdAt,
		},
	}, adminOperationsFilter{User: "tenant-a/demo", UserSearch: "tenant-a/demo", Limit: 2})
	if len(items) != 2 {
		t.Fatalf("image activity count = %d, want cap at 2", len(items))
	}
	if items[1].DeploymentTarget != "tenant-a/demo" {
		t.Fatalf("merged target = %q, want tenant-a/demo", items[1].DeploymentTarget)
	}
}

func TestFilterDeploymentsForOperationsAppliesFiltersAndLimit(t *testing.T) {
	createdAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	deployments := filterDeploymentsForOperations([]map[string]any{
		{
			"name":       "demo",
			"namespace":  "tenant-a",
			"user_id":    "user-1",
			"created_by": "user-1",
			"image":      "registry.example.com/tenant-a/demo:v1",
			"created_at": createdAt,
		},
		{
			"name":       "old",
			"namespace":  "tenant-a",
			"user_id":    "user-1",
			"image":      "registry.example.com/tenant-a/old:v1",
			"created_at": createdAt.Add(-48 * time.Hour),
		},
		{
			"name":       "other",
			"namespace":  "tenant-b",
			"user_id":    "user-2",
			"image":      "registry.example.com/tenant-b/other:v1",
			"created_at": createdAt,
		},
	}, adminOperationsFilter{
		User:       "tenant-a",
		UserSearch: "tenant-a",
		Since:      createdAt.Add(-1 * time.Hour),
		Until:      createdAt.Add(1 * time.Hour),
		Limit:      1,
	})
	if len(deployments) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(deployments))
	}
	if got := deployments[0]["name"]; got != "demo" {
		t.Fatalf("deployment name = %v, want demo", got)
	}
}

func TestPlatformUserActivityWhereKeepsTimeFiltersOutOfTopLevelWhere(t *testing.T) {
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	where, args := platformUserActivityWhere(adminOperationsFilter{
		User:       "alice",
		UserSearch: "alice",
		Since:      since,
		Until:      until,
	})
	if len(args) != 3 {
		t.Fatalf("top-level args = %d, want only user search args", len(args))
	}
	if want := "u.deleted_at IS NULL"; !strings.Contains(where, want) {
		t.Fatalf("where = %q, want %q", where, want)
	}

	auditArgs := append([]any{}, args...)
	predicate := platformAuditTimeWhere("a", adminOperationsFilter{Since: since, Until: until}, &auditArgs)
	if len(auditArgs) != 5 {
		t.Fatalf("audit args = %d, want user args plus since/until", len(auditArgs))
	}
	if !strings.Contains(predicate, "a.created_at >= $4") || !strings.Contains(predicate, "a.created_at <= $5") {
		t.Fatalf("audit predicate = %q", predicate)
	}
}
