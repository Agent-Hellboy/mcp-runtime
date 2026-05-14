package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcp-runtime/pkg/events"
)

func BenchmarkEmitAnalyticsEvent(b *testing.B) {
	ingest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	b.Cleanup(ingest.Close)

	proxy := &gatewayServer{
		analyticsURL: ingest.URL,
		httpClient:   ingest.Client(),
	}
	event, err := events.NewEnvelope(
		"benchmark-proxy",
		"mcp.request",
		map[string]any{
			"method":     http.MethodPost,
			"path":       "/mcp",
			"status":     http.StatusAccepted,
			"latency_ms": int64(12),
			"bytes_in":   int64(128),
			"bytes_out":  256,
			"rpc_method": "tools/call",
			"tool_name":  "echo",
		},
		time.Now().UTC(),
	)
	if err != nil {
		b.Fatalf("NewEnvelope() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		proxy.emit(ctx, event)
		cancel()
	}
}
