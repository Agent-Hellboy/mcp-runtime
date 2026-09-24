// Package mcpdefaults defines platform defaults shared across the API, CLI,
// operator, and runtime services.
package mcpdefaults

const (
	MCPServerPort       = 8088
	MCPGatewayPort      = 8091
	MCPServersNamespace = "mcp-servers"

	AuthHumanIDHeader   = "X-MCP-Human-ID"
	AuthAgentIDHeader   = "X-MCP-Agent-ID"
	AuthTeamIDHeader    = "X-MCP-Team-ID"
	AuthSessionIDHeader = "X-MCP-Agent-Session"
	AuthTokenHeader     = "Authorization"

	PolicyMode          = "allow-list"
	PolicyDecision      = "deny"
	PolicyDecisionAllow = "allow"
	PolicyEnforceOn     = "call_tool"
	PolicyVersion       = "v1"

	SessionStore    = "kubernetes"
	SessionMaxLife  = "24h"
	SessionIdleTime = "1h"
	SessionUpstream = AuthTokenHeader
)

// GatewayPolicyConfigMapName returns the ConfigMap name used by a server's gateway.
func GatewayPolicyConfigMapName(serverName string) string {
	return serverName + "-gateway-policy"
}

// DefaultIngressPath returns the default public MCP path for a server name.
func DefaultIngressPath(serverName string) string {
	return "/" + serverName + "/mcp"
}
