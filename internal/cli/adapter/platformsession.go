package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/agentadapter"
	"mcp-runtime/internal/cli/platformapi"
)

const (
	// EnvPlatformURL overrides the platform API base URL. When set, the
	// adapter resolves its identity by calling POST /api/runtime/adapter/sessions.
	EnvPlatformURL = "MCP_PLATFORM_API_URL"
	// EnvAdapterServer / Namespace / Agent populate the request to the platform.
	EnvAdapterServer      = "MCP_RUNTIME_ADAPTER_SERVER"
	EnvAdapterNamespace   = "MCP_RUNTIME_ADAPTER_NAMESPACE"
	EnvAdapterAgent       = "MCP_RUNTIME_ADAPTER_AGENT"
	EnvAdapterAutoRefresh = "MCP_RUNTIME_ADAPTER_AUTO_REFRESH"
	// EnvAdapterAuthMode selects the adapter auth mode (header or mtls).
	EnvAdapterAuthMode = "MCP_RUNTIME_AUTH_MODE"
	// EnvMTLSTrustDomain is the SPIFFE trust domain used to build the CSR's
	// URI SAN; it must match spec.auth.trustDomain on the target MCPServer.
	EnvMTLSTrustDomain = "MCP_MTLS_TRUST_DOMAIN"
	// DefaultMTLSTrustDomain matches the platform default trust domain.
	DefaultMTLSTrustDomain = "mcpruntime.org"
	// adapterRefreshLead is how far in advance of expiry the refresher fires.
	// Keep this above the platform's adapterSessionRefreshBuffer so the
	// refresh and reuse-window stay aligned.
	adapterRefreshLead = 5 * time.Minute
	// adapterRefreshFloor caps how aggressively short TTLs reschedule, so a
	// 1-minute TTL doesn't turn into a tight retry loop.
	adapterRefreshFloor = 30 * time.Second
)

// platformSessionFlags carries the platform-session bootstrap settings.
type platformSessionFlags struct {
	server      string
	namespace   string
	agent       string
	platformURL string
	autoRefresh bool
}

func (f *platformSessionFlags) enabled() bool {
	return f != nil && strings.TrimSpace(f.server) != ""
}

func bindPlatformSessionFlags(cmd *cobra.Command, f *platformSessionFlags) {
	cmd.Flags().StringVar(&f.server, "server", os.Getenv(EnvAdapterServer),
		"MCPServer name to fetch an issued adapter session for (enables platform-issued sessions; default: $"+EnvAdapterServer+")")
	cmd.Flags().StringVar(&f.namespace, "namespace", os.Getenv(EnvAdapterNamespace),
		"Namespace of the target MCPServer; defaults to the principal's primary namespace (default: $"+EnvAdapterNamespace+")")
	cmd.Flags().StringVar(&f.agent, "agent", os.Getenv(EnvAdapterAgent),
		"Agent identifier to associate with the issued session (default: $"+EnvAdapterAgent+")")
	cmd.Flags().StringVar(&f.platformURL, "platform-url", os.Getenv(EnvPlatformURL),
		"Platform API base URL; overrides the URL stored by mcp-runtime auth login (default: $"+EnvPlatformURL+")")
	cmd.Flags().BoolVar(&f.autoRefresh, "auto-refresh", parseEnvBoolSimple(EnvAdapterAutoRefresh),
		"Refresh the issued adapter session a few minutes before expiry (default: $"+EnvAdapterAutoRefresh+")")
}

