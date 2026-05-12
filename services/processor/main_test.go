package main

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

func TestClickHouseBatchTraceAttributesIncludeKafkaBoundaries(t *testing.T) {
	attrs := clickhouseBatchTraceAttributes([]kafka.Message{
		{Topic: "mcp.events", Partition: 2, Offset: 10},
		{Topic: "mcp.events", Partition: 2, Offset: 12},
		{Topic: "mcp.events", Partition: 3, Offset: 4},
	}, 3)
	values := attributeMap(attrs)

	if got := values["batch.size"].AsInt64(); got != 3 {
		t.Fatalf("batch.size = %d, want 3", got)
	}
	if got := values["kafka.first_offset"].AsInt64(); got != 4 {
		t.Fatalf("kafka.first_offset = %d, want 4", got)
	}
	if got := values["kafka.last_offset"].AsInt64(); got != 12 {
		t.Fatalf("kafka.last_offset = %d, want 12", got)
	}
	if got := values["kafka.partition_count"].AsInt64(); got != 2 {
		t.Fatalf("kafka.partition_count = %d, want 2", got)
	}
	if got := values["kafka.partitions"].AsString(); got != "2,3" {
		t.Fatalf("kafka.partitions = %q, want 2,3", got)
	}
	if got := values["kafka.offset_ranges"].AsString(); got != "mcp.events/2:10-12,mcp.events/3:4-4" {
		t.Fatalf("kafka.offset_ranges = %q, want per-partition ranges", got)
	}
}

func attributeMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value
	}
	return out
}
