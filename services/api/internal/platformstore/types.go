package platformstore

import (
	"database/sql"
	"strings"
	"time"
)

const (
	platformJWTIssuer   = "mcp-runtime"
	platformJWTAudience = "platform"
	passwordProvider    = "password"
	defaultDBMaxConns   = 10
	defaultDBMaxIdle    = 5
)

const OIDCProviderPrefix = "oidc:"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

const (
	SharedCatalogNamespace = "mcp-servers"
	TeamNamespacePrefix    = "mcp-team-"
	TeamRoleOwner          = "owner"
	TeamRoleMember         = "member"
	NamespaceScopeUser     = "user"
	NamespaceScopeTeam     = "team"
)

type Store struct {
	db        *sql.DB
	jwtSecret []byte
}

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Namespace string `json:"namespace"`
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

type PrincipalTeam struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

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

type Team struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamMembership struct {
	TeamID        string    `json:"team_id"`
	TeamSlug      string    `json:"team_slug"`
	TeamName      string    `json:"team_name"`
	TeamNamespace string    `json:"team_namespace"`
	UserID        string    `json:"user_id"`
	Email         string    `json:"email,omitempty"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
}

type APIKeySummary struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	Revoked   bool       `json:"revoked"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type AuditEvent struct {
	UserID           string
	Action           string
	Resource         string
	Namespace        string
	Status           string
	Message          string
	ActorIP          string
	RequestID        string
	Source           string
	AuthIdentity     string
	ImageRef         string
	ServerName       string
	DeploymentTarget string
}

type AuditLog struct {
	ID               int64     `json:"id"`
	UserID           string    `json:"user_id,omitempty"`
	Email            string    `json:"email,omitempty"`
	Action           string    `json:"action"`
	Resource         string    `json:"resource"`
	Namespace        string    `json:"namespace,omitempty"`
	Status           string    `json:"status"`
	Message          string    `json:"message,omitempty"`
	ActorIP          string    `json:"actor_ip,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
	Source           string    `json:"source,omitempty"`
	AuthIdentity     string    `json:"auth_identity,omitempty"`
	ImageRef         string    `json:"image_ref,omitempty"`
	ServerName       string    `json:"server_name,omitempty"`
	DeploymentTarget string    `json:"deployment_target,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type OperationsFilter struct {
	User       string
	UserSearch string
	Since      time.Time
	Until      time.Time
	Limit      int
}

type OperationsFilterResponse struct {
	User  string `json:"user,omitempty"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	Limit int    `json:"limit"`
}

type UserActivity struct {
	ID                  string     `json:"id"`
	Email               string     `json:"email"`
	Role                string     `json:"role"`
	Namespace           string     `json:"namespace,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	LastLoginAt         *time.Time `json:"last_login_at,omitempty"`
	LastActivityAt      *time.Time `json:"last_activity_at,omitempty"`
	LoginCount          int64      `json:"login_count"`
	FailedActionCount   int64      `json:"failed_action_count"`
	RegistryCredentials int64      `json:"registry_credentials"`
	APIKeys             int64      `json:"api_keys"`
}

type ImageActivity struct {
	UserID           string    `json:"user_id,omitempty"`
	Email            string    `json:"email,omitempty"`
	Namespace        string    `json:"namespace,omitempty"`
	ImageRef         string    `json:"image_ref"`
	SourceImage      string    `json:"source_image,omitempty"`
	ServerName       string    `json:"server_name,omitempty"`
	DeploymentTarget string    `json:"deployment_target,omitempty"`
	Action           string    `json:"action"`
	Status           string    `json:"status"`
	Source           string    `json:"source,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
