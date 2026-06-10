package main

import "context"

// policyFilter is stage 2 of the gateway pipeline. It acquires the current
// atomic policy snapshot and sets Exchange.Policy and Exchange.PolicyErr.
//
// OAuth protected-resource metadata requests (/.well-known/oauth-protected-resource)
// are handled here, immediately after the policy is available, and produce a
// Respond result that terminates the pipeline without passing through auth or
// authz stages.
func (s *gatewayServer) policyFilter(_ context.Context, ex *Exchange) Result {
	ex.Policy, ex.PolicyErr = s.currentPolicy()
	if s.handleOAuthProtectedResource(ex.W, ex.R, ex.Policy) {
		return Respond
	}
	return Continue
}
