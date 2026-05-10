package agentadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type capturedRuntimeRequest struct {
	Host    string
	Headers http.Header
	Body    string
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

	err = RunStdioShim(context.Background(), Config{
		RuntimeURL:      runtimeURL,
		HumanID:         "support-lead",
		AgentID:         "ticket-triage-agent",
		SessionID:       "sess-ticket-triage-agent",
		HostHeader:      "mcp.example.local",
		ProtocolVersion: "2025-01-01",
		HTTPClient:      upstream.Client(),
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
	err = RunStdioShim(context.Background(), Config{
		RuntimeURL: runtimeURL,
		HumanID:    "human-1",
		AgentID:    "agent-1",
		SessionID:  "session-1",
		HTTPClient: upstream.Client(),
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
	err = RunStdioShim(context.Background(), Config{
		RuntimeURL: runtimeURL,
		HumanID:    "human-1",
		AgentID:    "agent-1",
		SessionID:  "session-1",
		HTTPClient: upstream.Client(),
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

func TestDecodeSSEDataMessages(t *testing.T) {
	t.Parallel()

	messages := decodeSSEDataMessages([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
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
