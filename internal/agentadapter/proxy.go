package agentadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// NewHTTPProxyHandler returns a reverse proxy that forwards MCP HTTP traffic to
// the configured runtime route and injects issued governance identity headers.
func NewHTTPProxyHandler(cfg Config) (http.Handler, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	target := cloneURL(cfg.RuntimeURL)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			rewriteToRuntimeRoute(req.Out.URL, target, req.In.URL.RawQuery)
			req.Out.Host = target.Host
			if cfg.HostHeader != "" {
				req.Out.Host = cfg.HostHeader
			}
			req.SetXForwarded()
			applyGovernanceHeaders(req.Out.Header, cfg)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

// RunHTTPProxy serves the local HTTP adapter until the context is cancelled.
func RunHTTPProxy(ctx context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	handler, err := NewHTTPProxyHandler(cfg)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultHTTPClientLimit)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func rewriteToRuntimeRoute(out, target *url.URL, inboundRawQuery string) {
	out.Scheme = target.Scheme
	out.Host = target.Host
	out.Path = target.Path
	out.RawPath = target.RawPath
	out.OmitHost = false
	out.ForceQuery = false
	out.Fragment = ""
	out.RawQuery = mergeRawQuery(target.RawQuery, inboundRawQuery)
}

func mergeRawQuery(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + "&" + second
	}
}
