package events

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNewEnvelope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	envelope, err := NewEnvelope("mcp-proxy", "mcp.request", map[string]any{"ok": true}, now)
	if err != nil {
		t.Fatalf("NewEnvelope() error = %v", err)
	}
	if envelope.Timestamp != now.Format(time.RFC3339Nano) {
		t.Fatalf("timestamp = %q, want %q", envelope.Timestamp, now.Format(time.RFC3339Nano))
	}
	if !json.Valid(envelope.Payload) {
		t.Fatalf("payload is not valid JSON: %s", envelope.Payload)
	}
}

func TestEnvelopeValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		event   Envelope
		wantErr bool
	}{
		{name: "valid", event: Envelope{Source: "mcp-proxy", EventType: "mcp.request", Payload: json.RawMessage(`{"ok":true}`)}},
		{name: "missing source", event: Envelope{EventType: "mcp.request", Payload: json.RawMessage(`{"ok":true}`)}, wantErr: true},
		{name: "missing event type", event: Envelope{Source: "mcp-proxy", Payload: json.RawMessage(`{"ok":true}`)}, wantErr: true},
		{name: "missing payload", event: Envelope{Source: "mcp-proxy", EventType: "mcp.request"}, wantErr: true},
		{name: "null payload", event: Envelope{Source: "mcp-proxy", EventType: "mcp.request", Payload: json.RawMessage(` null `)}, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.event.Validate()
			if tc.wantErr && !errors.Is(err, ErrMissingFields) {
				t.Fatalf("Validate() error = %v, want ErrMissingFields", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestTimestampTimeFallsBackOnMalformedTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	got := (Envelope{Timestamp: "bad"}).TimestampTime(now)
	if !got.Equal(now) {
		t.Fatalf("TimestampTime() = %v, want %v", got, now)
	}
}

func TestSetTraceID(t *testing.T) {
	t.Parallel()

	envelope := Envelope{}
	envelope.SetTraceID("  30646a2d2c2b2a292827262524232221  ")
	if envelope.TraceID != "30646a2d2c2b2a292827262524232221" {
		t.Fatalf("TraceID = %q, want trimmed trace ID", envelope.TraceID)
	}
	envelope.SetTraceID("  ")
	if envelope.TraceID != "30646a2d2c2b2a292827262524232221" {
		t.Fatalf("TraceID changed on empty input: %q", envelope.TraceID)
	}
}
