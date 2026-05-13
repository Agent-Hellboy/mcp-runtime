package agentadapter

import (
	"strings"
	"sync"
	"time"
)

// toolsListCache stores recent tools/list response bodies keyed by the caller's
// governance identity and the runtime URL. It is intended to absorb the chatty
// tools/list calls some agent SDKs make every conversation turn without
// hitting the runtime each time.
//
// Cache invariants:
//   - Anonymous mode bypasses the cache entirely: the runtime can serve
//     different policy versions to anonymous callers and we have no way to
//     key on them.
//   - Each entry is short-lived (caller-supplied TTL, typically 30 s). The
//     gateway is the source of truth for grants; the cache is an optimisation.
//   - On a tools/list_changed notification from the runtime, all entries
//     are invalidated.
type toolsListCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]toolsCacheEntry
}

type toolsCacheEntry struct {
	body    []byte
	expires time.Time
}

func newToolsListCache(ttl time.Duration) *toolsListCache {
	if ttl <= 0 {
		return nil
	}
	return &toolsListCache{
		ttl:     ttl,
		entries: make(map[string]toolsCacheEntry),
	}
}

func (c *toolsListCache) get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	// Return a copy so the caller can mutate freely.
	out := make([]byte, len(entry.body))
	copy(out, entry.body)
	return out, true
}

func (c *toolsListCache) put(key string, body []byte) {
	if c == nil || len(body) == 0 {
		return
	}
	stored := make([]byte, len(body))
	copy(stored, body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = toolsCacheEntry{
		body:    stored,
		expires: time.Now().Add(c.ttl),
	}
}

// invalidate drops every entry — used when the runtime emits
// notifications/tools/list_changed.
func (c *toolsListCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		delete(c.entries, k)
	}
}

func (c *toolsListCache) size() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// toolsCacheKey deterministically combines the identity and runtime URL into a
// cache key. The trailing "|" separators avoid collisions when one field is
// empty (e.g. anonymous mode would have empty identity fields — but that path
// short-circuits before reaching this function).
func toolsCacheKey(id Identity, runtimeURL string) string {
	var b strings.Builder
	b.Grow(len(id.HumanID) + len(id.AgentID) + len(id.TeamID) + len(id.SessionID) + len(runtimeURL) + 8)
	b.WriteString(id.HumanID)
	b.WriteByte('|')
	b.WriteString(id.AgentID)
	b.WriteByte('|')
	b.WriteString(id.TeamID)
	b.WriteByte('|')
	b.WriteString(id.SessionID)
	b.WriteByte('|')
	b.WriteString(runtimeURL)
	return b.String()
}
