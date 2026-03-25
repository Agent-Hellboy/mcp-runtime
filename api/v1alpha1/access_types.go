package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ServerReference identifies an MCPServer.
// +kubebuilder:object:generate=true
type ServerReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// SubjectRef identifies the human and optional agent a grant or session applies to.
// +kubebuilder:object:generate=true
type SubjectRef struct {
	HumanID string `json:"humanID,omitempty"`
	AgentID string `json:"agentID,omitempty"`
}

// ToolRule controls access to an individual MCP tool.
// +kubebuilder:object:generate=true
type ToolRule struct {
	Name          string         `json:"name"`
	Decision      PolicyDecision `json:"decision"`
	RequiredTrust TrustLevel     `json:"requiredTrust,omitempty"`
}

// MCPAccessGrantSpec defines who can use which MCP server and with what trust ceiling.
// +kubebuilder:object:generate=true
type MCPAccessGrantSpec struct {
	ServerRef     ServerReference `json:"serverRef"`
	Subject       SubjectRef      `json:"subject"`
	MaxTrust      TrustLevel      `json:"maxTrust,omitempty"`
	PolicyVersion string          `json:"policyVersion,omitempty"`
	Disabled      bool            `json:"disabled,omitempty"`
	ToolRules     []ToolRule      `json:"toolRules,omitempty"`
}

// MCPAccessGrantStatus captures observed grant state.
// +kubebuilder:object:generate=true
type MCPAccessGrantStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Message    string             `json:"message,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server",type="string",JSONPath=".spec.serverRef.name"
// +kubebuilder:printcolumn:name="Human",type="string",JSONPath=".spec.subject.humanID"
// +kubebuilder:printcolumn:name="Agent",type="string",JSONPath=".spec.subject.agentID"
// +kubebuilder:printcolumn:name="Trust",type="string",JSONPath=".spec.maxTrust"
// +kubebuilder:printcolumn:name="Disabled",type="boolean",JSONPath=".spec.disabled"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MCPAccessGrant grants a human or agent access to an MCPServer.
type MCPAccessGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPAccessGrantSpec   `json:"spec,omitempty"`
	Status MCPAccessGrantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPAccessGrantList contains a list of MCPAccessGrant.
type MCPAccessGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPAccessGrant `json:"items"`
}

// MCPAgentSessionSpec defines a consented server-side agent session.
// +kubebuilder:object:generate=true
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
// +kubebuilder:object:generate=true
type MCPAgentSessionStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Message    string             `json:"message,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server",type="string",JSONPath=".spec.serverRef.name"
// +kubebuilder:printcolumn:name="Human",type="string",JSONPath=".spec.subject.humanID"
// +kubebuilder:printcolumn:name="Agent",type="string",JSONPath=".spec.subject.agentID"
// +kubebuilder:printcolumn:name="Trust",type="string",JSONPath=".spec.consentedTrust"
// +kubebuilder:printcolumn:name="Revoked",type="boolean",JSONPath=".spec.revoked"
// +kubebuilder:printcolumn:name="Expires",type="date",JSONPath=".spec.expiresAt"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MCPAgentSession stores consent and upstream token state for an agent session.
type MCPAgentSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPAgentSessionSpec   `json:"spec,omitempty"`
	Status MCPAgentSessionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPAgentSessionList contains a list of MCPAgentSession.
type MCPAgentSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPAgentSession `json:"items"`
}
