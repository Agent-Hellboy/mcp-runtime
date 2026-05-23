package apiauth

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"mcp-sentinel-api/internal/platformstore"
)

const (
	RoleAdmin = platformstore.RoleAdmin
	RoleUser  = platformstore.RoleUser
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
	if r == nil {
		return ""
	}
	remoteHost := requestRemoteHost(r.RemoteAddr)
	remoteAddr, remoteOK := parseIPLiteral(remoteHost)
	forwarded := forwardedForAddrs(r.Header.Get("x-forwarded-for"))
	if len(forwarded) == 0 || !remoteOK || !trustedProxyAddr(remoteAddr) {
		return remoteHost
	}
	for i := len(forwarded) - 1; i >= 0; i-- {
		if !trustedProxyAddr(forwarded[i]) {
			return forwarded[i].String()
		}
	}
	return forwarded[0].String()
}

func requestRemoteHost(remote string) string {
	remote = strings.TrimSpace(remote)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return strings.TrimSpace(host)
	}
	return strings.Trim(remote, "[]")
}

func forwardedForAddrs(value string) []netip.Addr {
	values := strings.Split(value, ",")
	addrs := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, ok := parseIPLiteral(value)
		if ok {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func parseIPLiteral(raw string) (netip.Addr, bool) {
	raw = strings.Trim(strings.TrimSpace(raw), "\"[]")
	if raw == "" || strings.EqualFold(raw, "unknown") {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = strings.Trim(host, "[]")
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func trustedProxyAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	cidrs := strings.TrimSpace(os.Getenv("PLATFORM_TRUSTED_PROXY_CIDRS"))
	if cidrs == "" {
		cidrs = strings.TrimSpace(os.Getenv("MCP_TRUSTED_PROXY_CIDRS"))
	}
	if cidrs != "" {
		for _, raw := range strings.Split(cidrs, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
			if err == nil && prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
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
