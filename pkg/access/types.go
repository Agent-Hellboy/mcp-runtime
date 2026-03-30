package access

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServerReference identifies an MCPServer.
type ServerReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// SubjectRef identifies the human and optional agent a grant or session applies to.
type SubjectRef struct {
	HumanID string `json:"humanID,omitempty"`
	AgentID string `json:"agentID,omitempty"`
}

// TrustLevel defines trust levels for access control.
type TrustLevel string

const (
	TrustNone TrustLevel = "none"
	TrustLow  TrustLevel = "low"
	TrustMid  TrustLevel = "mid"
	TrustHigh TrustLevel = "high"
	TrustFull TrustLevel = "full"
)

// PolicyDecision defines policy decisions for tool access.
type PolicyDecision string

const (
	DecisionAllow PolicyDecision = "allow"
	DecisionDeny  PolicyDecision = "deny"
	DecisionAudit PolicyDecision = "audit"
)

// ToolRule controls access to an individual MCP tool.
type ToolRule struct {
	Name          string         `json:"name"`
	Decision      PolicyDecision `json:"decision"`
	RequiredTrust TrustLevel     `json:"requiredTrust,omitempty"`
}

// SecretKeyRef references a secret key.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// MCPAccessGrantSpec defines who can use which MCP server and with what trust ceiling.
type MCPAccessGrantSpec struct {
	ServerRef     ServerReference `json:"serverRef"`
	Subject       SubjectRef      `json:"subject"`
	MaxTrust      TrustLevel      `json:"maxTrust,omitempty"`
	PolicyVersion string          `json:"policyVersion,omitempty"`
	Disabled      bool            `json:"disabled,omitempty"`
	ToolRules     []ToolRule      `json:"toolRules,omitempty"`
}

// MCPAccessGrantStatus captures observed grant state.
type MCPAccessGrantStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Message    string             `json:"message,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MCPAccessGrant grants a human or agent access to an MCPServer.
type MCPAccessGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPAccessGrantSpec   `json:"spec,omitempty"`
	Status MCPAccessGrantStatus `json:"status,omitempty"`
}

// MCPAccessGrantList contains a list of MCPAccessGrant.
type MCPAccessGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPAccessGrant `json:"items"`
}

// MCPAgentSessionSpec defines a consented server-side agent session.
type MCPAgentSessionSpec struct {
	ServerRef              ServerReference `json:"serverRef"`
	Subject                SubjectRef      `json:"subject"`
	ConsentedTrust         TrustLevel      `json:"consentedTrust,omitempty"`
	ExpiresAt              *metav1.Time    `json:"expiresAt,omitempty"`
	Revoked                bool            `json:"revoked,omitempty"`
	UpstreamTokenSecretRef *SecretKeyRef   `json:"upstreamTokenSecretRef,omitempty"`
	PolicyVersion          string          `json:"policyVersion,omitempty"`
}

// MCPAgentSessionStatus captures observed session state.
type MCPAgentSessionStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Message    string             `json:"message,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MCPAgentSession stores consent and upstream token state for an agent session.
type MCPAgentSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPAgentSessionSpec   `json:"spec,omitempty"`
	Status MCPAgentSessionStatus `json:"status,omitempty"`
}

// MCPAgentSessionList contains a list of MCPAgentSession.
type MCPAgentSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPAgentSession `json:"items"`
}
