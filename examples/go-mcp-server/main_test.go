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
	for _, want := range []string{"aaa-ping", "echo", "add", "upper", "lower", "slugify", "create_task"} {
		if !hasTool(tools.Tools, want) {
			t.Fatalf("tools/list missing %s: %#v", want, tools.Tools)
		}
	}

	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	for _, want := range []string{"hello", "summarize", "task_brief"} {
		if !hasPrompt(prompts.Prompts, want) {
			t.Fatalf("prompts/list missing %s: %#v", want, prompts.Prompts)
		}
	}

	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	for _, want := range []string{"embedded:readme", "embedded:task-guide"} {
		if !hasResource(resources.Resources, want) {
			t.Fatalf("resources/list missing %s: %#v", want, resources.Resources)
		}
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

	callRes, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_task",
		Arguments: map[string]any{"title": "Test adapter flow", "priority": "high", "owner": "ide"},
	})
	if err != nil {
		t.Fatalf("call tool create_task: %v", err)
	}
	if got := firstText(callRes.Content); got != "task: Test adapter flow\npriority: high\nowner: ide\nstatus: open" {
		t.Fatalf("create_task returned %q", got)
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

	promptRes, err = clientSession.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "task_brief",
		Arguments: map[string]string{"goal": "verify the IDE adapter path"},
	})
	if err != nil {
		t.Fatalf("get prompt task_brief: %v", err)
	}
	if len(promptRes.Messages) != 1 {
		t.Fatalf("task_brief prompt messages = %d, want 1", len(promptRes.Messages))
	}
	if got := firstText([]mcp.Content{promptRes.Messages[0].Content}); got != "Turn this goal into a concise task brief with acceptance criteria: verify the IDE adapter path" {
		t.Fatalf("task_brief prompt returned %q", got)
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

func hasResource(resources []*mcp.Resource, want string) bool {
	for _, resource := range resources {
		if resource != nil && resource.URI == want {
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
