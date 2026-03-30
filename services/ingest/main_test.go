package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleEventsRejectsWhitespaceOnlyRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "source",
			body: `{"source":"   ","event_type":"tool.call","payload":{"ok":true}}`,
		},
		{
			name: "event type",
			body: `{"source":"mcp-proxy","event_type":"   ","payload":{"ok":true}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := &ingestServer{}
			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.handleEvents(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if !strings.Contains(recorder.Body.String(), `"error":"missing_fields"`) {
				t.Fatalf("body = %q, want missing_fields", recorder.Body.String())
			}
		})
	}
}

func TestHandleReadyWithoutKafkaReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	server := &ingestServer{}
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	recorder := httptest.NewRecorder()

	server.handleReady(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), `"error":"kafka_unavailable"`) {
		t.Fatalf("body = %q, want kafka_unavailable", recorder.Body.String())
	}
}
