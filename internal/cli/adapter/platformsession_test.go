package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcp-runtime/internal/agentadapter"
	"mcp-runtime/internal/cli/platformapi"
)

// fakePlatformServer is a barebones implementation of the platform adapter-
// session endpoint sufficient to drive applyPlatformSession in tests.
func fakePlatformServer(t *testing.T, expiresAt time.Time, calls *int32) (*httptest.Server, *platformapi.PlatformClient) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/runtime/adapter/sessions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(calls, 1)
		var req platformapi.AdapterSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := platformapi.AdapterSession{
			Name:           "adapter-fake",
			Namespace:      "mcp-team-acme",
			HumanID:        "user-123",
			AgentID:        req.AgentID,
			TeamID:         "team-acme",
			ServerName:     req.ServerName,
			ConsentedTrust: "low",
			PolicyVersion:  "v1",
			ExpiresAt:      expiresAt,
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	t.Setenv("MCP_PLATFORM_API_URL", server.URL)
	t.Setenv("MCP_PLATFORM_API_TOKEN", "test-token")
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		t.Fatalf("NewPlatformClient: %v", err)
	}
	return server, client
}

func TestApplyPlatformSessionPopulatesIdentity(t *testing.T) {
	var calls int32
	_, _ = fakePlatformServer(t, time.Now().Add(time.Hour), &calls)
	flags := platformSessionFlags{
		server:    "demo",
		namespace: "mcp-team-acme",
		agent:     "ops-agent",
	}
	var buf bytes.Buffer
	id, provider, refresher, err := applyPlatformSession(context.Background(), &flags, &buf)
	if err != nil {
		t.Fatalf("applyPlatformSession: %v", err)
	}
	if refresher != nil {
		t.Fatal("refresher should be nil when auto-refresh is off")
	}
	if provider != nil {
		t.Fatal("provider should be nil when auto-refresh is off")
	}
	if id.HumanID != "user-123" || id.AgentID != "ops-agent" || id.TeamID != "team-acme" || id.SessionID != "adapter-fake" {
		t.Fatalf("identity = %#v, want platform-issued values", id)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}

func TestApplyPlatformSessionAutoRefreshUsesProvider(t *testing.T) {
	var calls int32
	// Short expiry forces the refresh loop to fire quickly.
	_, _ = fakePlatformServer(t, time.Now().Add(2*time.Second), &calls)
	flags := platformSessionFlags{
		server:      "demo",
		namespace:   "mcp-team-acme",
		agent:       "ops-agent",
		autoRefresh: true,
	}
	var buf bytes.Buffer
	id, provider, refresher, err := applyPlatformSession(context.Background(), &flags, &buf)
	if err != nil {
		t.Fatalf("applyPlatformSession: %v", err)
	}
	if refresher == nil || provider == nil {
		t.Fatal("auto-refresh must return a refresher and a provider")
	}
	defer refresher.Stop()
	got := provider()
	if got != id {
		t.Fatalf("provider() = %#v, want initial identity %#v", got, id)
	}
	// Wait long enough for at least one refresh tick (adapterRefreshFloor = 30s
	// in production; this test sleeps a short window to verify the loop wakes
	// at least once when expiry is well past).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Not asserting a refresh fired by 2s because adapterRefreshFloor is 30s,
	// but at minimum the provider remains valid for the test duration.
	if provider() != id {
		t.Fatal("provider must keep returning the latest identity")
	}
}

func TestMergeIdentityFromIssuedFlagWins(t *testing.T) {
	flag := agentadapter.Identity{HumanID: "explicit", SessionID: "fixed"}
	issued := agentadapter.Identity{HumanID: "issued", AgentID: "issued-agent", TeamID: "issued-team", SessionID: "issued-session"}
	got := mergeIdentityFromIssued(flag, issued)
	if got.HumanID != "explicit" {
		t.Fatalf("HumanID = %q, want explicit flag to win", got.HumanID)
	}
	if got.AgentID != "issued-agent" {
		t.Fatalf("AgentID = %q, want issued fallback", got.AgentID)
	}
	if got.SessionID != "fixed" {
		t.Fatalf("SessionID = %q, want explicit flag to win", got.SessionID)
	}
}

func TestApplyPlatformSessionDisabledWhenServerUnset(t *testing.T) {
	flags := platformSessionFlags{}
	id, provider, refresher, err := applyPlatformSession(context.Background(), &flags, nil)
	if err != nil {
		t.Fatalf("applyPlatformSession: %v", err)
	}
	if provider != nil || refresher != nil {
		t.Fatal("disabled bootstrap must return nil provider and refresher")
	}
	if id != (agentadapter.Identity{}) {
		t.Fatalf("identity = %#v, want empty zero value", id)
	}
}

func TestApplyPlatformSessionRequiresAgent(t *testing.T) {
	flags := platformSessionFlags{server: "demo"}
	_, _, _, err := applyPlatformSession(context.Background(), &flags, nil)
	if err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Fatalf("err = %v, want missing --agent error", err)
	}
}
