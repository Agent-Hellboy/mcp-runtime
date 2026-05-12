package serviceutil

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestInitTracerWithoutEndpointInstallsTracePropagator(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
	})
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := InitTracer("test-service")
	if err != nil {
		t.Fatalf("InitTracer() error = %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	headers := CaptureTraceContext(ctx)
	if headers["traceparent"] == "" {
		t.Fatalf("CaptureTraceContext() missing traceparent: %#v", headers)
	}
	extracted := trace.SpanContextFromContext(ContextWithTraceContext(context.Background(), headers))
	if extracted.TraceID() != traceID {
		t.Fatalf("extracted trace ID = %s, want %s", extracted.TraceID(), traceID)
	}
	if !extracted.IsRemote() {
		t.Fatal("extracted span context should be remote")
	}
	if got := TraceIDFromContext(ctx); got != traceID.String() {
		t.Fatalf("TraceIDFromContext() = %q, want %q", got, traceID.String())
	}
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("TraceIDFromContext(background) = %q, want empty", got)
	}
}
