package server

// discoverToolsFromServer calls tools/list on a running MCP server and returns
// the discovered tools as --tool-spec strings (name:trust:sideEffect).
// It performs a minimal initialize → notifications/initialized → tools/list
// handshake so any server that follows the MCP spec will respond correctly.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// decodeMCPResponse reads an HTTP response that is either plain JSON or
// SSE (text/event-stream) and returns the first JSON-RPC message found.
func decodeMCPResponse(resp *http.Response) (*mcpResponse, error) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		// Parse SSE: find the first "data: {...}" line that is a JSON-RPC response.
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var r mcpResponse
			if err := json.Unmarshal([]byte(payload), &r); err == nil {
				return &r, nil
			}
		}
		return nil, fmt.Errorf("no JSON-RPC message found in SSE stream")
	}
	// Plain JSON (or unknown — try JSON first)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r mcpResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%w (body prefix: %.40s)", err, string(body))
	}
	return &r, nil
}

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type mcpToolsListResult struct {
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
}

// DiscoverToolsFromServer connects to a running MCP server at url and returns
// the tool names. They are returned as bare names; callers wrap them into
// --tool flags or metadata.ToolConfig values.
//
// If url does not end with an explicit path, /mcp is appended automatically
// because that is the default MCP endpoint path used by the go-sdk and the
// workspace-assistant-mcp example server.
func DiscoverToolsFromServer(serverURL string) ([]string, error) {
	serverURL = strings.TrimRight(serverURL, "/")
	// Append /mcp if no path segment is present (e.g. http://localhost:8088)
	if !strings.Contains(serverURL[strings.Index(serverURL, "://")+3:], "/") {
		serverURL += "/mcp"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	post := func(req mcpRequest) (*mcpResponse, string, error) {
		body, _ := json.Marshal(req)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		// MCP 2025-06-18 requires both application/json and text/event-stream.
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, "", fmt.Errorf("request to %s failed: %w", serverURL, err)
		}
		defer resp.Body.Close()
		sessionID := resp.Header.Get("Mcp-Session-Id")
		mcpResp, err := decodeMCPResponse(resp)
		if err != nil {
			return nil, sessionID, fmt.Errorf("decode response: %w", err)
		}
		return mcpResp, sessionID, nil
	}

	postWithSession := func(sessionID string, req mcpRequest) (*mcpResponse, error) {
		body, _ := json.Marshal(req)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			httpReq.Header.Set("Mcp-Session-Id", sessionID)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("request to %s failed: %w", serverURL, err)
		}
		defer resp.Body.Close()
		mcpResp, err := decodeMCPResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return mcpResp, nil
	}

	// decodeMCPResponse is defined outside the closures to avoid redeclaration.

	// Step 1: initialize
	initResp, sessionID, err := post(mcpRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "mcp-runtime-discover", "version": "1.0"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if initResp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s", initResp.Error.Message)
	}

	// Step 2: notifications/initialized (no response expected)
	_, _ = postWithSession(sessionID, mcpRequest{ //nolint:errcheck
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})

	// Step 3: tools/list
	toolsResp, err := postWithSession(sessionID, mcpRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
		Params:  map[string]any{},
	})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	if toolsResp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", toolsResp.Error.Message)
	}

	var result mcpToolsListResult
	if err := json.Unmarshal(toolsResp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	names := make([]string, 0, len(result.Tools))
	for _, t := range result.Tools {
		if strings.TrimSpace(t.Name) != "" {
			names = append(names, t.Name)
		}
	}
	return names, nil
}
