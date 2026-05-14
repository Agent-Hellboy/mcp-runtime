package agentadapter

import (
	"context"
	"errors"
	"testing"
)

func TestRequestTrackerTrackAndDone(t *testing.T) {
	t.Parallel()

	rt := newRequestTracker()
	ctx := context.Background()

	child, id := rt.track(ctx)
	if rt.size() != 1 {
		t.Fatalf("size = %d, want 1 after track", rt.size())
	}
	if child.Err() != nil {
		t.Fatal("tracked context should not be cancelled after track")
	}

	rt.done(id)
	if rt.size() != 0 {
		t.Fatalf("size = %d, want 0 after done", rt.size())
	}
	// done cancels the context so any in-flight request unblocks.
	if child.Err() == nil {
		t.Fatal("tracked context should be cancelled after done")
	}
}

func TestRequestTrackerCancelAllCancelsAllContexts(t *testing.T) {
	t.Parallel()

	rt := newRequestTracker()
	ctx := context.Background()

	child1, _ := rt.track(ctx)
	child2, _ := rt.track(ctx)
	if rt.size() != 2 {
		t.Fatalf("size = %d, want 2", rt.size())
	}

	cause := errors.New("shutdown")
	rt.cancelAll(cause)

	if rt.size() != 0 {
		t.Fatalf("size = %d, want 0 after cancelAll", rt.size())
	}
	if child1.Err() == nil {
		t.Fatal("child1 should be cancelled after cancelAll")
	}
	if child2.Err() == nil {
		t.Fatal("child2 should be cancelled after cancelAll")
	}
	if !errors.Is(context.Cause(child1), cause) {
		t.Fatalf("child1 cause = %v, want %v", context.Cause(child1), cause)
	}
}

func TestRequestTrackerDoneIsIdempotent(t *testing.T) {
	t.Parallel()

	rt := newRequestTracker()
	_, id := rt.track(context.Background())
	rt.done(id)
	rt.done(id) // second call must not panic
	if rt.size() != 0 {
		t.Fatalf("size = %d, want 0", rt.size())
	}
}

func TestRequestTrackerCancelAllIsIdempotent(t *testing.T) {
	t.Parallel()

	rt := newRequestTracker()
	rt.track(context.Background())
	rt.cancelAll(nil)
	rt.cancelAll(nil) // second call must not panic
}
