package agentadapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
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
		TeamID:     "team-acme",
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
	req.Header.Set(TeamIDHeader, "spoofed-team")
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
	assertHeader(t, upstreamHeaders, TeamIDHeader, "team-acme")
	assertHeader(t, upstreamHeaders, AgentSessionHeader, "sess-ticket-triage-agent")
	assertHeader(t, upstreamHeaders, MCPProtocolHeader, "2025-06-18")
	assertHeader(t, upstreamHeaders, MCPSessionHeader, "client-mcp-session")
	assertHeader(t, upstreamHeaders, "content-type", "application/json")
	assertHeader(t, upstreamHeaders, "accept", "application/json, text/event-stream")
	if got := recorder.Header().Get(MCPSessionHeader); got != "runtime-mcp-session" {
		t.Fatalf("response %s = %q, want runtime-mcp-session", MCPSessionHeader, got)
	}
}

func TestHTTPProxyStripsSpoofedTeamHeaderWhenUnset(t *testing.T) {
	t.Parallel()

	var upstreamHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
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

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set(TeamIDHeader, "spoofed-team")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if got := upstreamHeaders.Get(TeamIDHeader); got != "" {
		t.Fatalf("upstream %s = %q, want empty when adapter TeamID is unset", TeamIDHeader, got)
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

func TestHTTPProxyLogsRuntimeDenialWhenInfoEnabled(t *testing.T) {
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
	var logs bytes.Buffer
	cfg := testConfig(target)
	cfg.LogLevel = "info"
	cfg.LogWriter = &logs
	handler, err := NewHTTPProxyHandler(cfg)
	if err != nil {
		t.Fatalf("NewHTTPProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"upper"}}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	logLine := logs.String()
	for _, want := range []string{"mcp-runtime-agent-proxy:", "403", "trust_too_low", "method=tools/call", "tool=upper"} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log line = %q, want %q", logLine, want)
		}
	}
}

func TestHTTPProxyConvertsUpstreamConnectionFailureToJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	upstream.Close()

	handler, err := NewHTTPProxyHandler(testConfig(target))
	if err != nil {
		t.Fatalf("NewHTTPProxyHandler() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"upper"}}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("content-type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content-type = %q, want application/json", contentType)
	}
	var response rpcErrorResponse
	if err := json.Unmarshal(bytes.TrimSpace(recorder.Body.Bytes()), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v; body=%s", err, recorder.Body.String())
	}
	if string(response.ID) != `"call-1"` {
		t.Fatalf("id = %s, want call-1", response.ID)
	}
	if response.Error.Code != -32000 {
		t.Fatalf("error code = %d, want -32000", response.Error.Code)
	}
	if response.Error.Data["http_status"] != float64(http.StatusBadGateway) {
		t.Fatalf("http_status = %#v, want %d", response.Error.Data["http_status"], http.StatusBadGateway)
	}
}

func TestHTTPProxyCanSuppressXForwardedHeaders(t *testing.T) {
	t.Parallel()

	var upstreamHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	cfg := testConfig(target)
	cfg.DisableXForwarded = true
	handler, err := NewHTTPProxyHandler(cfg)
	if err != nil {
		t.Fatalf("NewHTTPProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8099/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if got := upstreamHeaders.Get(header); got != "" {
			t.Fatalf("%s = %q, want empty when X-Forwarded headers are disabled", header, got)
		}
	}
}

func TestHTTPProxyFlushesStreamableHTTPEventFrames(t *testing.T) {
	t.Parallel()

	releaseSecond := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSecond) }) }
	defer release()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not implement http.Flusher")
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"step\":1}}\n\n"))
		flusher.Flush()
		<-releaseSecond
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"step\":2}}\n\n"))
		flusher.Flush()
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
	proxy := httptest.NewServer(handler)
	t.Cleanup(proxy.Close)

	resp, err := proxy.Client().Post(proxy.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"upper"}}`))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	firstLineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		firstLineCh <- line
	}()

	select {
	case line := <-firstLineCh:
		if !strings.Contains(line, `"step":1`) {
			t.Fatalf("first event line = %q, want step 1", line)
		}
	case err := <-errCh:
		t.Fatalf("ReadString() error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event before upstream response completed")
	}

	release()
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(rest), `"step":2`) {
		t.Fatalf("remaining stream = %q, want step 2", string(rest))
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
