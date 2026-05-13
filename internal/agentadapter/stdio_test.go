package agentadapter

import (
	"bufio"
	"bytes"
	"context"
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

type capturedRuntimeRequest struct {
	Host    string
	Headers http.Header
	Body    string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRunStdioShimInjectsHeadersAndMaintainsRuntimeMCPSession(t *testing.T) {
	t.Parallel()

	var requests []capturedRuntimeRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, capturedRuntimeRequest{
			Host:    r.Host,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		switch len(requests) {
		case 1:
			w.Header().Set(MCPSessionHeader, "runtime-mcp-session")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"5"}]}}`))
		}
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer

	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "support-lead",
			AgentID:   "ticket-triage-agent",
			TeamID:    "team-acme",
			SessionID: "sess-ticket-triage-agent",
		},
		HostHeader:      "mcp.example.local",
		ProtocolVersion: "2025-01-01",
		Transport:       &RuntimeTransport{Base: upstream.Client().Transport},
	}, StdioOptions{
		Stdin:  strings.NewReader(input),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}

	lines := nonEmptyLines(output.String())
	if len(lines) != 2 {
		t.Fatalf("output lines = %#v, want 2 responses", lines)
	}
	if len(requests) != 2 {
		t.Fatalf("runtime requests = %d, want 2", len(requests))
	}
	first := requests[0]
	if first.Host != "mcp.example.local" {
		t.Fatalf("first host = %q, want host override", first.Host)
	}
	assertHeader(t, first.Headers, HumanIDHeader, "support-lead")
	assertHeader(t, first.Headers, AgentIDHeader, "ticket-triage-agent")
	assertHeader(t, first.Headers, TeamIDHeader, "team-acme")
	assertHeader(t, first.Headers, AgentSessionHeader, "sess-ticket-triage-agent")
	assertHeader(t, first.Headers, MCPProtocolHeader, "2025-06-18")
	if got := first.Headers.Get(MCPSessionHeader); got != "" {
		t.Fatalf("first %s = %q, want empty before initialize response", MCPSessionHeader, got)
	}
	assertHeader(t, requests[1].Headers, MCPSessionHeader, "runtime-mcp-session")
	assertHeader(t, requests[1].Headers, MCPProtocolHeader, "2025-06-18")
}

func TestRunStdioShimConvertsHTTPDenialToJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"trust_too_low"}`))
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	var output bytes.Buffer
	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Base: upstream.Client().Transport},
	}, StdioOptions{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"upper"}}` + "\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}

	var response rpcErrorResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v; output=%s", err, output.String())
	}
	if string(response.ID) != `"call-1"` {
		t.Fatalf("id = %s, want call-1", response.ID)
	}
	if response.Error.Message != "trust_too_low" {
		t.Fatalf("error message = %q, want trust_too_low", response.Error.Message)
	}
	if response.Error.Data["http_status"] != float64(http.StatusForbidden) {
		t.Fatalf("http_status = %#v, want %d", response.Error.Data["http_status"], http.StatusForbidden)
	}
}

func TestRunStdioShimLogsRuntimeDenialWhenInfoEnabled(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"trust_too_low"}`))
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	var output bytes.Buffer
	var logs bytes.Buffer
	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Base: upstream.Client().Transport},
		LogLevel:  "info",
		LogWriter: &logs,
	}, StdioOptions{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"upper"}}` + "\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}
	logLine := logs.String()
	for _, want := range []string{"mcp-runtime-mcp-shim:", "403", "trust_too_low", "method=tools/call", "tool=upper"} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log line = %q, want %q", logLine, want)
		}
	}
}

func TestRunStdioShimAppliesRequestTimeout(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"call-1","result":{}}`))
		}
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	var output bytes.Buffer
	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Timeout: 10 * time.Millisecond},
	}, StdioOptions{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"upper"}}` + "\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}

	var response rpcErrorResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v; output=%s", err, output.String())
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

func TestRunStdioShimSuppressesHTTPRequestErrorAfterContextCancellation(t *testing.T) {
	t.Parallel()

	runtimeURL, err := url.Parse("http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			cancel()
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}
	var output bytes.Buffer
	err = RunStdioShim(ctx, ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Base: client.Transport},
	}, StdioOptions{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"upper"}}` + "\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no synthetic error during shutdown", output.String())
	}
}

