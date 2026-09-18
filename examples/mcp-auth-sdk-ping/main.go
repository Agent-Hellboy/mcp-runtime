package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	mcpauth "github.com/Agent-Hellboy/mcp-auth/auth-client/go/mcpauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	issuer := strings.TrimRight(os.Getenv("MCP_AUTH_ISSUER"), "/")
	resource := os.Getenv("MCP_AUTH_RESOURCE")
	metadataURL := os.Getenv("MCP_AUTH_RESOURCE_METADATA_URL")
	mcpPath := envOr("MCP_PATH", "/mcp")
	if issuer == "" || resource == "" || metadataURL == "" {
		log.Fatal("MCP_AUTH_ISSUER, MCP_AUTH_RESOURCE, and MCP_AUTH_RESOURCE_METADATA_URL are required")
	}
	jwksURL, err := resolveJWKS(issuer)
	if err != nil {
		log.Fatal(err)
	}
	verifier := &mcpauth.JWTVerifier{JWKSURL: jwksURL, Issuer: issuer, Audience: resource, RequiredScopes: map[string]bool{"tools:read": true}}
	if strings.HasPrefix(jwksURL, "http://") {
		// The optional cluster-local JWKS URL is reached directly over the
		// Service network. mcp-auth's HTTPS guard still applies; this header
		// records that the request is the same trusted backchannel that an
		// ingress would forward as HTTPS. Never use this for a public URL.
		verifier.HTTPClient = &http.Client{Transport: forwardedHTTPSRoundTripper{base: http.DefaultTransport}}
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-auth-sdk-ping", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "sdk-ping", Description: "Return pong after mcp-auth SDK verification"}, func(context.Context, *mcp.CallToolRequest, any) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"ok":true}`) })
	// The SDK middleware owns the 401/403 challenges, so the WWW-Authenticate
	// header the MCP authorization spec requires on every 401 stays correct
	// without each server re-deriving it.
	mux.Handle(mcpPath, mcpauth.RequireToken(verifier, mcpauth.ResourceMetadata{URL: metadataURL}, handler))
	// The SDK derives the document from the verifier, so scopes_supported always
	// matches the scope RequireToken enforces. Hand-writing this JSON is how the
	// member goes missing: the client then asks for no scope and every call
	// fails 403 insufficient_scope, which reads as broken auth rather than a
	// missing advertisement.
	metadataHandler := mcpauth.ProtectedResourceMetadataHandler(verifier, resource, issuer)
	metadataPath := "/.well-known/oauth-protected-resource" + mcpPath
	mux.Handle(metadataPath, metadataHandler)
	mux.Handle("/.well-known/oauth-protected-resource", metadataHandler)
	log.Fatal(http.ListenAndServe(":"+envOr("PORT", "8088"), mux))
}

type forwardedHTTPSRoundTripper struct{ base http.RoundTripper }

func (t forwardedHTTPSRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header.Set("X-Forwarded-Proto", "https")
	return t.base.RoundTrip(copy)
}

// resolveJWKS returns the JWKS endpoint used to verify access tokens.
//
// In the cluster the public issuer is not reachable from inside a pod, and the
// authorization server is fronted by an ingress that strips its path prefix, so
// the metadata document is served at <issuer>/.well-known/... rather than at
// the RFC 8414 path-insertion location the SDK's discovery derives. Setting
// MCP_AUTH_JWKS_URL to the in-cluster endpoint therefore skips discovery
// entirely; the token's issuer and audience are still validated in full.
// Without it the example discovers normally against a directly reachable
// authorization server.
func resolveJWKS(issuer string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("MCP_AUTH_JWKS_URL")); configured != "" {
		return configured, nil
	}
	metadata, err := mcpauth.DiscoverAuthorizationServer(http.DefaultClient, issuer)
	if err != nil {
		return "", err
	}
	return metadata.JWKSURI, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
