package auth

import (
	"sync"
	"time"
)

const (
	APILoginLockoutBase = 15 * time.Second
	APILoginLockoutMax  = 5 * time.Minute
)

type LoginAttempt struct {
	Failures    int
	LockedUntil time.Time
}

type LoginAttemptTracker struct {
	mu      sync.Mutex
	nowFunc func() time.Time
	entries map[string]LoginAttempt
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
	state := t.entries[key]
	now := t.nowFunc()
	return state.LockedUntil.IsZero() || !state.LockedUntil.After(now)
}

func (t *LoginAttemptTracker) RecordFailure(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	state := t.entries[key]
	state.Failures++
	state.LockedUntil = now.Add(lockoutDurationForFailures(state.Failures))
	t.entries[key] = state
	return state.Failures
}

func (t *LoginAttemptTracker) RecordSuccess(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.entries[key]
	failures := state.Failures
	delete(t.entries, key)
	return failures
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
