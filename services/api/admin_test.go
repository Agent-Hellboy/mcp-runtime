package main

import (
	"net/http"
	"net/http/httptest"
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
