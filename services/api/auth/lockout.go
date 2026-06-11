package auth

import (
	"sort"
	"sync"
	"time"
)

const (
	APILoginLockoutBase          = 15 * time.Second
	APILoginLockoutMax           = 5 * time.Minute
	APILoginAttemptIdleTTL       = 30 * time.Minute
	APILoginAttemptMaxEntries    = 4096
	apiLoginAttemptPruneInterval = time.Minute
	apiLoginAttemptEvictionBatch = 256
)

type LoginAttempt struct {
	Failures    int
	LockedUntil time.Time
	LastSeen    time.Time
}

type LoginAttemptTracker struct {
	mu        sync.Mutex
	nowFunc   func() time.Time
	entries   map[string]LoginAttempt
	lastPrune time.Time
}

func NewLoginAttemptTracker(nowFn func() time.Time) *LoginAttemptTracker {
	return &LoginAttemptTracker{
		nowFunc: nowFn,
		entries: map[string]LoginAttempt{},
	}
}

func (t *LoginAttemptTracker) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	t.pruneIfDueLocked(now)
	state, ok := t.entries[key]
	if !ok {
		return true
	}
	state.LastSeen = now
	t.entries[key] = state
	return state.LockedUntil.IsZero() || !state.LockedUntil.After(now)
}

func (t *LoginAttemptTracker) RecordFailure(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	t.pruneIfDueLocked(now)
	state := t.entries[key]
	state.Failures++
	state.LockedUntil = now.Add(lockoutDurationForFailures(state.Failures))
	state.LastSeen = now
	t.entries[key] = state
	t.enforceMaxLocked(now)
	return state.Failures
}

func (t *LoginAttemptTracker) RecordSuccess(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneIfDueLocked(t.nowFunc())
	state := t.entries[key]
	failures := state.Failures
	delete(t.entries, key)
	return failures
}

func (t *LoginAttemptTracker) pruneIfDueLocked(now time.Time) {
	if !t.lastPrune.IsZero() && now.Sub(t.lastPrune) < apiLoginAttemptPruneInterval {
		return
	}
	t.lastPrune = now
	for key, state := range t.entries {
		if now.Sub(state.LastSeen) > APILoginAttemptIdleTTL && !state.LockedUntil.After(now) {
			delete(t.entries, key)
		}
	}
}

func (t *LoginAttemptTracker) enforceMaxLocked(now time.Time) {
	if len(t.entries) <= APILoginAttemptMaxEntries {
		return
	}
	target := APILoginAttemptMaxEntries - apiLoginAttemptEvictionBatch
	if target < 0 {
		target = 0
	}
	type candidate struct {
		key      string
		lastSeen time.Time
		locked   bool
	}
	candidates := make([]candidate, 0, len(t.entries))
	for key, state := range t.entries {
		candidates = append(candidates, candidate{
			key:      key,
			lastSeen: state.LastSeen,
			locked:   state.LockedUntil.After(now),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].locked != candidates[j].locked {
			return !candidates[i].locked
		}
		return candidates[i].lastSeen.Before(candidates[j].lastSeen)
	})
	for _, entry := range candidates[:len(t.entries)-target] {
		delete(t.entries, entry.key)
	}
}

func lockoutDurationForFailures(failures int) time.Duration {
	if failures <= 2 {
		return 0
	}
	steps := failures - 2
	lockout := APILoginLockoutBase
	for i := 1; i < steps; i++ {
		lockout *= 2
		if lockout >= APILoginLockoutMax {
			return APILoginLockoutMax
		}
	}
	if lockout > APILoginLockoutMax {
		return APILoginLockoutMax
	}
	return lockout
}
