package main

import (
	"context"
	"net/http"
	"strings"
)

func (s *RuntimeServer) writeAudit(ctx context.Context, ev auditEvent) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.WriteAudit(ctx, ev)
}

func auditSource(r *http.Request, p principal) string {
	source := requestSource(r)
	if p.AuthType == "" {
		return source
	}
	return source + ":" + p.AuthType
}

func requestSource(r *http.Request) string {
	if r != nil {
		switch source := strings.ToLower(strings.TrimSpace(r.Header.Get("x-mcp-source"))); source {
		case "ui", "cli", "api":
			return source
		}
	}
	return "api"
}

func auditIdentityLabel(p principal) string {
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
