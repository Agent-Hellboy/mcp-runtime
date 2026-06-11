package platformstore

import (
	"database/sql"
	"strings"
	"time"

	"mcp-runtime/pkg/platform"
)

const (
	platformJWTIssuer   = "mcp-runtime"
	platformJWTAudience = "platform"
	passwordProvider    = "password"
	defaultDBMaxConns   = 10
	defaultDBMaxIdle    = 5
)

// OIDCProviderPrefix prefixes provider names stored for OIDC identities.
const OIDCProviderPrefix = "oidc:"

const (
	// RoleAdmin is the platform-wide administrator role.
	RoleAdmin = "admin"
	// RoleUser is the regular signed-in platform user role.
	RoleUser = "user"
)

const (
	// SharedCatalogNamespace is the legacy shared MCP server catalog namespace.
	SharedCatalogNamespace = "mcp-servers"
	// TeamNamespacePrefix is prepended to normalized team slugs for managed namespaces.
	TeamNamespacePrefix = "mcp-team-"
	// TeamRoleOwner grants team administration privileges.
	TeamRoleOwner = "owner"
	// TeamRoleMember grants regular team membership privileges.
	TeamRoleMember = "member"
	// NamespaceScopeUser marks a namespace owned by a single platform user.
	NamespaceScopeUser = "user"
	// NamespaceScopeTeam marks a namespace owned by a platform team.
	NamespaceScopeTeam = "team"
)

// Store owns platform identity, API key, team, namespace, and audit persistence.
type Store struct {
	db        *sql.DB
	jwtSecret []byte
}

// User is the persisted platform account shape returned by identity operations.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Namespace string `json:"namespace"`
}

// Principal is the authenticated platform identity attached to API requests.
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

// PrincipalTeam describes a team membership embedded in an authenticated principal.
type PrincipalTeam struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

// UserID returns the platform user identifier carried by the principal subject.
func (p Principal) UserID() string {
	return strings.TrimSpace(p.Subject)
}

// HasNamespace reports whether the principal can see or use namespace.
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

// TeamRole returns the principal's role for a team slug, or empty when absent.
func (p Principal) TeamRole(slug string) string {
	slug = strings.TrimSpace(slug)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Slug) == slug {
			return strings.TrimSpace(team.Role)
		}
	}
	return ""
}

// TeamForNamespace returns the principal's team membership for a namespace.
func (p Principal) TeamForNamespace(namespace string) (PrincipalTeam, bool) {
	namespace = strings.TrimSpace(namespace)
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Namespace) == namespace {
			return team, true
		}
	}
	return PrincipalTeam{}, false
}

// Team is a managed platform team and its Kubernetes namespace.
type Team struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"created_at"`
}

// TeamMembership is the platform team membership API contract.
type TeamMembership = platform.TeamMembership

// APIKeySummary is the non-secret metadata for a user API key or registry credential.
type APIKeySummary struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	Revoked   bool       `json:"revoked"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// AuditEvent is the write-side platform audit envelope.
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

// AuditLog is the read-side platform audit record returned to administrators.
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

// OperationsFilter constrains platform operations and activity queries.
type OperationsFilter struct {
	User       string
	UserSearch string
	Since      time.Time
	Until      time.Time
	Limit      int
}

// OperationsFilterResponse is the API echo of an operations filter.
type OperationsFilterResponse struct {
	User  string `json:"user,omitempty"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	Limit int    `json:"limit"`
}

// UserActivity summarizes recent platform activity for a user account.
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

// ImageActivity describes an image publish or deploy-related audit entry.
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
