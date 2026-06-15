package main

import (
	"context"
	"net/http"

	"mcp-platform-api/internal/apiauth"
	"mcp-platform-api/internal/platformstore"
	"mcp-runtime/pkg/apihttp"
)

const (
	roleAdmin = apiauth.RoleAdmin
	roleUser  = apiauth.RoleUser
)

type platformStore = platformstore.Store
type platformUser = platformstore.User
type principal = apiauth.Principal
type principalTeam = apiauth.PrincipalTeam
type auditEvent = platformstore.AuditEvent
type platformAuditLog = platformstore.AuditLog
type adminOperationsFilter = platformstore.OperationsFilter
type adminOperationsFilterResponse = platformstore.OperationsFilterResponse
type platformUserActivity = platformstore.UserActivity
type platformImageActivity = platformstore.ImageActivity
type teamRecord = platformstore.Team
type teamMembershipRecord = platformstore.TeamMembership
type userAPIKeySummary = platformstore.APIKeySummary
type userAPIKeyStore interface {
	AuthenticateUserAPIKey(ctx context.Context, rawKey string) (principal, bool, error)
	ListUserAPIKeys(ctx context.Context, userID string) ([]userAPIKeySummary, error)
	CreateUserAPIKey(ctx context.Context, userID, name string) (userAPIKeySummary, string, error)
	RevokeUserAPIKey(ctx context.Context, userID, id string) (userAPIKeySummary, error)
}

type auditWriter interface {
	WriteAudit(context.Context, auditEvent)
}

const (
	sharedCatalogNamespace = platformstore.SharedCatalogNamespace
	teamNamespacePrefix    = platformstore.TeamNamespacePrefix
	teamRoleOwner          = platformstore.TeamRoleOwner
	teamRoleMember         = platformstore.TeamRoleMember
	namespaceScopeUser     = platformstore.NamespaceScopeUser
	namespaceScopeTeam     = platformstore.NamespaceScopeTeam
	oidcProviderPrefix     = platformstore.OIDCProviderPrefix
	accessApplyMaxBytes    = apihttp.ApplyMaxBytes
)

func newPlatformStore(ctx context.Context, dsn string, jwtSecret []byte) (*platformStore, error) {
	return platformstore.Open(ctx, dsn, jwtSecret)
}

func newTestPlatformStore(jwtSecret []byte) *platformStore {
	return platformstore.NewForTest(jwtSecret)
}

func platformUserActivityWhere(filter adminOperationsFilter) (string, []any) {
	return platformstore.UserActivityWhere(filter)
}

func platformAuditTimeWhere(alias string, filter adminOperationsFilter, args *[]any) string {
	return platformstore.AuditTimeWhere(alias, filter, args)
}

func adminOperationsUserSearch(filter adminOperationsFilter) string {
	return platformstore.AdminOperationsUserSearch(filter)
}

func NormalizeTeamSlug(raw string) string {
	return platformstore.NormalizeTeamSlug(raw)
}

func ValidateTeamSlug(slug string) error {
	return platformstore.ValidateTeamSlug(slug)
}

func ValidateTeamNamespace(namespace string) error {
	return platformstore.ValidateTeamNamespace(namespace)
}

func principalFromContext(ctx context.Context) (principal, bool) {
	return apiauth.FromContext(ctx)
}

func withPrincipal(ctx context.Context, p principal) context.Context {
	return apiauth.WithPrincipal(ctx, p)
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

func requestSource(r *http.Request) string {
	return apiauth.RequestSource(r)
}

func auditIdentityLabel(p principal) string {
	return apiauth.AuditIdentityLabel(p)
}
