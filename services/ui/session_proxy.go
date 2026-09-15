package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"mcp-runtime/pkg/serviceutil"
)

const (
	defaultRuntimeUpstream   = "http://mcp-runtime-api.mcp-sentinel.svc.cluster.local:8084"
	defaultAnalyticsUpstream = "http://mcp-analytics-api.mcp-sentinel.svc.cluster.local:8085"
	uiSessionAPIPrefix       = "/api/ui/v1"
	sessionProxyTimeout      = 15 * time.Second
)

var sessionProxyRuntimePrefixes = []string{
	"/dashboard/summary",
	"/runtime/namespaces",
	"/runtime/servers",
	"/runtime/tools",
	"/runtime/server-events",
	"/runtime/observability/links",
	"/runtime/observability/grafana/dashboard",
	"/runtime/observability/prometheus/query",
	"/runtime/teams",
	"/runtime/grants",
	"/runtime/sessions",
	"/runtime/components",
	"/runtime/policy",
	"/user/api-keys",
	"/admin/operations",
	"/admin/deployments",
}

var sessionProxyAnalyticsPrefixes = []string{
	"/events",
	"/analytics/usage",
	"/user/analytics/usage",
}

// sessionProxyWriteRoutes is the allowlist of state-changing routes the UI
// session may reach, kept separate from the read allowlist so that widening
// reads can never silently widen writes. Each entry is matched exactly or as a
// path prefix when Wildcard is set, and only for the listed methods.
//
// Phase 3 needs exactly two: create a user API key, and revoke one.
var sessionProxyWriteRoutes = []sessionProxyWriteRoute{
	{Prefix: "/user/api-keys", Methods: []string{http.MethodPost}},
	{Prefix: "/user/api-keys/", Methods: []string{http.MethodDelete}, Wildcard: true},
}

type sessionProxyWriteRoute struct {
	Prefix   string
	Methods  []string
	Wildcard bool
}

