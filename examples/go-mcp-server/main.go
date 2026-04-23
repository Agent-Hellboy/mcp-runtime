package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type smokePingArgs struct {
	Note string `json:"note,omitempty" jsonschema:"optional no-op note"`
}

type echoArgs struct {
	Message string `json:"message" jsonschema:"message to echo"`
}

type addArgs struct {
	A float64 `json:"a" jsonschema:"first number"`
	B float64 `json:"b" jsonschema:"second number"`
}

type messageArgs struct {
	Message string `json:"message" jsonschema:"message to transform"`
}

type slugifyArgs struct {
	Message string `json:"message" jsonschema:"message to slugify"`
}

type server struct{}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func main() {
	port := envOr("PORT", "8088")
	mcpPath := normalizeMCPPath(envOr("MCP_PATH", "/mcp"))
	mcpServer := newMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	})
	mux.Handle(mcpPath, handler)

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("go-example-mcp listening on :%s", port)
	log.Fatal(httpServer.ListenAndServe())
}

func newMCPServer() *mcp.Server {
	srv := &server{}
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "go-example-mcp",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: "Go MCP example server with smoke, text, prompt, and resource examples.",
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "aaa-ping",
		Description: "Return a simple pong response",
	}, srv.smokePingTool)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "echo",
		Description: "Echo back the provided message",
	}, srv.echoTool)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "add",
		Description: "Add two numbers",
	}, srv.addTool)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "upper",
		Description: "Uppercase the provided message",
	}, srv.upperTool)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "lower",
		Description: "Lowercase the provided message",
	}, srv.lowerTool)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "slugify",
		Description: "Convert the provided message into a URL slug",
	}, srv.slugifyTool)

	mcpServer.AddResource(&mcp.Resource{
		Name:        "readme",
		Description: "Sample resource served by the Go MCP example server",
		MIMEType:    "text/plain",
		URI:         "embedded:readme",
	}, srv.readResource)

	mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "hello",
		Description: "Return a simple prompt message",
	}, srv.getHelloPrompt)

	mcpServer.AddPrompt(&mcp.Prompt{
		Name:        "summarize",
		Description: "Summarize a short text input",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "text",
				Description: "Text to summarize",
				Required:    true,
			},
		},
	}, srv.getSummarizePrompt)

	return mcpServer
}

func (s *server) smokePingTool(_ context.Context, _ *mcp.CallToolRequest, _ *smokePingArgs) (*mcp.CallToolResult, any, error) {
	return textResult("pong"), nil, nil
}

func (s *server) echoTool(_ context.Context, _ *mcp.CallToolRequest, args *echoArgs) (*mcp.CallToolResult, any, error) {
	if args == nil {
		args = &echoArgs{}
	}
	return textResult(args.Message), nil, nil
}

func (s *server) addTool(_ context.Context, _ *mcp.CallToolRequest, args *addArgs) (*mcp.CallToolResult, any, error) {
	if args == nil {
		args = &addArgs{}
	}
	return textResult(fmt.Sprintf("%g", args.A+args.B)), nil, nil
}

func (s *server) upperTool(_ context.Context, _ *mcp.CallToolRequest, args *messageArgs) (*mcp.CallToolResult, any, error) {
	if args == nil {
		args = &messageArgs{}
	}
	return textResult(strings.ToUpper(args.Message)), nil, nil
}

func (s *server) lowerTool(_ context.Context, _ *mcp.CallToolRequest, args *messageArgs) (*mcp.CallToolResult, any, error) {
	if args == nil {
		args = &messageArgs{}
	}
	return textResult(strings.ToLower(args.Message)), nil, nil
}

func (s *server) slugifyTool(_ context.Context, _ *mcp.CallToolRequest, args *slugifyArgs) (*mcp.CallToolResult, any, error) {
	if args == nil {
		args = &slugifyArgs{}
	}
	slug := strings.ToLower(strings.TrimSpace(args.Message))
	slug = nonSlugChars.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return textResult(slug), nil, nil
}

func (s *server) readResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil || strings.TrimSpace(req.Params.URI) == "" {
		return nil, fmt.Errorf("invalid request")
	}
	if req.Params.URI != "embedded:readme" {
		return nil, fmt.Errorf("resource not found: %s", req.Params.URI)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "text/plain",
				Text:     "This is a sample resource payload from the Go MCP example server.",
			},
		},
	}, nil
}

func (s *server) getHelloPrompt(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{
				Role:    "assistant",
				Content: &mcp.TextContent{Text: "Hello from the Go MCP example server."},
			},
		},
	}, nil
}

func (s *server) getSummarizePrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := ""
	if req != nil && req.Params != nil {
		text = req.Params.Arguments["text"]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "No text provided."
	}
	return &mcp.GetPromptResult{
		Description: "Short summary prompt",
		Messages: []*mcp.PromptMessage{
			{
				Role:    "assistant",
				Content: &mcp.TextContent{Text: fmt.Sprintf("Summarize this briefly: %s", text)},
			},
		},
	}, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func normalizeMCPPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
