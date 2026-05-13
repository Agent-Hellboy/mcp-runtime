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
// replacing any caller-supplied values. TeamID is omitted when empty so the
// gateway sees a missing header (the documented "any team" semantics) rather
// than an empty value. SessionID is required by ValidateConfig today and is
// always set; the anonymous-mode flow that may omit it is a Phase 3b change.
func (id Identity) Apply(headers http.Header) {
	headers.Del(HumanIDHeader)
	headers.Del(AgentIDHeader)
	headers.Del(TeamIDHeader)
	headers.Del(AgentSessionHeader)
	headers.Set(HumanIDHeader, id.HumanID)
	headers.Set(AgentIDHeader, id.AgentID)
	if id.TeamID != "" {
		headers.Set(TeamIDHeader, id.TeamID)
	}
	headers.Set(AgentSessionHeader, id.SessionID)
}
