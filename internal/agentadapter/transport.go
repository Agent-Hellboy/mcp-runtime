package agentadapter

import (
	"net/http"
	"time"
)

// RuntimeTransport is the shared outbound HTTP transport used by both the
// reverse proxy and the stdio shim when forwarding to the runtime. It owns
// the base round-tripper and the per-request timeout so production gates
// (mTLS, bearer auth, retries, OTel) get implemented in a single place and
// behave identically for both adapters.
type RuntimeTransport struct {
	// Base is the underlying round-tripper. nil means http.DefaultTransport
	// (production); tests can swap in a mock by setting this field.
	Base http.RoundTripper
	// Timeout is the per-request timeout applied to the *http.Client wrapper
	// returned by Client(). Zero means no timeout (matches the previous
	// "unbounded by default" behavior).
	Timeout time.Duration
}

// RoundTrip implements http.RoundTripper so the transport can plug directly
// into httputil.ReverseProxy.Transport.
func (t *RuntimeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base()
	return base.RoundTrip(req)
}

// Client returns an *http.Client whose Transport is this RuntimeTransport.
// Both the stdio shim and the reverse proxy route outbound requests through
// this wrapper so auth, OTel, and retry logic lives in one place.
func (t *RuntimeTransport) Client() *http.Client {
	var timeout time.Duration
	if t != nil {
		timeout = t.Timeout
	}
	return &http.Client{Transport: t, Timeout: timeout}
}

// CloseIdleConnections drains idle connections on the base round-tripper if
// it supports the optional interface, matching net/http's contract.
func (t *RuntimeTransport) CloseIdleConnections() {
	if closer, ok := t.base().(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (t *RuntimeTransport) base() http.RoundTripper {
	if t == nil || t.Base == nil {
		return http.DefaultTransport
	}
	return t.Base
}
