package platformauth

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const UnknownRequestIP = "unknown"

type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

func RequestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("x-forwarded-for")); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if ip := strings.TrimSpace(part); ip != "" {
				return ip
			}
		}
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	if remote != "" {
		return remote
	}
	return UnknownRequestIP
}

func RequestSource(r *http.Request) string {
	if r != nil {
		switch source := strings.ToLower(strings.TrimSpace(r.Header.Get("x-mcp-source"))); source {
		case "ui", "cli", "api":
			return source
		}
	}
	return "api"
}

func AuditSource(r *http.Request, p Principal) string {
	source := RequestSource(r)
	if p.AuthType == "" {
		return source
	}
	return source + ":" + p.AuthType
}

func AuditIdentityLabel(p Principal) string {
	switch {
	case p.APIKeyID != "":
		return "api_key:" + p.APIKeyID
	case p.Email != "" && p.AuthType != "":
		return p.AuthType + ":" + p.Email
	case p.Subject != "" && p.AuthType != "":
		return p.AuthType + ":" + p.Subject
	case p.AuthType != "":
		return p.AuthType
	default:
		return ""
	}
}
