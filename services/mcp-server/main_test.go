package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewMCPServerExposesSmokeToolAndPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := newMCPServer(&server{}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if !hasTool(tools.Tools, "aaa-ping") {
		t.Fatalf("tools/list missing aaa-ping: %#v", tools.Tools)
	}

	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if !hasPrompt(prompts.Prompts, "hello") {
		t.Fatalf("prompts/list missing hello: %#v", prompts.Prompts)
	}

	callRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "aaa-ping",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call tool aaa-ping: %v", err)
	}
	if got := firstText(callRes.Content); got != "pong" {
		t.Fatalf("aaa-ping returned %q, want %q", got, "pong")
	}

	promptRes, err := clientSession.GetPrompt(ctx, &mcp.GetPromptParams{Name: "hello"})
	if err != nil {
		t.Fatalf("get prompt hello: %v", err)
	}
	if len(promptRes.Messages) != 1 {
		t.Fatalf("hello prompt messages = %d, want 1", len(promptRes.Messages))
	}
	if got := firstText([]mcp.Content{promptRes.Messages[0].Content}); got != "Hello from the MCP example server." {
		t.Fatalf("hello prompt returned %q", got)
	}
}

func hasTool(tools []*mcp.Tool, want string) bool {
	for _, tool := range tools {
		if tool != nil && tool.Name == want {
			return true
		}
	}
	return false
}

func hasPrompt(prompts []*mcp.Prompt, want string) bool {
	for _, prompt := range prompts {
		if prompt != nil && prompt.Name == want {
			return true
		}
	}
	return false
}

func firstText(content []mcp.Content) string {
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}
