package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	mcpauth "github.com/example/mcp-auth/auth-client/go/mcpauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	issuer := strings.TrimRight(os.Getenv("MCP_AUTH_ISSUER"), "/")
	resource := os.Getenv("MCP_AUTH_RESOURCE")
	metadataURL := os.Getenv("MCP_AUTH_RESOURCE_METADATA_URL")
	mcpPath := envOr("MCP_PATH", "/mcp")
	discoveryIssuer := envOr("MCP_AUTH_DISCOVERY_ISSUER", issuer)
	internalBase := strings.TrimRight(os.Getenv("MCP_AUTH_INTERNAL_BASE_URL"), "/")
	if issuer == "" || resource == "" || metadataURL == "" {
		log.Fatal("MCP_AUTH_ISSUER, MCP_AUTH_RESOURCE, and MCP_AUTH_RESOURCE_METADATA_URL are required")
	}
	metadata, err := mcpauth.DiscoverAuthorizationServer(http.DefaultClient, discoveryIssuer)
	if err != nil {
		log.Fatal(err)
	}
	if internalBase != "" {
		metadata.JWKSURI = internalBase + strings.TrimPrefix(metadata.JWKSURI, issuer)
	}
	verifier := &mcpauth.JWTVerifier{JWKSURL: metadata.JWKSURI, Issuer: issuer, Audience: resource, RequiredScopes: map[string]bool{"tools:read": true}}
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-auth-sdk-ping", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "sdk-ping", Description: "Return pong after mcp-auth SDK verification"}, func(context.Context, *mcp.CallToolRequest, any) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"ok":true}`) })
	mux := http.NewServeMux()
	mux.Handle(mcpPath, requireAuth(verifier, metadataURL, handler))
	metadataHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q],"bearer_methods_supported":["header"]}`, resource, issuer)
	}
	metadataPath := "/.well-known/oauth-protected-resource" + mcpPath
	mux.HandleFunc(metadataPath, metadataHandler)
	mux.HandleFunc("/.well-known/oauth-protected-resource", metadataHandler)
	log.Fatal(http.ListenAndServe(":"+envOr("PORT", "8088"), mux))
}

func requireAuth(verifier *mcpauth.JWTVerifier, metadataURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := verifier.Verify(parts[1]); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
