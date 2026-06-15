// Package internalapi holds shared request/response DTOs for platform-api
// /internal/* endpoints. Provider and consumer services import these structs so
// field renames become compile errors across the split.
package internalapi

import (
	"time"

	"mcp-runtime/pkg/platform"
	"mcp-runtime/pkg/platformauth"
)

// AuthResolveRequest is POST /internal/auth/resolve.
type AuthResolveRequest struct {
	APIKey string `json:"api_key"`
}

// AuthResolveResponse is POST /internal/auth/resolve.
type AuthResolveResponse struct {
	OK        bool                   `json:"ok"`
	Principal platformauth.Principal `json:"principal,omitempty"`
}

// PrincipalResolveRequest is POST /internal/identity/principal.
type PrincipalResolveRequest struct {
	UserID string `json:"user_id"`
}

// PrincipalResolveResponse is POST /internal/identity/principal.
type PrincipalResolveResponse struct {
	Principal platformauth.Principal `json:"principal"`
}

// ResolveIDsRequest is POST /internal/identity/resolve-ids.
type ResolveIDsRequest struct {
	UserIDs []string `json:"user_ids"`
	TeamIDs []string `json:"team_ids"`
}

// ResolveIDsResponse is POST /internal/identity/resolve-ids.
type ResolveIDsResponse struct {
	Users map[string]string `json:"users"`
	Teams map[string]string `json:"teams"`
}

// AuditEvent is POST /internal/audit.
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

// TeamCreateRequest is POST /internal/identity/teams.
type TeamCreateRequest struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	CreatedByUserID string `json:"created_by_user_id"`
}

// TeamsListResponse is GET /internal/identity/teams.
type TeamsListResponse struct {
	Teams []Team `json:"teams"`
}

// Team is a managed platform team and its Kubernetes namespace.
type Team struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"created_at"`
}

// TeamMembershipUpsertRequest is POST /internal/identity/teams/{slug}/members.
type TeamMembershipUpsertRequest struct {
	UserID string `json:"userID"`
	Role   string `json:"role"`
}

// TeamMembershipPutRequest is PUT /internal/identity/teams/{slug}/members/{userID}.
type TeamMembershipPutRequest struct {
	Role   string `json:"role"`
	UserID string `json:"userID"`
}

// TeamMembershipResponse wraps a membership mutation result.
type TeamMembershipResponse struct {
	Membership TeamMembership `json:"membership"`
}

// TeamMembersListResponse is GET /internal/identity/teams/{slug}/members.
type TeamMembersListResponse struct {
	Members []TeamMembership `json:"members"`
}

// TeamMembership is the platform team membership API contract.
type TeamMembership = platform.TeamMembership

// CreateUserRequest is POST /internal/identity/users.
type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUserResponse is POST /internal/identity/users.
type CreateUserResponse struct {
	User User `json:"user"`
}

// TeamUserCreateRequest is POST /internal/identity/teams/{slug}/users.
type TeamUserCreateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// TeamUserCreateResponse is POST /internal/identity/teams/{slug}/users.
type TeamUserCreateResponse struct {
	User       User           `json:"user"`
	Membership TeamMembership `json:"membership"`
}

// User is a platform user record returned by identity APIs.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// NamespacesListResponse is GET /internal/identity/namespaces.
type NamespacesListResponse struct {
	Namespaces []map[string]any `json:"namespaces"`
}
