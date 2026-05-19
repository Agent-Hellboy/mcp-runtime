package main

import (
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
	TeamID    string
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

type analyticsEvent struct {
	Envelope     events.Envelope
	TraceContext map[string]string
}

type gatewayServer struct {
	proxy                 *httputil.ReverseProxy
	metrics               *gatewayMetrics
	analyticsURL          string
	apiKey                string
	source                string
	eventType             string
	analyticsQueue        chan analyticsEvent
	stripPrefix           string
	externalBaseURL       *url.URL
	httpClient            *http.Client
	policyFile            string
	serverName            string
	serverNamespace       string
	clusterName           string
	defaultHumanHeader    string
	defaultAgentHeader    string
	defaultTeamHeader     string
	defaultSessionHeader  string
	defaultPolicyMode     string
	defaultPolicyDecision string
	defaultPolicyVersion  string
	analyticsMu           sync.Mutex
	analyticsOnce         sync.Once
	analyticsWG           sync.WaitGroup
	analyticsClosed       bool
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
	analyticsEmitTimeout  = 5
	defaultHumanHeader    = "X-MCP-Human-ID"
	defaultAgentHeader    = "X-MCP-Agent-ID"
	defaultTeamHeader     = "X-MCP-Team-ID"
	defaultSessionHeader  = "X-MCP-Agent-Session"
	defaultPolicyMode     = "allow-list"
	defaultPolicyDecision = "deny"
	defaultPolicyVersion  = "v1"
	oauthProtectedPrefix  = "/.well-known/oauth-protected-resource"
	defaultTokenHeader    = "Authorization"
)

// main initializes and starts the MCP gateway service.
