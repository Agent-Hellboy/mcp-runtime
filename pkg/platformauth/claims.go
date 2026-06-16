package platformauth

import (
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type TeamClaim struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

type Claims struct {
	jwt.RegisteredClaims
	Email             string      `json:"email,omitempty"`
	Role              string      `json:"role,omitempty"`
	Namespace         string      `json:"namespace,omitempty"`
	Teams             []TeamClaim `json:"teams,omitempty"`
	AllowedNamespaces []string    `json:"allowed_namespaces,omitempty"`
	APIKeyID          string      `json:"api_key_id,omitempty"`
	AuthType          string      `json:"auth_type,omitempty"`
	IsService         bool        `json:"is_service,omitempty"`
}

type Principal struct {
	Role              string          `json:"role"`
	Subject           string          `json:"subject,omitempty"`
	Email             string          `json:"email,omitempty"`
	Namespace         string          `json:"namespace,omitempty"`
	AllowedNamespaces []string        `json:"allowed_namespaces,omitempty"`
	Teams             []PrincipalTeam `json:"teams,omitempty"`
	AuthType          string          `json:"auth_type,omitempty"`
	APIKeyID          string          `json:"api_key_id,omitempty"`
	IsService         bool            `json:"is_service,omitempty"`
}

type PrincipalTeam = TeamClaim

func (p Principal) UserID() string {
	return strings.TrimSpace(p.Subject)
}

func (p Principal) HasNamespace(namespace string) bool {
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

func (p Principal) TeamRole(slug string) string {
	slug = strings.TrimSpace(slug)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Slug) == slug {
			return strings.TrimSpace(team.Role)
		}
	}
	return ""
}

func (p Principal) TeamForNamespace(namespace string) (PrincipalTeam, bool) {
	namespace = strings.TrimSpace(namespace)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Namespace) == namespace {
			return team, true
		}
	}
	return PrincipalTeam{}, false
}

func ToPrincipal(claims Claims) Principal {
	return Principal{
		Role:              claims.Role,
		Subject:           claims.Subject,
		Email:             claims.Email,
		Namespace:         claims.Namespace,
		AllowedNamespaces: append([]string(nil), claims.AllowedNamespaces...),
		Teams:             append([]PrincipalTeam(nil), claims.Teams...),
		AuthType:          claims.AuthType,
		APIKeyID:          claims.APIKeyID,
		IsService:         claims.IsService,
	}
}

func ClaimsFromPrincipal(p Principal) Claims {
	return Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: p.Subject},
		Email:            p.Email,
		Role:             p.Role,
		Namespace:        p.Namespace,
		Teams:            append([]TeamClaim(nil), p.Teams...),
		AllowedNamespaces: append(
			[]string(nil),
			p.AllowedNamespaces...,
		),
		APIKeyID:  p.APIKeyID,
		AuthType:  p.AuthType,
		IsService: p.IsService,
	}
}
