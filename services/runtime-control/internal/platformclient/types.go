package platformclient

import (
	"time"

	"mcp-runtime/pkg/platform"
)

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

// User is a platform user record returned by identity APIs.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// APIKeySummary is the non-secret metadata for a user API key.
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
	UserID           string `json:"user_id,omitempty"`
	Action           string `json:"action"`
	Resource         string `json:"resource"`
	Namespace        string `json:"namespace,omitempty"`
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
	ActorIP          string `json:"actor_ip,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	Source           string `json:"source,omitempty"`
	AuthIdentity     string `json:"auth_identity,omitempty"`
	ImageRef         string `json:"image_ref,omitempty"`
	ServerName       string `json:"server_name,omitempty"`
	DeploymentTarget string `json:"deployment_target,omitempty"`
}

// OperationsFilter constrains platform operations and activity queries.
type OperationsFilter struct {
	User       string
	UserSearch string
	Since      time.Time
	Until      time.Time
	Limit      int
}

// OperationsSnapshot is the platform activity bundle for admin operations.
type OperationsSnapshot struct {
	Users     []UserActivity  `json:"users"`
	AuditLogs []AuditLog      `json:"audit_logs"`
	Images    []ImageActivity `json:"images"`
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

// AuditLog is the read-side platform audit record.
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

// ImageActivity describes image publish or deploy-related audit entries.
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

// OperationsFilterResponse is the API echo of an operations filter.
type OperationsFilterResponse struct {
	User  string `json:"user,omitempty"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	Limit int    `json:"limit"`
}
