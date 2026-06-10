// Package main implements the MCP gateway service.
//
// # Request filter pipeline
//
// Every inbound request is processed through a fixed ordered pipeline of five
// stages before audit/analytics finalization runs unconditionally as stage 6.
// Stage order is security-sensitive and is fixed in code:
//
//	Stage 1 – InspectFilter:   bounded body capture; RPC method and tool name extraction
//	Stage 2 – PolicyFilter:    atomic policy snapshot acquisition; OAuth metadata early-exit
//	Stage 3 – AuthFilter:      authentication and identity extraction (header or OAuth JWT)
//	Stage 4 – AuthzFilter:     authorization and session/grant evaluation
//	Stage 5 – UpstreamFilter:  identity header rewrite; path rewrite; upstream proxy
//	Stage 6 – (orchestrator):  audit/analytics finalization
//
// Ordering guarantees:
//   - Authentication (stage 3) always completes before authorization (stage 4).
//   - The policy snapshot (stage 2) is captured once and held immutable for the
//     entire exchange lifetime; no later stage may re-acquire it.
//   - Authorization inputs (Policy and Identity on Exchange) must not be mutated
//     after the authz stage sets Decision; upstream preparation (stage 5) only
//     reads them.
//
// Each filter returns Continue, Reject, or Respond. On any non-Continue result
// the pipeline halts and stage 6 runs from the orchestrator.
package main

import (
	"context"
	"net/http"
	"time"

	policypkg "mcp-runtime/pkg/policy"
)

// Result is the signal a Filter returns to the pipeline runner.
type Result int

const (
	// Continue passes control to the next filter in the pipeline.
	Continue Result = iota
	// Reject signals that this filter wrote a denial response. The pipeline
	// halts; stage 6 (audit) runs from the orchestrator.
	Reject
	// Respond signals that this filter wrote a terminal response (e.g. OAuth
	// metadata, successful upstream proxy). The pipeline halts; stage 6 runs.
	Respond
)

// Filter is a single stage in the gateway request pipeline.
type Filter interface {
	Handle(context.Context, *Exchange) Result
}

// FilterFunc is a function that implements Filter.
type FilterFunc func(context.Context, *Exchange) Result

// Handle implements Filter.
func (f FilterFunc) Handle(ctx context.Context, ex *Exchange) Result { return f(ctx, ex) }

// Exchange holds all request-scoped state shared across pipeline stages.
// Fields are written by one designated stage and read by all later stages.
// Do not mutate Policy or Identity after AuthzFilter sets Decision.
type Exchange struct {
	// W wraps the original ResponseWriter to capture status code and byte count.
	W *statusRecorder
	// R is the inbound HTTP request. InspectFilter may replace Body with a
	// buffered reader; no later stage should replace R.
	R *http.Request
	// OriginalPath is r.URL.Path captured before any prefix stripping.
	OriginalPath string
	// StartTime is captured by the orchestrator for latency accounting.
	StartTime time.Time

	// Set by stage 1 (InspectFilter).
	Inspection rpcInspection

	// Set by stage 2 (PolicyFilter). Immutable for the lifetime of this exchange.
	Policy    *policypkg.Document
	PolicyErr error

	// Set by stage 3 (AuthFilter). Must be complete before stage 4 reads them.
	Identity   identityContext
	OAuthToken string // forwarded to upstream via session.UpstreamTokenHeader

	// Set by stage 4 (AuthzFilter). Policy and Identity must not change after this.
	Decision policypkg.Decision
}

// newExchange initialises an Exchange for a fresh inbound request.
func newExchange(w http.ResponseWriter, r *http.Request, defaultPolicyVersion string) *Exchange {
	return &Exchange{
		W:            &statusRecorder{ResponseWriter: w, status: http.StatusOK},
		R:            r,
		OriginalPath: r.URL.Path,
		StartTime:    time.Now(),
		Decision: policypkg.Decision{
			Allowed:       true,
			Status:        http.StatusOK,
			Reason:        "allowed",
			PolicyVersion: defaultPolicyVersion,
		},
	}
}
