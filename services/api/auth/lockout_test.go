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

func TestLoginAttemptTrackerAllowDoesNotCreateEntry(t *testing.T) {
	tracker := NewLoginAttemptTracker(time.Now)
	if !tracker.Allow("new-client") {
		t.Fatal("new client should be allowed")
	}
	if got := len(tracker.entries); got != 0 {
		t.Fatalf("login attempt entries = %d, want 0", got)
	}
}