// sessionProxyRouteMatches reports whether a request path belongs to a write
// route. A wildcard route requires exactly one more non-empty segment after its
// prefix, so it matches neither its own bare prefix (an empty resource id) nor
// anything nested deeper.
func sessionProxyRouteMatches(route sessionProxyWriteRoute, requestPath string) bool {
	if !route.Wildcard {
		return requestPath == route.Prefix
	}
	rest, ok := strings.CutPrefix(requestPath, route.Prefix)
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// sessionProxyWriteAllowed reports whether a method and path pair is on the
// write allowlist. Everything not listed is denied.
func sessionProxyWriteAllowed(method, requestPath string) bool {
	for _, route := range sessionProxyWriteRoutes {
		if !sessionProxyRouteMatches(route, requestPath) {
			continue
		}
		for _, allowed := range route.Methods {
			if allowed == method {
				return true
			}
		}
	}
	return false
}

// sessionProxyMaxWriteBody bounds proxied request bodies. Phase 3 writes are
// small JSON documents; anything larger is a bug or an attack.
const sessionProxyMaxWriteBody = 32 * 1024

var sessionProxyHTTPClient = &http.Client{
	Timeout: sessionProxyTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type sessionProxy struct {
	runtimeBase   *url.URL
	analyticsBase *url.URL
	store         *uiSessionStore
	client        *http.Client
}

func parseRuntimeUpstream(raw string) (*url.URL, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		return nil, errors.New("runtime upstream is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("runtime upstream must use http or https")
	}
	if u.Host == "" {
		return nil, errors.New("runtime upstream must include scheme and host")
	}
	if u.User != nil {
		return nil, errors.New("runtime upstream must not include userinfo")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func newSessionProxy(runtimeBase *url.URL, store *uiSessionStore) *sessionProxy {
	return newSessionProxyWithUpstreams(runtimeBase, runtimeBase, store)
}

func newSessionProxyWithUpstreams(runtimeBase, analyticsBase *url.URL, store *uiSessionStore) *sessionProxy {
	return &sessionProxy{
		runtimeBase:   runtimeBase,
		analyticsBase: analyticsBase,
		store:         store,
		client:        sessionProxyHTTPClient,
	}
}

func sessionProxyUpstreamPath(requestPath string) (string, bool) {
	_, upstreamPath, ok := sessionProxyRoute(requestPath)
	return upstreamPath, ok
}

func sessionProxyRoute(requestPath string) (*url.URL, string, bool) {
	cleaned := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(requestPath), "/"))
	suffix, ok := strings.CutPrefix(cleaned, uiSessionAPIPrefix)
	if !ok || suffix == "" {
		return nil, "", false
	}
	if sessionProxyPathAllowed(suffix, sessionProxyRuntimePrefixes) {
		return nil, "/api/v1" + suffix, true
	}
	if sessionProxyPathAllowed(suffix, sessionProxyAnalyticsPrefixes) {
		return nil, "/api/v1" + suffix, true
	}
	return nil, "", false
}

func sessionProxyPathAllowed(requestPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}

func (p *sessionProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	write := unsafeHTTPMethod(r.Method)
	base, upstreamPath, ok := sessionProxyRoute(r.URL.Path)
	if !ok {
		serviceutil.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if write {
		suffix := strings.TrimPrefix(upstreamPath, "/api/v1")
		if !sessionProxyWriteAllowed(r.Method, suffix) {
			w.Header().Set("allow", sessionProxyAllowHeader(suffix))
			serviceutil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
			return
		}
	}
	sess, ok := p.store.sessionFromRequest(r)
	if !ok {
		serviceutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// Authenticate before CSRF so an expired session reads as 401 (sign in
	// again) rather than 403 (forbidden), which is what the client acts on.
	authHeader, apiKey, ok := sessionUpstreamCredential(sess)
	if !ok {
		serviceutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if status := verifyCSRF(r, sess); status != 0 {
		// #nosec G706 -- path-only telemetry; no credential or token material.
		log.Printf("ui session proxy csrf rejection method=%q path=%q", r.Method, r.URL.Path)
		serviceutil.WriteJSON(w, status, map[string]string{"error": "csrf_failed"})
		return
	}

	var body io.Reader
	if write && r.Body != nil {
		body = http.MaxBytesReader(w, r.Body, sessionProxyMaxWriteBody)
	}

	if strings.HasPrefix(upstreamPath, "/api/v1/events") ||
		strings.HasPrefix(upstreamPath, "/api/v1/analytics/usage") ||
		strings.HasPrefix(upstreamPath, "/api/v1/user/analytics/usage") {
		base = p.analyticsBase
	} else {
		base = p.runtimeBase
	}
	upstreamURL, err := resolveSessionProxyURL(base, upstreamPath, r.URL.RawQuery)
	if err != nil {
		serviceutil.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream_error"})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		serviceutil.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream_error"})
		return
	}
	// Forward only the content type; the CSRF token is a UI-origin concern and
	// must never be relayed upstream.
	if write {
		contentType := strings.TrimSpace(r.Header.Get("content-type"))
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("content-type", contentType)
	}
	if accept := strings.TrimSpace(r.Header.Get("accept")); accept != "" {
		req.Header.Set("accept", accept)
	} else {
		req.Header.Set("accept", "application/json")
	}
	copySessionProxyOriginHeaders(req, r)
	req.Header.Set("x-mcp-source", "ui")
	if authHeader != "" {
		req.Header.Set("authorization", authHeader)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}

	client := p.client
	if client == nil {
		client = sessionProxyHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// #nosec G706 -- path-only telemetry; upstream URL and credentials are omitted.
		log.Printf("ui session proxy upstream error path=%q", r.URL.Path)
		serviceutil.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream_error"})
		return
	}
	copySessionProxyResponse(w, resp)
}

// copySessionProxyOriginHeaders preserves the public origin used by runtime-api
// when it builds browser-facing connect and observability URLs. The ingress
// supplied forwarded values win; direct requests use the UI request host and
// transport as a safe fallback. No client credential or unrelated proxy header
// is forwarded.
func copySessionProxyOriginHeaders(dst, src *http.Request) {
	forwardedHost := strings.TrimSpace(src.Header.Get("x-forwarded-host"))
	if forwardedHost == "" {
		forwardedHost = strings.TrimSpace(src.Host)
	}
	if forwardedHost != "" {
		dst.Header.Set("x-forwarded-host", forwardedHost)
	}

	forwardedProto := strings.TrimSpace(src.Header.Get("x-forwarded-proto"))
	if forwardedProto == "" {
		forwardedProto = "http"
		if src.TLS != nil {
			forwardedProto = "https"
		}
	}
	dst.Header.Set("x-forwarded-proto", forwardedProto)
}

func sessionUpstreamCredential(sess uiSession) (authHeader, apiKey string, ok bool) {
	if header := strings.TrimSpace(sess.UpstreamAuthHeader); header != "" {
		return header, "", true
	}
	if key := strings.TrimSpace(sess.UpstreamAPIKey); key != "" {
		return "", key, true
	}
	return "", "", false
}

func resolveSessionProxyURL(base *url.URL, upstreamPath, rawQuery string) (*url.URL, error) {
	if base == nil {
		return nil, errors.New("runtime upstream is empty")
	}
	ref, err := url.Parse(upstreamPath)
	if err != nil {
		return nil, err
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host {
		return nil, errors.New("runtime upstream redirected off host")
	}
	resolved.RawQuery = rawQuery
	resolved.Fragment = ""
	return resolved, nil
}

func copySessionProxyResponse(w http.ResponseWriter, resp *http.Response) {
	defer drainAndClose(resp.Body)
	dst := w.Header()
	for key, values := range resp.Header {
		if skipSessionProxyResponseHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func skipSessionProxyResponseHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Authorization", "Cookie", "Set-Cookie", "Connection", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Trailers",
		"Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

// sessionProxyAllowHeader lists the methods a path actually accepts, so a
// rejected write reports the truth instead of a blanket "GET".
func sessionProxyAllowHeader(requestPath string) string {
	methods := []string{http.MethodGet}
	for _, route := range sessionProxyWriteRoutes {
		if sessionProxyRouteMatches(route, requestPath) {
			methods = append(methods, route.Methods...)
		}
	}
	return strings.Join(methods, ", ")
}