// applyPlatformSession asks the platform for an issued adapter session and
// returns the effective identity for first use plus, when autoRefresh is set,
// an IdentityProvider that the adapter calls on every outbound request.
//
// baseIdentity carries the user's explicit flag/env overrides (--human-id,
// --agent-id, etc.). The returned identity and the IdentityProvider closure
// both apply mergeIdentityFromIssued(base, issued) so user overrides survive
// every refresh — without this, auto-refresh would silently revert to the
// platform-issued values on each tick.
//
// errMsgSink receives refresh failures; the loop keeps the previous identity
// in place until the next tick succeeds, so transient platform errors do not
// take the adapter down.
func applyPlatformSession(
	ctx context.Context,
	f *platformSessionFlags,
	baseIdentity agentadapter.Identity,
	errMsgSink io.Writer,
) (agentadapter.Identity, agentadapter.IdentityProvider, *platformSessionRefresher, error) {
	if !f.enabled() {
		return baseIdentity, nil, nil, nil
	}
	if strings.TrimSpace(f.agent) == "" {
		return agentadapter.Identity{}, nil, nil, errors.New("--agent (or $" + EnvAdapterAgent + ") is required when --server is set")
	}

	// Override the resolved URL so callers can target a non-default platform
	// without re-running mcp-runtime auth login.
	if u := strings.TrimSpace(f.platformURL); u != "" {
		if err := os.Setenv("MCP_PLATFORM_API_URL", u); err != nil {
			return agentadapter.Identity{}, nil, nil, fmt.Errorf("set MCP_PLATFORM_API_URL: %w", err)
		}
	}

	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return agentadapter.Identity{}, nil, nil, fmt.Errorf("platform client: %w", err)
	}

	session, err := client.CreateAdapterSession(ctx, platformapi.AdapterSessionRequest{
		ServerName: strings.TrimSpace(f.server),
		Namespace:  strings.TrimSpace(f.namespace),
		AgentID:    strings.TrimSpace(f.agent),
	})
	if err != nil {
		return agentadapter.Identity{}, nil, nil, fmt.Errorf("create adapter session: %w", err)
	}
	issued := adapterIdentityFromSession(session)
	merged := mergeIdentityFromIssued(baseIdentity, issued)

	if !f.autoRefresh {
		return merged, nil, nil, nil
	}
	holder := &atomic.Value{}
	holder.Store(issued)
	r := &platformSessionRefresher{
		client: client,
		flags:  *f,
		holder: holder,
		expiry: session.ExpiresAt,
		sink:   errMsgSink,
	}
	r.start(ctx)
	provider := agentadapter.IdentityProvider(func() agentadapter.Identity {
		// Merge on every call so user overrides survive each refresh.
		return mergeIdentityFromIssued(baseIdentity, holder.Load().(agentadapter.Identity))
	})
	return merged, provider, r, nil
}

// adapterIdentityFromSession converts a platform AdapterSession into the
// governance headers the adapter forwards to the runtime. The session's
// metadata.Name is the SessionID — it doubles as the cluster's
// MCPAgentSession resource name, so the gateway can look it up directly.
func adapterIdentityFromSession(s platformapi.AdapterSession) agentadapter.Identity {
	return agentadapter.Identity{
		HumanID:   s.HumanID,
		AgentID:   s.AgentID,
		TeamID:    s.TeamID,
		SessionID: s.Name,
	}
}

// mergeIdentityFromIssued lets explicit flag/env values override the issued
// session's fields. This keeps the existing flag-driven flow predictable
// while still letting --server populate any field the user did not supply.
func mergeIdentityFromIssued(flag, issued agentadapter.Identity) agentadapter.Identity {
	out := flag
	if out.HumanID == "" {
		out.HumanID = issued.HumanID
	}
	if out.AgentID == "" {
		out.AgentID = issued.AgentID
	}
	if out.TeamID == "" {
		out.TeamID = issued.TeamID
	}
	if out.SessionID == "" {
		out.SessionID = issued.SessionID
	}
	return out
}

// platformSessionRefresher periodically renews the issued adapter session.
// The latest Identity is stored in holder so the IdentityProvider closure
// the adapter calls on every request returns the up-to-date value without
// any explicit handoff.
type platformSessionRefresher struct {
	client *platformapi.PlatformClient
	flags  platformSessionFlags
	holder *atomic.Value // stores agentadapter.Identity
	expiry time.Time
	sink   io.Writer

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *platformSessionRefresher) start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()
	go r.loop(ctx)
}

// Stop cancels the refresh loop and waits for it to return. Safe to call
// multiple times.
func (r *platformSessionRefresher) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *platformSessionRefresher) loop(ctx context.Context) {
	defer close(r.done)
	for {
		wait := time.Until(r.expiry) - adapterRefreshLead
		if wait < adapterRefreshFloor {
			wait = adapterRefreshFloor
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		session, err := r.client.CreateAdapterSession(ctx, platformapi.AdapterSessionRequest{
			ServerName: strings.TrimSpace(r.flags.server),
			Namespace:  strings.TrimSpace(r.flags.namespace),
			AgentID:    strings.TrimSpace(r.flags.agent),
		})
		if err != nil {
			// Log and keep going; the existing identity is still valid until
			// the actual expiry. The next tick will retry.
			if r.sink != nil {
				fmt.Fprintf(r.sink, "mcp-runtime adapter: session refresh failed: %v\n", err)
			}
			continue
		}
		r.holder.Store(adapterIdentityFromSession(session))
		r.expiry = session.ExpiresAt
	}
}
