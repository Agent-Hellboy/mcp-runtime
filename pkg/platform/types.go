package platform

import "time"

// TeamMembership is the shared API contract for platform team membership data.
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
