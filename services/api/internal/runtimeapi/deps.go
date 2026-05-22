package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"go.uber.org/zap"

	"mcp-runtime/pkg/serviceutil"
	"mcp-sentinel-api/internal/apiauth"
	"mcp-sentinel-api/internal/apierr"
	"mcp-sentinel-api/internal/apihttp"
	"mcp-sentinel-api/internal/platformstore"
)

const (
	roleAdmin               = apiauth.RoleAdmin
	roleUser                = apiauth.RoleUser
	sharedCatalogNamespace  = platformstore.SharedCatalogNamespace
	teamNamespacePrefix     = platformstore.TeamNamespacePrefix
	teamRoleOwner           = platformstore.TeamRoleOwner
	teamRoleMember          = platformstore.TeamRoleMember
	namespaceScopeUser      = platformstore.NamespaceScopeUser
	namespaceScopeTeam      = platformstore.NamespaceScopeTeam
	accessApplyMaxBytes     = apihttp.ApplyMaxBytes
	deploymentApplyMaxBytes = 32 * 1024
	teamApplyMaxBytes       = 8 * 1024
)

// Principal is the authenticated platform identity contract shared by runtime handlers.
type Principal = apiauth.Principal
type principal = apiauth.Principal
type principalTeam = apiauth.PrincipalTeam
type platformStore = platformstore.Store
type auditEvent = platformstore.AuditEvent
type teamRecord = platformstore.Team
type teamMembershipRecord = platformstore.TeamMembership
type userAPIKeySummary = platformstore.APIKeySummary

type auditWriter interface {
	WriteAudit(context.Context, auditEvent)
}

func principalFromContext(ctx context.Context) (principal, bool) {
	return apiauth.FromContext(ctx)
}

func withPrincipal(ctx context.Context, p principal) context.Context {
	return apiauth.WithPrincipal(ctx, p)
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
	return apiauth.RequestIP(r)
}

func auditSource(r *http.Request, p principal) string {
	return apiauth.AuditSource(r, p)
}

func auditIdentityLabel(p principal) string {
	return apiauth.AuditIdentityLabel(p)
}

func envOr(key, fallback string) string {
	return serviceutil.EnvOr(key, fallback)
}

// NormalizeTeamSlug canonicalizes a team slug using the platform store rules shared with identity APIs.
func NormalizeTeamSlug(raw string) string {
	return platformstore.NormalizeTeamSlug(raw)
}
