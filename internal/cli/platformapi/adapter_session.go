package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AdapterSessionRequest is the input contract for the platform API endpoint
// POST /api/runtime/adapter/sessions. RequestedTTL/Trust are optional; empty
// values fall back to platform-side defaults.
type AdapterSessionRequest struct {
	ServerName     string `json:"serverName"`
	Namespace      string `json:"namespace,omitempty"`
	AgentID        string `json:"agentID"`
	RequestedTrust string `json:"requestedTrust,omitempty"`
	RequestedTTL   string `json:"requestedTTL,omitempty"`
}

// AdapterSession captures the identity the adapter must inject into runtime
// requests. ExpiresAt is absolute (server-side time); callers should refresh
// before it elapses.
type AdapterSession struct {
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	HumanID        string    `json:"humanID"`
	AgentID        string    `json:"agentID"`
	TeamID         string    `json:"teamID,omitempty"`
	ServerName     string    `json:"serverName"`
	ConsentedTrust string    `json:"consentedTrust"`
	PolicyVersion  string    `json:"policyVersion"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Reused         bool      `json:"reused"`
}

// CreateAdapterSession asks the platform to issue (or reuse) an MCPAgentSession
// for the calling principal. The returned session.Name doubles as the
// SessionID the adapter forwards on every runtime request.
func (c *PlatformClient) CreateAdapterSession(ctx context.Context, req AdapterSessionRequest) (AdapterSession, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return AdapterSession{}, fmt.Errorf("marshal request: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "/runtime/adapter/sessions", "", bytes.NewReader(body))
	if err != nil {
		return AdapterSession{}, err
	}
	defer resp.Body.Close()
	respBody, err := readBody(io.LimitReader(resp.Body, maxAPIBodyRead))
	if err != nil {
		return AdapterSession{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return AdapterSession{}, httpAPIError(resp.StatusCode, respBody)
	}
	var session AdapterSession
	if err := json.Unmarshal(respBody, &session); err != nil {
		return AdapterSession{}, fmt.Errorf("decode response: %w", err)
	}
	return session, nil
}
