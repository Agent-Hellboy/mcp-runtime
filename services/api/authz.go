package main

import (
	"context"
	"strings"
)

const (
	roleAdmin = "admin"
	roleUser  = "user"
)

type principal struct {
	Role      string `json:"role"`
	Subject   string `json:"subject,omitempty"`
	Email     string `json:"email,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	AuthType  string `json:"auth_type,omitempty"`
	APIKeyID  string `json:"api_key_id,omitempty"`
	IsService bool   `json:"is_service,omitempty"`
}

func (p principal) userID() string {
	return strings.TrimSpace(p.Subject)
}

type principalContextKey struct{}

func principalFromContext(ctx context.Context) (principal, bool) {
	v := ctx.Value(principalContextKey{})
	if v == nil {
		return principal{}, false
	}
	p, ok := v.(principal)
	return p, ok
}
