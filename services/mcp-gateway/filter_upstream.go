package main

// upstreamFilter is stage 5 of the gateway pipeline. It rewrites identity
// headers and the upstream token on the outbound request, strips any configured
// path prefix, and forwards the request to the upstream MCP server via the
// reverse proxy.
//
// upstreamFilter reads Exchange.Policy, Exchange.Identity, and Exchange.OAuthToken
// (all set by earlier stages) and must not mutate them. It always returns Respond
// so the pipeline halts and stage 6 (audit) runs from the orchestrator.
func (s *gatewayServer) upstreamFilter(ex *Exchange) Result {
	s.applyIdentityHeaders(ex.R, ex.Policy, ex.Identity)
	s.applyUpstreamToken(ex.R, ex.Policy, ex.OAuthToken)

	if trimmedPath, ok := trimRequestPathPrefix(ex.R.URL.Path, s.stripPrefix); ok {
		ex.R.URL.Path = trimmedPath
		// Always clear RawPath when Path was trimmed. If RawPath trims cleanly
		// keep the percent-encoded form; otherwise clear it so Go's URL machinery
		// falls back to escaping Path — preventing an inconsistent URL where Path
		// is stripped but RawPath still carries the prefix.
		if trimmedRaw, rawOK := trimRequestPathPrefix(ex.R.URL.RawPath, s.stripPrefix); rawOK {
			ex.R.URL.RawPath = trimmedRaw
		} else {
			ex.R.URL.RawPath = ""
		}
		if ex.R.URL.Path == "" {
			ex.R.URL.Path = "/"
			if ex.R.URL.RawPath != "" {
				ex.R.URL.RawPath = "/"
			}
		}
	}

	s.proxy.ServeHTTP(ex.W, ex.R)
	return Respond
}
