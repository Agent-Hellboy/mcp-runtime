package agentadapter

import (
	"net/http"
	"testing"
	"time"
)

func TestRuntimeTransportRoutesThroughBase(t *testing.T) {
	t.Parallel()

	var called bool
	transport := &RuntimeTransport{
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: http.StatusTeapot, Body: http.NoBody, Header: http.Header{}, Request: req}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "http://example/test", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if !called {
		t.Fatal("base round-tripper was not invoked")
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
}

func TestRuntimeTransportClientAppliesTimeout(t *testing.T) {
	t.Parallel()

	transport := &RuntimeTransport{Timeout: 7 * time.Second}
	client := transport.Client()
	if client.Timeout != 7*time.Second {
		t.Fatalf("client.Timeout = %s, want 7s", client.Timeout)
	}
	if client.Transport != transport {
		t.Fatalf("client.Transport = %#v, want transport", client.Transport)
	}
}

func TestRuntimeTransportNilBaseUsesDefault(t *testing.T) {
	t.Parallel()

	transport := &RuntimeTransport{}
	if got := transport.base(); got != http.DefaultTransport {
		t.Fatalf("base() = %#v, want http.DefaultTransport", got)
	}
}
