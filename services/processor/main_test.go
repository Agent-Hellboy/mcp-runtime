package main

import (
	"testing"

	"github.com/segmentio/kafka-go"
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
