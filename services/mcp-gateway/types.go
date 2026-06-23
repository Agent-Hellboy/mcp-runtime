package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

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

// policySnapshot is the atomically-swapped view of the active gateway policy.
// A snapshot only ever holds a complete, validated policy (the last-known-good
// document); an invalid reload never replaces Policy and instead records its
// failure in Err while leaving the rest of the snapshot intact.
type policySnapshot struct {
	Policy   *policypkg.Document
	Revision string
	LoadedAt time.Time
	// Err holds the most recent reload error for observability. It does not
	// imply the active Policy is unusable: on a failed reload the previous
	// known-good Policy is retained and Err describes why the update was
	// rejected.
	Err error
	// Ready is true once a valid policy snapshot has been activated. It stays
	// true across subsequent failed reloads (last-known-good is retained).
	Ready bool
}

type rpcInspection struct {
	Method        string
	ToolName      string
	ToolCall      bool
	Indeterminate bool
	FailureReason string
	// IsRPCAttempt is true when the request had application/json content-type
	// (or no content-type) and is therefore a genuine MCP client attempt that
	// should be audited even when Method could not be extracted.
	IsRPCAttempt bool
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
	verifiedSPIFFEHeader  string
	trustedProxySPIFFE    string
	defaultPolicyMode     string
	defaultPolicyDecision string
	defaultPolicyVersion  string
	analyticsMu           sync.Mutex
	analyticsOnce         sync.Once
	analyticsWG           sync.WaitGroup
	analyticsClosed       bool
	analyticsDropped      atomic.Uint64
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
	maxRPCBodyBytes      = 1 << 20
	analyticsQueueSize   = 256
	analyticsWorkerCount = 4
	analyticsEmitTimeout = 5
	defaultHumanHeader   = "X-MCP-Human-ID"
	defaultAgentHeader   = "X-MCP-Agent-ID"
	defaultTeamHeader    = "X-MCP-Team-ID"
	defaultSessionHeader = "X-MCP-Agent-Session"
	// defaultVerifiedSPIFFEHeader carries the caller's SPIFFE identity as
	// extracted and injected by the TLS-terminating ingress (Traefik) in
	// auth.mode mtls. It is trusted only on an ingress-authenticated mTLS hop;
	// see authenticateMTLS.
	defaultVerifiedSPIFFEHeader = "X-MCP-Verified-SPIFFE-ID"
	defaultPolicyMode           = "allow-list"
	defaultPolicyDecision       = "deny"
	defaultPolicyVersion        = "v1"
	oauthProtectedPrefix        = "/.well-known/oauth-protected-resource"
	defaultTokenHeader          = "Authorization"
)

// main initializes and starts the MCP gateway service.
