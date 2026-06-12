package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginAttemptTrackerPrunesIdleEntriesPeriodically(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })

	tracker.RecordFailure("client-old")
	now = now.Add(APILoginAttemptIdleTTL + time.Second)
	tracker.RecordFailure("client-new")

	if _, ok := tracker.entries["client-old"]; ok {
		t.Fatal("idle login attempt entry was not pruned")
	}
	if _, ok := tracker.entries["client-new"]; !ok {
		t.Fatal("new login attempt entry missing")
	}
}

func TestLoginAttemptTrackerDoesNotPruneOnEveryRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })
	tracker.RecordFailure("client")
	firstPrune := tracker.lastPrune

	now = now.Add(apiLoginAttemptPruneInterval / 2)
	tracker.RecordFailure("client")

	if !tracker.lastPrune.Equal(firstPrune) {
		t.Fatalf("last prune = %s, want %s", tracker.lastPrune, firstPrune)
	}
}

func TestLoginAttemptTrackerCapsEntriesAndPreservesLockedEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })

	for i := 0; i < 3; i++ {
		tracker.RecordFailure("locked")
	}
	for i := 0; i < APILoginAttemptMaxEntries; i++ {
		now = now.Add(time.Millisecond)
		tracker.RecordFailure(fmt.Sprintf("client-%d", i))
	}

	if got := len(tracker.entries); got > APILoginAttemptMaxEntries {
		t.Fatalf("login attempt entries = %d, want <= %d", got, APILoginAttemptMaxEntries)
	}
	if _, ok := tracker.entries["locked"]; !ok {
		t.Fatal("active lockout was evicted before unlocked entries")
	}
	if _, ok := tracker.entries["client-0"]; ok {
		t.Fatal("oldest unlocked entry was not evicted")
	}
}

func TestLoginAttemptTrackerNormalizesEmailSprayToIPBucket(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })

	for i := 0; i < 2; i++ {
		tracker.RecordFailure("203.0.113.10|target@example.com")
	}
	for i := 0; i < APILoginAttemptMaxEntries; i++ {
		now = now.Add(time.Millisecond)
		tracker.RecordFailure(fmt.Sprintf("203.0.113.10|dummy-%d@example.com", i))
	}

	state, ok := tracker.entries["203.0.113.10"]
	if !ok {
		t.Fatal("ip bucket entry was evicted")
	}
	if len(tracker.entries) != 1 {
		t.Fatalf("login attempt entries = %d, want 1 ip bucket", len(tracker.entries))
	}
	if state.Failures != APILoginAttemptMaxEntries+2 {
		t.Fatalf("ip bucket failures = %d, want %d", state.Failures, APILoginAttemptMaxEntries+2)
	}
}

func TestLoginAttemptTrackerAllowDoesNotCreateEntry(t *testing.T) {
	tracker := NewLoginAttemptTracker(time.Now)
	if !tracker.Allow("new-client") {
		t.Fatal("new client should be allowed")
	}
	if got := len(tracker.entries); got != 0 {
		t.Fatalf("login attempt entries = %d, want 0", got)
	}
}

func TestLoginAttemptTrackerNormalizesEmptyIPToUnknownBucket(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })

	tracker.RecordFailure("|target@example.com")
	tracker.RecordFailure("|other@example.com")

	if got := len(tracker.entries); got != 1 {
		t.Fatalf("login attempt entries = %d, want 1 unknown ip bucket", got)
	}
	state, ok := tracker.entries[loginAttemptUnknownIP]
	if !ok {
		t.Fatalf("missing unknown ip bucket, keys=%v", tracker.entries)
	}
	if state.Failures != 2 {
		t.Fatalf("unknown ip bucket failures = %d, want 2", state.Failures)
	}
}

func TestLoginAttemptTrackerAllowAtLockoutBoundary(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })
	tracker.entries["203.0.113.44"] = LoginAttempt{
		Failures:    3,
		LockedUntil: now,
		LastSeen:    now,
	}

	if !tracker.Allow("203.0.113.44|user@example.com") {
		t.Fatal("client should be allowed when lockout expires exactly at now")
	}
}

func TestLoginAttemptTrackerEvictionTargetSize(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewLoginAttemptTracker(func() time.Time { return now })

	for i := 0; i < APILoginAttemptMaxEntries+1; i++ {
		now = now.Add(time.Millisecond)
		tracker.RecordFailure(fmt.Sprintf("client-%d", i))
	}

	want := APILoginAttemptMaxEntries - apiLoginAttemptEvictionBatch
	if got := len(tracker.entries); got != want {
		t.Fatalf("login attempt entries = %d, want %d after eviction", got, want)
	}
}
