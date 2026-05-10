package agentadapter

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPProxyInjectsGovernanceHeadersAndPreservesMCPHeaders(t *testing.T) {
	t.Parallel()

	var upstreamHost string
	var upstreamPath string
	var upstreamQuery string
	var upstreamHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHost = r.Host
		upstreamPath = r.URL.Path
		upstreamQuery = r.URL.RawQuery
		upstreamHeaders = r.Header.Clone()
		w.Header().Set(MCPSessionHeader, "runtime-mcp-session")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL + "/go-example-mcp/mcp?source=runtime")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	handler, err := NewHTTPProxyHandler(Config{
		RuntimeURL: target,
		HumanID:    "support-lead",
		AgentID:    "ticket-triage-agent",
		SessionID:  "sess-ticket-triage-agent",
		HostHeader: "mcp.example.local",
	})
	if err != nil {
		t.Fatalf("NewHTTPProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp?client=true", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolHeader, "2025-06-18")
	req.Header.Set(MCPSessionHeader, "client-mcp-session")
	req.Header.Set(HumanIDHeader, "spoofed-human")
	req.Header.Set(AgentIDHeader, "spoofed-agent")
	req.Header.Set(AgentSessionHeader, "spoofed-session")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if upstreamHost != "mcp.example.local" {
		t.Fatalf("upstream host = %q, want host header override", upstreamHost)
	}
	if upstreamPath != "/go-example-mcp/mcp" {
		t.Fatalf("upstream path = %q, want exact runtime route", upstreamPath)
	}
	if upstreamQuery != "source=runtime&client=true" {
		t.Fatalf("upstream query = %q, want merged query", upstreamQuery)
	}
	assertHeader(t, upstreamHeaders, HumanIDHeader, "support-lead")
	assertHeader(t, upstreamHeaders, AgentIDHeader, "ticket-triage-agent")
	assertHeader(t, upstreamHeaders, AgentSessionHeader, "sess-ticket-triage-agent")
	assertHeader(t, upstreamHeaders, MCPProtocolHeader, "2025-06-18")
	assertHeader(t, upstreamHeaders, MCPSessionHeader, "client-mcp-session")
	assertHeader(t, upstreamHeaders, "content-type", "application/json")
	assertHeader(t, upstreamHeaders, "accept", "application/json, text/event-stream")
	if got := recorder.Header().Get(MCPSessionHeader); got != "runtime-mcp-session" {
		t.Fatalf("response %s = %q, want runtime-mcp-session", MCPSessionHeader, got)
	}
}

func TestHTTPProxyPropagatesRuntimeDenial(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"trust_too_low"}`))
	}))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	handler, err := NewHTTPProxyHandler(testConfig(target))
	if err != nil {
		t.Fatalf("NewHTTPProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"upper"}}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if strings.TrimSpace(string(body)) != `{"error":"trust_too_low"}` {
		t.Fatalf("body = %q, want trust_too_low denial", strings.TrimSpace(string(body)))
	}
}

func testConfig(runtimeURL *url.URL) Config {
	return Config{
		RuntimeURL: runtimeURL,
		HumanID:    "human-1",
		AgentID:    "agent-1",
		SessionID:  "session-1",
	}
}

func assertHeader(t *testing.T, headers http.Header, name, want string) {
	t.Helper()
	if got := headers.Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}
