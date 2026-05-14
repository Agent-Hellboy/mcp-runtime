package main

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestMessageInputForBatchPausesAtConfiguredLimit(t *testing.T) {
	input := make(chan kafka.Message)

	if got := messageInputForBatch(1, 2, input); got == nil {
		t.Fatal("messageInputForBatch() paused before batch reached limit")
	}
	if got := messageInputForBatch(2, 2, input); got != nil {
		t.Fatal("messageInputForBatch() did not pause at batch limit")
	}
	if got := messageInputForBatch(3, 2, input); got != nil {
		t.Fatal("messageInputForBatch() did not pause above batch limit")
	}
}

func TestContextFromKafkaTraceHeadersExtractsTraceparent(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
	})

	traceID := trace.TraceID{48, 47, 46, 45, 44, 43, 42, 41, 40, 39, 38, 37, 36, 35, 34, 33}
	parentSpanID := trace.SpanID{3, 6, 9, 12, 15, 18, 21, 24}
	traceparent := "00-" + traceID.String() + "-" + parentSpanID.String() + "-01"

	ctx := contextFromKafkaTraceHeaders(context.Background(), []kafka.Header{
		{Key: "TraceParent", Value: []byte(traceparent)},
	})
	spanContext := trace.SpanContextFromContext(ctx)

	if spanContext.TraceID() != traceID {
		t.Fatalf("trace ID = %s, want %s", spanContext.TraceID(), traceID)
	}
	if spanContext.SpanID() != parentSpanID {
		t.Fatalf("span ID = %s, want %s", spanContext.SpanID(), parentSpanID)
	}
	if !spanContext.IsRemote() {
		t.Fatal("span context should be remote")
	}
}
