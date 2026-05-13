package agentadapter

import (
	"testing"
	"time"
)

func TestToolsListCacheHitsBeforeTTL(t *testing.T) {
	t.Parallel()

	c := newToolsListCache(50 * time.Millisecond)
	if c == nil {
		t.Fatal("newToolsListCache returned nil for positive ttl")
	}

	c.put("k1", []byte(`{"result":{"tools":[]}}`))
	got, ok := c.get("k1")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if string(got) != `{"result":{"tools":[]}}` {
		t.Fatalf("body = %s, want stored value", got)
	}
}

func TestToolsListCacheExpiresAfterTTL(t *testing.T) {
	t.Parallel()

	c := newToolsListCache(10 * time.Millisecond)
	c.put("k1", []byte(`{}`))
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.get("k1"); ok {
		t.Fatal("expected expiry, got hit")
	}
	if c.size() != 0 {
		t.Fatalf("size = %d, want 0 after expiry", c.size())
	}
}

func TestToolsListCacheInvalidateClearsAll(t *testing.T) {
	t.Parallel()

	c := newToolsListCache(time.Hour)
	c.put("a", []byte(`{}`))
	c.put("b", []byte(`{}`))
	c.invalidate()
	if c.size() != 0 {
		t.Fatalf("size = %d, want 0 after invalidate", c.size())
	}
}

func TestToolsListCacheDisabledWhenTTLZero(t *testing.T) {
	t.Parallel()

	if c := newToolsListCache(0); c != nil {
		t.Fatal("expected nil cache for zero ttl")
	}
	if c := newToolsListCache(-1); c != nil {
		t.Fatal("expected nil cache for negative ttl")
	}
}

func TestToolsCacheKeyVariesOnIdentity(t *testing.T) {
	t.Parallel()

	id1 := Identity{HumanID: "h1", AgentID: "a", SessionID: "s"}
	id2 := Identity{HumanID: "h2", AgentID: "a", SessionID: "s"}
	if toolsCacheKey(id1, "http://r") == toolsCacheKey(id2, "http://r") {
		t.Fatal("keys must differ when HumanID differs")
	}
	if toolsCacheKey(id1, "http://r1") == toolsCacheKey(id1, "http://r2") {
		t.Fatal("keys must differ when runtimeURL differs")
	}
}

func TestToolsListCacheNilSafe(t *testing.T) {
	t.Parallel()

	var c *toolsListCache
	// nil receiver should not panic for any method.
	c.put("k", []byte(`{}`))
	if _, ok := c.get("k"); ok {
		t.Fatal("nil cache must always miss")
	}
	c.invalidate()
	if c.size() != 0 {
		t.Fatalf("nil cache size = %d, want 0", c.size())
	}
}
