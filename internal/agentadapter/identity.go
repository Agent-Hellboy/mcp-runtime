package agentadapter

import "net/http"

// Identity is the issued governance identity that adapters attach to every
// runtime request. The platform issues these values out-of-band (or, in a
// later phase, through the adapter session endpoint); the adapter only
// forwards them.
type Identity struct {
	HumanID   string
	AgentID   string
	TeamID    string
	SessionID string
}

// Apply writes the governance identity onto an outbound request's headers,
// replacing any caller-supplied values. Headers are always deleted first to
// strip spoofed inbound values. A header is only re-set when its value is
// non-empty, so anonymous-mode adapters with partial identity naturally omit
// the missing headers rather than forwarding empty strings.
func (id Identity) Apply(headers http.Header) {
	headers.Del(HumanIDHeader)
	headers.Del(AgentIDHeader)
	headers.Del(TeamIDHeader)
	headers.Del(AgentSessionHeader)
	if id.HumanID != "" {
		headers.Set(HumanIDHeader, id.HumanID)
	}
	if id.AgentID != "" {
		headers.Set(AgentIDHeader, id.AgentID)
	}
	if id.TeamID != "" {
		headers.Set(TeamIDHeader, id.TeamID)
	}
	if id.SessionID != "" {
		headers.Set(AgentSessionHeader, id.SessionID)
	}
}
