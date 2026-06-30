package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clickhousepkg "mcp-runtime/pkg/clickhouse"
)

type eventReaderStub struct {
	filters clickhousepkg.EventFilters
	err     error
}

func (s *eventReaderStub) QueryEvents(context.Context, int, int) ([]clickhousepkg.EventRow, error) {
	return nil, s.err
}
func (s *eventReaderStub) QueryStats(context.Context) (uint64, error) { return 7, s.err }
func (s *eventReaderStub) QuerySources(context.Context) ([]clickhousepkg.SourceStat, error) {
	return nil, s.err
}
func (s *eventReaderStub) QueryEventTypes(context.Context) ([]clickhousepkg.EventTypeStat, error) {
	return nil, s.err
}
func (s *eventReaderStub) QueryEventsFiltered(_ context.Context, filters clickhousepkg.EventFilters) ([]clickhousepkg.EventRow, error) {
	s.filters = filters
	return nil, s.err
}

func TestEventsBuildsFiltersFromQueryParams(t *testing.T) {
	stub := &eventReaderStub{}
	handler := NewHandler(stub)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?server=payments&team_id=team-1&limit=100", nil)

	handler.Events(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if stub.filters.Server != "payments" || stub.filters.TeamID != "team-1" || stub.filters.Limit != 100 {
		t.Fatalf("filters = %#v", stub.filters)
	}
}

func TestEventsRejectsInvalidLimit(t *testing.T) {
	handler := NewHandler(&eventReaderStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?server=payments&limit=5000", nil)

	handler.Events(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_query_param" {
		t.Fatalf("error code = %q", body["error"])
	}
}

func TestEventsRejectsInvalidCursor(t *testing.T) {
	handler := NewHandler(&eventReaderStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?cursor=not-valid", nil)

	handler.Events(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestEventsReturnsPaginationMeta(t *testing.T) {
	handler := NewHandler(pagingEventStub{pageSize: 2})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=2", nil)

	handler.Events(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta = %#v", body["meta"])
	}
	if meta["has_more"] != true {
		t.Fatalf("has_more = %#v, want true", meta["has_more"])
	}
	if _, ok := meta["next_cursor"].(string); !ok || meta["next_cursor"] == "" {
		t.Fatalf("next_cursor = %#v, want non-empty string", meta["next_cursor"])
	}
	if _, ok := body["next"].(string); !ok {
		t.Fatalf("next link missing: %#v", body["next"])
	}
}

type pagingEventStub struct {
	pageSize int
}

func (s pagingEventStub) QueryEvents(_ context.Context, limit, _ int) ([]clickhousepkg.EventRow, error) {
	n := limit
	if s.pageSize > 0 {
		n = s.pageSize
	}
	rows := make([]clickhousepkg.EventRow, n)
	return rows, nil
}
func (pagingEventStub) QueryStats(context.Context) (uint64, error) { return 0, nil }
func (pagingEventStub) QuerySources(context.Context) ([]clickhousepkg.SourceStat, error) {
	return nil, nil
}
func (pagingEventStub) QueryEventTypes(context.Context) ([]clickhousepkg.EventTypeStat, error) {
	return nil, nil
}
func (pagingEventStub) QueryEventsFiltered(_ context.Context, filters clickhousepkg.EventFilters) ([]clickhousepkg.EventRow, error) {
	rows := make([]clickhousepkg.EventRow, filters.Limit)
	return rows, nil
}

func TestStatsReturnsQueryFailedEnvelope(t *testing.T) {
	handler := NewHandler(&eventReaderStub{err: errors.New("unavailable")})
	recorder := httptest.NewRecorder()

	handler.Stats(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	got := recorder.Body.String()
	if !strings.Contains(got, `"error":"query_failed"`) {
		t.Fatalf("body = %q", got)
	}
	if !strings.Contains(got, `"message":"query failed"`) {
		t.Fatalf("body = %q", got)
	}
}
