// Verifies a token issued by the separately deployed mcp-auth server.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	mcpauth "github.com/Agent-Hellboy/mcp-auth/auth-client/go/mcpauth"
)

// protocolVersion is the MCP revision this check negotiates. Keep it in step
// with internal/agentadapter/config.go DefaultProtocolVersion.
const protocolVersion = "2025-06-18"

func main() {
	issuer := strings.TrimRight(os.Getenv("MCP_AUTH_ISSUER"), "/")
	resource := os.Getenv("MCP_AUTH_RESOURCE")
	token := strings.TrimSpace(os.Getenv("MCP_AUTH_ACCESS_TOKEN"))
	if issuer == "" || resource == "" || token == "" {
		fmt.Fprintln(os.Stderr, "set MCP_AUTH_ISSUER, MCP_AUTH_RESOURCE, and MCP_AUTH_ACCESS_TOKEN")
		os.Exit(2)
	}
	metadata, err := mcpauth.DiscoverAuthorizationServer(http.DefaultClient, issuer)
	if err != nil {
		panic(err)
	}
	claims, err := (&mcpauth.JWTVerifier{JWKSURL: metadata.JWKSURI, Issuer: issuer, Audience: resource}).Verify(token)
	if err != nil {
		panic(err)
	}
	fmt.Printf("mcp-auth SDK verification passed: subject=%s scopes=%v\n", claims.Subject, claims.Scopes)
	mcpURL := strings.TrimRight(os.Getenv("MCP_SERVER_URL"), "/")
	if mcpURL == "" {
		return
	}
	// A conforming initialize carries protocolVersion, capabilities and
	// clientInfo; params:{} only works against a lenient server.
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"` + protocolVersion + `",` +
		`"capabilities":{},` +
		`"clientInfo":{"name":"mcp-auth-sdk-client","version":"1.0.0"}}}`
	req, err := http.NewRequest(http.MethodPost, mcpURL, strings.NewReader(initialize))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		panic(fmt.Sprintf("authenticated MCP initialize returned %s: %s", response.Status, body))
	}
	fmt.Printf("authenticated MCP initialize passed: %s\n", response.Status)
}
