package agentadapter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	proxyReadHeaderTimeout = 5 * time.Second
	proxyShutdownTimeout   = 10 * time.Second
)

type rpcRequestMetadataContextKey struct{}

// NewHTTPProxyHandler returns a reverse proxy that forwards MCP HTTP traffic to
// the configured runtime route and injects issued governance identity headers.
func NewHTTPProxyHandler(cfg Config) (http.Handler, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	target := cloneURL(cfg.RuntimeURL)
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(req *httputil.ProxyRequest) {
			rewriteToRuntimeRoute(req.Out.URL, target, req.In.URL.RawQuery)
			req.Out.Host = target.Host
			if cfg.HostHeader != "" {
				req.Out.Host = cfg.HostHeader
			}
			if !cfg.DisableXForwarded {
				req.SetXForwarded()
			}
			applyGovernanceHeaders(req.Out.Header, cfg)
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode < http.StatusBadRequest || resp.StatusCode >= http.StatusInternalServerError {
				return nil
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
			meta := rpcRequestMetadataFromContext(resp.Request.Context())
			logRuntimeDenial(cfg, "mcp-runtime-agent-proxy", resp.StatusCode, extractHTTPErrorMessage(resp.StatusCode, body), meta)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			meta := rpcRequestMetadataFromContext(r.Context())
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(jsonRPCHTTPError(rpcIDOrNull(meta), http.StatusBadGateway, err.Error(), nil))
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		meta, err := captureRPCRequestMetadata(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), rpcRequestMetadataContextKey{}, meta))
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
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: proxyReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), proxyShutdownTimeout)
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

func captureRPCRequestMetadata(r *http.Request) (rpcRequestMetadata, error) {
	if r.Body == nil {
		return rpcRequestMetadata{}, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return rpcRequestMetadata{}, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return parseRPCRequestMetadata(bytes.TrimSpace(body)), nil
}

func rpcRequestMetadataFromContext(ctx context.Context) rpcRequestMetadata {
	meta, _ := ctx.Value(rpcRequestMetadataContextKey{}).(rpcRequestMetadata)
	return meta
}
