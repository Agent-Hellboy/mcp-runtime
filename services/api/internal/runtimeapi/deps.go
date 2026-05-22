package runtimeapi

import (
	"context"
	"net/http"

	"mcp-runtime/pkg/serviceutil"
	"mcp-sentinel-api/internal/apiauth"
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
	apihttp.WriteBodyDecodeError(w, err)
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
