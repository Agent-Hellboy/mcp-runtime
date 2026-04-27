package clickhouse

import (
	"errors"
	"testing"
	"time"
)

type stubRowScanner struct {
	values []any
	err    error
}

func (s stubRowScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch ptr := dest[i].(type) {
		case *time.Time:
			*ptr = s.values[i].(time.Time)
		case *string:
			*ptr = s.values[i].(string)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func TestValidateDBName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "analytics_events", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "starts with digit", input: "1events", wantErr: true},
		{name: "contains dash", input: "analytics-events", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateDBName(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

func TestNormalizeEventLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int
		want  int
	}{
		{input: 0, want: 100},
		{input: -5, want: 100},
		{input: 25, want: 25},
		{input: 2000, want: 1000},
	}

	for _, tc := range tests {
		if got := normalizeEventLimit(tc.input); got != tc.want {
			t.Fatalf("normalizeEventLimit(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestScanEventRow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	scanner := stubRowScanner{
		values: []any{
			now,
			"gateway",
			"tools/call",
			"demo-one",
			"mcp-servers",
			"kind",
			"user-123",
			"ops-agent",
			"sess-1",
			"allow",
			"add",
			`{"value":1}`,
		},
	}

	var row EventRow
	if err := scanEventRow(scanner, &row); err != nil {
		t.Fatalf("scanEventRow returned error: %v", err)
	}
	if row.Timestamp != now {
		t.Fatalf("unexpected timestamp: got %v want %v", row.Timestamp, now)
	}
	if string(row.Payload) != `{"value":1}` {
		t.Fatalf("unexpected payload: %s", row.Payload)
	}
}

func TestScanEventRowWrapsInvalidJSONPayload(t *testing.T) {
	t.Parallel()

	scanner := stubRowScanner{
		values: []any{
			time.Unix(1_700_000_000, 0).UTC(),
			"gateway",
			"tools/call",
			"",
			"",
			"",
			"",
			"",
			"",
			"",
			"",
			"plain-text",
		},
	}

	var row EventRow
	if err := scanEventRow(scanner, &row); err != nil {
		t.Fatalf("scanEventRow returned error: %v", err)
	}
	if string(row.Payload) != `"plain-text"` {
		t.Fatalf("unexpected wrapped payload: %s", row.Payload)
	}
}
