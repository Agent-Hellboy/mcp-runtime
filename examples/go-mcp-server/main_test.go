package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewMCPServerExposesSmokeSurface(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := newMCPServer().Connect(ctx, serverTransport, nil)
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
	for _, want := range []string{"aaa-ping", "echo", "add", "upper", "lower", "slugify"} {
		if !hasTool(tools.Tools, want) {
			t.Fatalf("tools/list missing %s: %#v", want, tools.Tools)
		}
	}

	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	for _, want := range []string{"hello", "summarize"} {
		if !hasPrompt(prompts.Prompts, want) {
			t.Fatalf("prompts/list missing %s: %#v", want, prompts.Prompts)
		}
	}

	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources.Resources) != 1 || resources.Resources[0].URI != "embedded:readme" {
		t.Fatalf("resources/list unexpected result: %#v", resources.Resources)
	}
}

func TestSmokeSurfaceHandlers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := newMCPServer().Connect(ctx, serverTransport, nil)
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

	callRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "aaa-ping",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call tool aaa-ping: %v", err)
	}
	if got := firstText(callRes.Content); got != "pong" {
		t.Fatalf("aaa-ping returned %q, want pong", got)
	}

	callRes, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "upper",
		Arguments: map[string]any{"message": "governance"},
	})
	if err != nil {
		t.Fatalf("call tool upper: %v", err)
	}
	if got := firstText(callRes.Content); got != "GOVERNANCE" {
		t.Fatalf("upper returned %q, want GOVERNANCE", got)
	}

	readRes, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: "embedded:readme"})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(readRes.Contents) != 1 || readRes.Contents[0].Text == "" {
		t.Fatalf("unexpected resource contents: %#v", readRes.Contents)
	}

	promptRes, err := clientSession.GetPrompt(ctx, &mcp.GetPromptParams{Name: "hello"})
	if err != nil {
		t.Fatalf("get prompt hello: %v", err)
	}
	if len(promptRes.Messages) != 1 {
		t.Fatalf("hello prompt messages = %d, want 1", len(promptRes.Messages))
	}
	if got := firstText([]mcp.Content{promptRes.Messages[0].Content}); got != "Hello from the Go MCP example server." {
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
