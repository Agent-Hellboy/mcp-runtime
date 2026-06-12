package apiauth

import (
	"context"
	"net"
	"net/http"
	"strings"

	"mcp-sentinel-api/internal/platformstore"
)

const (
	RoleAdmin = platformstore.RoleAdmin
	RoleUser  = platformstore.RoleUser

	// UnknownRequestIP is the lockout/rate-limit bucket when no client IP can be
	// derived. Callers must not treat an empty string as a distinct bucket.
	UnknownRequestIP = "unknown"
)

type Principal = platformstore.Principal
type PrincipalTeam = platformstore.PrincipalTeam

type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	v := ctx.Value(contextKey{})
	if v == nil {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}

func RequestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("x-forwarded-for")); xff != "" {
		if ip := firstNonEmptyForwardedIP(xff); ip != "" {
			return ip
		}
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		if host = strings.TrimSpace(host); host != "" {
			return host
		}
	}
	if remote != "" {
		return remote
	}
	return UnknownRequestIP
}

func firstNonEmptyForwardedIP(xff string) string {
	for _, part := range strings.Split(xff, ",") {
		if ip := strings.TrimSpace(part); ip != "" {
			return ip
		}
	}
	return ""
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
