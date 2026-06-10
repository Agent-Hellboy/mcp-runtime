package main

// policyFilter is stage 2 of the gateway pipeline. It acquires the current
// atomic policy snapshot and sets Exchange.Policy and Exchange.PolicyErr.
//
// OAuth protected-resource metadata requests (/.well-known/oauth-protected-resource)
// are handled here, immediately after the policy is available, and produce a
// Respond result that terminates the pipeline without passing through auth or
// authz stages. SkipAudit is set so the orchestrator does not emit a spurious
// audit event for a response that bypassed authentication entirely.
func (s *gatewayServer) policyFilter(ex *Exchange) Result {
	ex.Policy, ex.PolicyErr = s.currentPolicy()
	if s.handleOAuthProtectedResource(ex.W, ex.R, ex.Policy) {
		ex.SkipAudit = true
		return Respond
	}
	return Continue
}
