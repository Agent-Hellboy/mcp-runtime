package v1alpha1

func init() {
	// Register the types with the scheme builder
	SchemeBuilder.Register(
		&MCPServer{}, &MCPServerList{},
		&MCPAccessGrant{}, &MCPAccessGrantList{},
		&MCPAgentSession{}, &MCPAgentSessionList{},
	)
}
