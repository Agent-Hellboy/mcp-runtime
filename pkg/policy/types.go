// Package policy provides shared gateway policy types used by both the operator
// and the MCP proxy. This ensures contract compatibility between the operator-rendered
// policy and the proxy-consumed policy.
package policy

// Document is the root gateway policy document that contains all policy configuration.
type Document struct {
	Server   Server    `json:"server"`
	Auth     *Auth     `json:"auth,omitempty"`
	Policy   *Config   `json:"policy,omitempty"`
	Session  *Session  `json:"session,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"`
	Grants   []Grant   `json:"grants,omitempty"`
	Sessions []Binding `json:"sessions,omitempty"`
}

// Server identifies the MCP server this policy applies to.
type Server struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Cluster   string `json:"cluster,omitempty"`
}

// Auth configures authentication settings for the gateway.
type Auth struct {
	Mode            string `json:"mode,omitempty"`
	HumanIDHeader   string `json:"human_id_header,omitempty"`
	AgentIDHeader   string `json:"agent_id_header,omitempty"`
	SessionIDHeader string `json:"session_id_header,omitempty"`
	TokenHeader     string `json:"token_header,omitempty"`
	IssuerURL       string `json:"issuer_url,omitempty"`
	Audience        string `json:"audience,omitempty"`
}

// Config contains policy enforcement configuration.
type Config struct {
	Mode            string `json:"mode,omitempty"`
	DefaultDecision string `json:"default_decision,omitempty"`
	EnforceOn       string `json:"enforce_on,omitempty"`
	PolicyVersion   string `json:"policy_version,omitempty"`
}

// Session configures session management settings.
type Session struct {
	Required            bool   `json:"required,omitempty"`
	Store               string `json:"store,omitempty"`
	HeaderName          string `json:"header_name,omitempty"`
	MaxLifetime         string `json:"max_lifetime,omitempty"`
	IdleTimeout         string `json:"idle_timeout,omitempty"`
	UpstreamTokenHeader string `json:"upstream_token_header,omitempty"`
}

// Tool describes an MCP tool and its trust requirements.
type Tool struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	RequiredTrust string            `json:"required_trust,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// Grant defines access grants for subjects (humans/agents).
type Grant struct {
	Name          string       `json:"name"`
	HumanID       string       `json:"human_id,omitempty"`
	AgentID       string       `json:"agent_id,omitempty"`
	MaxTrust      string       `json:"max_trust,omitempty"`
	PolicyVersion string       `json:"policy_version,omitempty"`
	Disabled      bool         `json:"disabled,omitempty"`
	ToolRules     []ToolAccess `json:"tool_rules,omitempty"`
}

// Binding represents an agent session binding.
type Binding struct {
	Name             string `json:"name"`
	HumanID          string `json:"human_id,omitempty"`
	AgentID          string `json:"agent_id,omitempty"`
	ConsentedTrust   string `json:"consented_trust,omitempty"`
	Revoked          bool   `json:"revoked,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	PolicyVersion    string `json:"policy_version,omitempty"`
	UpstreamTokenRef string `json:"upstream_token_ref,omitempty"`
}

// ToolAccess defines access rules for a specific tool.
type ToolAccess struct {
	Name          string `json:"name"`
	Decision      string `json:"decision,omitempty"`
	RequiredTrust string `json:"required_trust,omitempty"`
}
