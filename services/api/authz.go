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
	Role              string          `json:"role"`
	Subject           string          `json:"subject,omitempty"`
	Email             string          `json:"email,omitempty"`
	Namespace         string          `json:"namespace,omitempty"`
	AllowedNamespaces []string        `json:"allowed_namespaces,omitempty"`
	Teams             []principalTeam `json:"teams,omitempty"`
	AuthType          string          `json:"auth_type,omitempty"`
	APIKeyID          string          `json:"api_key_id,omitempty"`
	IsService         bool            `json:"is_service,omitempty"`
}

type principalTeam struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

func (p principal) userID() string {
	return strings.TrimSpace(p.Subject)
}

func (p principal) hasNamespace(namespace string) bool {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return false
	}
	if strings.TrimSpace(p.Namespace) == namespace {
		return true
	}
	for _, allowed := range p.AllowedNamespaces {
		if strings.TrimSpace(allowed) == namespace {
			return true
		}
	}
	return false
}

func (p principal) teamRole(slug string) string {
	slug = strings.TrimSpace(slug)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Slug) == slug {
			return strings.TrimSpace(team.Role)
		}
	}
	return ""
}

func (p principal) teamForNamespace(namespace string) (principalTeam, bool) {
	namespace = strings.TrimSpace(namespace)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Namespace) == namespace {
			return team, true
		}
	}
	return principalTeam{}, false
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