func TestRunStdioShimStreamsEventsAndContinuesReadingStdin(t *testing.T) {
	t.Parallel()

	secondRequest := make(chan string, 1)
	releaseFinal := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFinal) }) }
	defer release()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		bodyText := string(body)
		if strings.Contains(bodyText, `"id":"server-1"`) {
			secondRequest <- bodyText
			w.WriteHeader(http.StatusAccepted)
			release()
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("response writer does not implement http.Flusher")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":\"server-1\",\"method\":\"sampling/createMessage\",\"params\":{}}\n\n"))
		flusher.Flush()
		select {
		case <-releaseFinal:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":\"client-1\",\"result\":{\"ok\":true}}\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	stdin, stdinWriter := io.Pipe()
	stdoutReader, stdout := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer stdout.Close()
		done <- RunStdioShim(ctx, ShimConfig{
			RuntimeURL: runtimeURL,
			Identity: Identity{
				HumanID:   "human-1",
				AgentID:   "agent-1",
				SessionID: "session-1",
			},
			Transport: &RuntimeTransport{Base: upstream.Client().Transport},
		}, StdioOptions{
			Stdin:  stdin,
			Stdout: stdout,
		})
	}()

	if _, err := stdinWriter.Write([]byte(`{"jsonrpc":"2.0","id":"client-1","method":"tools/call","params":{"name":"upper"}}` + "\n")); err != nil {
		t.Fatalf("stdin Write() error = %v", err)
	}
	reader := bufio.NewReader(stdoutReader)
	firstLine := readLineWithin(t, reader, 2*time.Second)
	if !strings.Contains(firstLine, `"id":"server-1"`) {
		t.Fatalf("first stdout line = %q, want streamed server request", firstLine)
	}

	if _, err := stdinWriter.Write([]byte(`{"jsonrpc":"2.0","id":"server-1","result":{"content":[]}}` + "\n")); err != nil {
		t.Fatalf("stdin follow-up Write() error = %v", err)
	}
	select {
	case body := <-secondRequest:
		if !strings.Contains(body, `"id":"server-1"`) {
			t.Fatalf("second runtime request = %q, want server-1 response", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shim to forward follow-up stdin message")
	}

	finalLine := readLineWithin(t, reader, 2*time.Second)
	if !strings.Contains(finalLine, `"id":"client-1"`) {
		t.Fatalf("final stdout line = %q, want original request response", finalLine)
	}
	cancel()
	_ = stdinWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStdioShim() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunStdioShim() did not exit after cancellation")
	}
}

func TestRunStdioShimDoesNotWriteResponseForNotificationAcceptedByHTTP(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	var output bytes.Buffer
	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Base: upstream.Client().Transport},
	}, StdioOptions{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty for notification", output.String())
	}
}

func TestRunStdioShimReturnsParseErrorForMalformedJSON(t *testing.T) {
	t.Parallel()

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	runtimeURL, err := url.Parse(upstream.URL + "/go-example-mcp/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	var output bytes.Buffer
	err = RunStdioShim(context.Background(), ShimConfig{
		RuntimeURL: runtimeURL,
		Identity: Identity{
			HumanID:   "human-1",
			AgentID:   "agent-1",
			SessionID: "session-1",
		},
		Transport: &RuntimeTransport{Base: upstream.Client().Transport},
	}, StdioOptions{
		Stdin:  strings.NewReader("{not-json\n"),
		Stdout: &output,
	})
	if err != nil {
		t.Fatalf("RunStdioShim() error = %v", err)
	}
	if upstreamCalled {
		t.Fatal("malformed JSON should not be forwarded upstream")
	}

	var response rpcErrorResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v; output=%s", err, output.String())
	}
	if string(response.ID) != "null" {
		t.Fatalf("id = %s, want null", response.ID)
	}
	if response.Error.Code != -32700 {
		t.Fatalf("error code = %d, want -32700", response.Error.Code)
	}
	if response.Error.Message != "parse error" {
		t.Fatalf("error message = %q, want parse error", response.Error.Message)
	}
}

func TestRunStdioShimReturnsWhenContextCancelledWhileIdle(t *testing.T) {
	t.Parallel()

	runtimeURL, err := url.Parse("http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	stdin, stdinWriter := io.Pipe()
	t.Cleanup(func() { _ = stdinWriter.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- RunStdioShim(ctx, ShimConfig{
			RuntimeURL: runtimeURL,
			Identity: Identity{
				HumanID:   "human-1",
				AgentID:   "agent-1",
				SessionID: "session-1",
			},
		}, StdioOptions{
			Stdin:  stdin,
			Stdout: io.Discard,
		})
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStdioShim() error = %v, want nil after context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunStdioShim() did not return after context cancellation")
	}
}

func TestDecodeStreamableHTTPEventMessages(t *testing.T) {
	t.Parallel()

	messages := decodeStreamableHTTPEventMessages([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want one message", messages)
	}
	if string(messages[0]) != `{"jsonrpc":"2.0","id":1,"result":{}}` {
		t.Fatalf("message = %s", messages[0])
	}
}

func nonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func readLineWithin(t *testing.T, reader *bufio.Reader, timeout time.Duration) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("ReadString() error = %v", got.err)
		}
		return got.line
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for stdout line")
		return ""
	}
}
