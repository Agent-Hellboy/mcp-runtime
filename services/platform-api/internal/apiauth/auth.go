package apiauth

import (
	"context"
	"net/http"

	"mcp-runtime/pkg/platformauth"
)

const (
	RoleAdmin        = platformauth.RoleAdmin
	RoleUser         = platformauth.RoleUser
	UnknownRequestIP = platformauth.UnknownRequestIP
)

type Principal = platformauth.Principal
type PrincipalTeam = platformauth.PrincipalTeam

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return platformauth.WithPrincipal(ctx, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	return platformauth.FromContext(ctx)
}

func RequestIP(r *http.Request) string {
	return platformauth.RequestIP(r)
}

func RequestSource(r *http.Request) string {
	return platformauth.RequestSource(r)
}

func AuditSource(r *http.Request, p Principal) string {
	return platformauth.AuditSource(r, p)
}

func AuditIdentityLabel(p Principal) string {
	return platformauth.AuditIdentityLabel(p)
}
