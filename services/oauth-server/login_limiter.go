package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit = 5
	loginBackoff      = time.Minute
	loginMaxBackoff   = 15 * time.Minute
)

type loginAttempt struct {
	failures    int
	blockedTill time.Time
	updatedAt   time.Time
}

// loginAttemptLimiter is intentionally local to an OAuth pod. Deployments
// with multiple replicas should add an ingress or shared rate limiter too;
// this protects the password verifier even when that layer is absent.
type loginAttemptLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginAttemptLimiter() *loginAttemptLimiter {
	return &loginAttemptLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginAttemptLimiter) allowed(keys ...string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	for _, key := range keys {
		if attempt, ok := l.attempts[key]; ok && now.Before(attempt.blockedTill) {
			return false
		}
	}
	return true
}

func (l *loginAttemptLimiter) failure(keys ...string) {
	if l == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	for _, key := range keys {
		if key == "" {
			continue
		}
		attempt := l.attempts[key]
		attempt.failures++
		attempt.updatedAt = now
		if attempt.failures >= loginFailureLimit {
			backoff := loginBackoff
			for i := loginFailureLimit; i < attempt.failures && backoff < loginMaxBackoff; i++ {
				backoff *= 2
			}
			if backoff > loginMaxBackoff {
				backoff = loginMaxBackoff
			}
			attempt.blockedTill = now.Add(backoff)
		}
		l.attempts[key] = attempt
	}
}

func (l *loginAttemptLimiter) success(keys ...string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.attempts, key)
	}
}

func (l *loginAttemptLimiter) pruneLocked(now time.Time) {
	if len(l.attempts) < 4096 {
		return
	}
	for key, attempt := range l.attempts {
		if now.Sub(attempt.updatedAt) > loginMaxBackoff {
			delete(l.attempts, key)
		}
	}
}

func loginAttemptKeys(r *http.Request, email string) []string {
	keys := make([]string, 0, 2)
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		keys = append(keys, "email:"+email)
	}
	if ip := requestRemoteIP(r); ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	return keys
}

func requestRemoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.TrimSpace(r.RemoteAddr))
}
