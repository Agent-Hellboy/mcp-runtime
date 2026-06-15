package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"go.uber.org/zap"

	"mcp-runtime-control/internal/apierr"
	"mcp-runtime-control/internal/platformclient"
	"mcp-runtime/pkg/platformauth"
	"mcp-runtime/pkg/serviceutil"
)

const (
	roleAdmin               = platformauth.RoleAdmin
	roleUser                = platformauth.RoleUser
	sharedCatalogNamespace  = "mcp-servers"
	teamNamespacePrefix     = "mcp-team-"
	teamRoleOwner           = "owner"
	teamRoleMember          = "member"
	namespaceScopeUser      = "user"
	namespaceScopeTeam      = "team"
	accessApplyMaxBytes     = 64 * 1024
	deploymentApplyMaxBytes = 32 * 1024
	teamApplyMaxBytes       = 8 * 1024
)

// Principal is the authenticated platform identity contract shared by runtime handlers.
type Principal = platformauth.Principal
type principal = platformauth.Principal
type principalTeam = platformauth.PrincipalTeam
type teamRecord = platformclient.Team
type teamMembershipRecord = platformclient.TeamMembership
type auditEvent = platformclient.AuditEvent
type userAPIKeySummary = platformclient.APIKeySummary

type identityStore interface {
	ListTeams(ctx context.Context) ([]teamRecord, error)
	GetTeamBySlug(ctx context.Context, slug string) (teamRecord, bool, error)
	CreateTeam(ctx context.Context, slug, name, createdByUserID string) (teamRecord, error)
	DeleteTeamBySlug(ctx context.Context, slug string) error
	ListNamespaces(ctx context.Context) ([]map[string]any, error)
	GetNamespace(ctx context.Context, namespace string) (map[string]any, bool, error)
	ListTeamMemberships(ctx context.Context, teamSlug string) ([]teamMembershipRecord, error)
	UpsertTeamMembership(ctx context.Context, teamSlug, userID, role string) (teamMembershipRecord, error)
	DeleteTeamMembership(ctx context.Context, teamSlug, userID string) error
	CreatePasswordUser(ctx context.Context, email, password, role string) (platformclient.User, error)
	CreateTeamUser(ctx context.Context, teamSlug, email, password, role string) (platformclient.User, teamMembershipRecord, error)
	Configured() bool
}

type auditWriter interface {
	WriteAudit(context.Context, auditEvent)
}

func principalFromContext(ctx context.Context) (principal, bool) {
	return platformauth.FromContext(ctx)
}

func withPrincipal(ctx context.Context, p principal) context.Context {
	return platformauth.WithPrincipal(ctx, p)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	serviceutil.WriteJSON(w, status, payload)
}

func writeBodyDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeAPIError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds %d bytes", maxBytesErr.Limit), err)
		return
	}
	writeAPIError(w, http.StatusBadRequest, "invalid request body", err)
}

func writeAPIError(w http.ResponseWriter, status int, message string, cause ...error) {
	apierr.Write(w, zap.L(), newAPIError(status, errorCode(message, status), message, cause...))
}

func newAPIError(status int, code, message string, cause ...error) error {
	switch status {
	case http.StatusBadRequest:
		return apierr.BadRequest(code, message, cause...)
	case http.StatusUnauthorized:
		return apierr.Unauthorized(code, message, cause...)
	case http.StatusForbidden:
		return apierr.Forbidden(code, message, cause...)
	case http.StatusNotFound:
		return apierr.NotFound(code, message, cause...)
	case http.StatusConflict:
		return apierr.Conflict(code, message, cause...)
	case http.StatusInternalServerError:
		return apierr.Internal(code, message, cause...)
	case http.StatusServiceUnavailable:
		return apierr.ServiceUnavailable(code, message, cause...)
	default:
		return &apierr.Error{Status: status, Code: code, Message: message, Cause: errors.Join(cause...)}
	}
}

func errorCode(message string, status int) string {
	if message = strings.TrimSpace(message); message == "" {
		message = http.StatusText(status)
	}
	var b strings.Builder
	previousUnderscore := false
	for _, r := range strings.ToLower(message) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			previousUnderscore = false
			continue
		}
		if !previousUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			previousUnderscore = true
		}
	}
	code := strings.Trim(b.String(), "_")
	if code == "" {
		return "error"
	}
	return code
}

func requestIP(r *http.Request) string {
	return platformauth.RequestIP(r)
}

func auditSource(r *http.Request, p principal) string {
	return platformauth.AuditSource(r, p)
}

func auditIdentityLabel(p principal) string {
	return platformauth.AuditIdentityLabel(p)
}

func envOr(key, fallback string) string {
	return serviceutil.EnvOr(key, fallback)
}

// NormalizeTeamSlug canonicalizes a team slug using the platform store rules shared with identity APIs.
func NormalizeTeamSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "_", "-")
	return raw
}
