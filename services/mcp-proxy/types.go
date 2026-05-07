package main

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/MicahParks/keyfunc"

	"mcp-runtime/pkg/events"
	policypkg "mcp-runtime/pkg/policy"
)

type identityContext struct {
	HumanID   string
	AgentID   string
	SessionID string
}

type policySnapshot struct {
	Policy *policypkg.Document
	Err    error
}

type rpcInspection struct {
	Method        string
	ToolName      string
	ToolCall      bool
	Indeterminate bool
	FailureReason string
}

type oauthProvider struct {
	jwks *keyfunc.JWKS
}

type authServerMetadata struct {
	JWKSURI string `json:"jwks_uri"`
}

type oauthAuthResult struct {
	Allowed  bool
	Status   int
	Reason   string
	Identity identityContext
	Token    string
}

type proxyServer struct {
	proxy                 *httputil.ReverseProxy
	analyticsURL          string
	apiKey                string
	source                string
	eventType             string
	analyticsQueue        chan events.Envelope
	stripPrefix           string
	externalBaseURL       *url.URL
	httpClient            *http.Client
	policyFile            string
	serverName            string
	serverNamespace       string
	clusterName           string
	defaultHumanHeader    string
	defaultAgentHeader    string
	defaultSessionHeader  string
	defaultPolicyMode     string
	defaultPolicyDecision string
	defaultPolicyVersion  string
	analyticsCancel       context.CancelFunc
	analyticsMu           sync.Mutex
	analyticsOnce         sync.Once
	oauthMu               sync.Mutex
	oauthProviders        map[string]*oauthProvider
	policyState           atomic.Value
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

const (
	maxRPCBodyBytes       = 1 << 20
	analyticsQueueSize    = 256
	analyticsWorkerCount  = 4
	defaultHumanHeader    = "X-MCP-Human-ID"
	defaultAgentHeader    = "X-MCP-Agent-ID"
	defaultSessionHeader  = "X-MCP-Agent-Session"
	defaultPolicyMode     = "allow-list"
	defaultPolicyDecision = "deny"
	defaultPolicyVersion  = "v1"
	oauthProtectedPrefix  = "/.well-known/oauth-protected-resource"
	defaultTokenHeader    = "Authorization"
)

// main initializes and starts the MCP Proxy service.
