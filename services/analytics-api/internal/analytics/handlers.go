package analytics

import (
	"context"
	"net/http"

	"mcp-runtime/pkg/apihttp"
	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/serviceutil"
)

type EventReader interface {
	QueryEvents(context.Context, int) ([]clickhousepkg.EventRow, error)
	QueryStats(context.Context) (uint64, error)
	QuerySources(context.Context) ([]clickhousepkg.SourceStat, error)
	QueryEventTypes(context.Context) ([]clickhousepkg.EventTypeStat, error)
	QueryEventsFiltered(context.Context, clickhousepkg.EventFilters) ([]clickhousepkg.EventRow, error)
}

type Handler struct {
	events EventReader
}

func NewHandler(events EventReader) *Handler {
	return &Handler{events: events}
}

func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	limit, err := apihttp.ParseLimit(r, 100, 1000)
	if err != nil {
		apihttp.WriteError(w, nil, err)
		return
	}

	var events []clickhousepkg.EventRow
	if hasEventFilters(r) {
		events, err = h.events.QueryEventsFiltered(r.Context(), eventFiltersFromRequest(r, limit))
	} else {
		events, err = h.events.QueryEvents(r.Context(), limit)
	}
	if err != nil {
		apihttp.WriteError(w, nil, &apihttp.Error{
			Status:  http.StatusInternalServerError,
			Code:    apihttp.CodeQueryFailed,
			Message: "query failed",
			Cause:   err,
		})
		return
	}
	serviceutil.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	count, err := h.events.QueryStats(r.Context())
	if err != nil {
		apihttp.WriteError(w, nil, &apihttp.Error{
			Status:  http.StatusInternalServerError,
			Code:    apihttp.CodeQueryFailed,
			Message: "query failed",
			Cause:   err,
		})
		return
	}
	serviceutil.WriteJSON(w, http.StatusOK, map[string]any{"events_total": count})
}

func (h *Handler) Sources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.events.QuerySources(r.Context())
	if err != nil {
		apihttp.WriteError(w, nil, &apihttp.Error{
			Status:  http.StatusInternalServerError,
			Code:    apihttp.CodeQueryFailed,
			Message: "query failed",
			Cause:   err,
		})
		return
	}
	serviceutil.WriteJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *Handler) EventTypes(w http.ResponseWriter, r *http.Request) {
	eventTypes, err := h.events.QueryEventTypes(r.Context())
	if err != nil {
		apihttp.WriteError(w, nil, &apihttp.Error{
			Status:  http.StatusInternalServerError,
			Code:    apihttp.CodeQueryFailed,
			Message: "query failed",
			Cause:   err,
		})
		return
	}
	serviceutil.WriteJSON(w, http.StatusOK, map[string]any{"event_types": eventTypes})
}

func hasEventFilters(r *http.Request) bool {
	q := r.URL.Query()
	for _, key := range []string{
		"trace_id", "source", "event_type", "server", "namespace", "team_id",
		"cluster", "human_id", "agent_id", "session_id", "decision", "tool_name", "reason",
	} {
		if q.Get(key) != "" {
			return true
		}
	}
	return false
}

func eventFiltersFromRequest(r *http.Request, limit int) clickhousepkg.EventFilters {
	q := r.URL.Query()
	return clickhousepkg.EventFilters{
		TraceID:   q.Get("trace_id"),
		Source:    q.Get("source"),
		EventType: q.Get("event_type"),
		Server:    q.Get("server"),
		Namespace: q.Get("namespace"),
		TeamID:    q.Get("team_id"),
		Cluster:   q.Get("cluster"),
		HumanID:   q.Get("human_id"),
		AgentID:   q.Get("agent_id"),
		SessionID: q.Get("session_id"),
		Decision:  q.Get("decision"),
		ToolName:  q.Get("tool_name"),
		Reason:    q.Get("reason"),
		Limit:     limit,
	}
}
