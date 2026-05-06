package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	sentinelaccess "mcp-runtime/pkg/access"
)

const (
	platformJWTIssuer   = "mcp-runtime"
	platformJWTAudience = "platform"
	passwordProvider    = "password"
	defaultDBMaxConns   = 10
	defaultDBMaxIdle    = 5
)

const oidcProviderPrefix = "oidc:"

const (
	sharedCatalogNamespace = "mcp-servers"
	teamNamespacePrefix    = "mcp-team-"
	teamRoleOwner          = "owner"
	teamRoleMember         = "member"
	namespaceScopeUser     = "user"
	namespaceScopeTeam     = "team"
)

type platformStore struct {
	db        *sql.DB
	jwtSecret []byte
}

type platformUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Namespace string `json:"namespace"`
}

type teamRecord struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"created_at"`
}

type teamMembershipRecord struct {
	TeamID        string    `json:"team_id"`
	TeamSlug      string    `json:"team_slug"`
	TeamName      string    `json:"team_name"`
	TeamNamespace string    `json:"team_namespace"`
	UserID        string    `json:"user_id"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
}

type auditEvent struct {
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

type auditWriter interface {
	WriteAudit(context.Context, auditEvent)
}

type platformAuditLog struct {
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

type adminOperationsFilter struct {
	User  string
	Since time.Time
	Until time.Time
	Limit int
}

type adminOperationsFilterResponse struct {
	User  string `json:"user,omitempty"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	Limit int    `json:"limit"`
}

type platformUserActivity struct {
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

type platformImageActivity struct {
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

func newPlatformStore(ctx context.Context, dsn string, jwtSecret []byte) (*platformStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(intEnvOrDefault("PLATFORM_DB_MAX_CONNS", defaultDBMaxConns))
	db.SetMaxIdleConns(intEnvOrDefault("PLATFORM_DB_MAX_IDLE_CONNS", defaultDBMaxIdle))
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &platformStore{db: db, jwtSecret: jwtSecret}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *platformStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, platformSchemaSQL)
	return err
}

func (s *platformStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func intEnvOrDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func (s *platformStore) CreatePasswordUser(ctx context.Context, email, password string, role string) (platformUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = roleUser
	}
	if role != roleUser && role != roleAdmin {
		return platformUser{}, errors.New("role must be user or admin")
	}
	if !validEmail(email) {
		return platformUser{}, errors.New("valid email required")
	}
	if len(password) < 8 {
		return platformUser{}, errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return platformUser{}, err
	}
	userID := uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return platformUser{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,role) VALUES ($1,$2,$3)`, userID, email, role); err != nil {
		return platformUser{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_identities (user_id,provider,subject,password_hash) VALUES ($1,$2,$3,$4)`, userID, passwordProvider, email, string(hash)); err != nil {
		return platformUser{}, err
	}
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT nextval('platform_namespace_seq')`).Scan(&seq); err != nil {
		return platformUser{}, err
	}
	namespace := fmt.Sprintf("user-%d", seq)
	if _, err = tx.ExecContext(ctx, `INSERT INTO namespaces (id,user_id,namespace) VALUES ($1,$2,$3)`, uuid.NewString(), userID, namespace); err != nil {
		return platformUser{}, err
	}
	if err = tx.Commit(); err != nil {
		return platformUser{}, err
	}
	return platformUser{ID: userID, Email: email, Role: role, Namespace: namespace}, nil
}

func (s *platformStore) EnsurePasswordUser(ctx context.Context, email, password string, role string) (platformUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = roleUser
	}
	if role != roleUser && role != roleAdmin {
		return platformUser{}, errors.New("role must be user or admin")
	}
	if !validEmail(email) {
		return platformUser{}, errors.New("valid email required")
	}
	if len(password) < 8 {
		return platformUser{}, errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return platformUser{}, err
	}

	var u platformUser
	err = s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(n.namespace, '')
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE u.email = $1 AND u.deleted_at IS NULL`, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return s.CreatePasswordUser(ctx, email, password, role)
	}
	if err != nil {
		return platformUser{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return platformUser{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, u.ID); err != nil {
		return platformUser{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO auth_identities (user_id, provider, subject, password_hash)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, subject)
DO UPDATE SET user_id = EXCLUDED.user_id, password_hash = EXCLUDED.password_hash`, u.ID, passwordProvider, email, string(hash)); err != nil {
		return platformUser{}, err
	}
	if u.Namespace == "" {
		var seq int64
		if err = tx.QueryRowContext(ctx, `SELECT nextval('platform_namespace_seq')`).Scan(&seq); err != nil {
			return platformUser{}, err
		}
		u.Namespace = fmt.Sprintf("user-%d", seq)
		if _, err = tx.ExecContext(ctx, `INSERT INTO namespaces (id,user_id,namespace) VALUES ($1,$2,$3)`, uuid.NewString(), u.ID, u.Namespace); err != nil {
			return platformUser{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return platformUser{}, err
	}
	u.Role = role
	return u, nil
}

func (s *platformStore) AuthenticatePassword(ctx context.Context, email, password string) (platformUser, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u platformUser
	var hash string
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(n.namespace, ''), ai.password_hash
FROM auth_identities ai
JOIN users u ON u.id = ai.user_id AND u.deleted_at IS NULL
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE ai.provider = $1 AND ai.subject = $2`, passwordProvider, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return platformUser{}, false, nil
	}
	if err != nil {
		return platformUser{}, false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return platformUser{}, false, nil
	}
	return u, true, nil
}

func (s *platformStore) EnsureOIDCUser(ctx context.Context, provider, subject, email, role string) (platformUser, error) {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = roleUser
	}
	if role != roleUser && role != roleAdmin {
		return platformUser{}, errors.New("role must be user or admin")
	}
	if provider == "" {
		return platformUser{}, errors.New("oidc provider required")
	}
	if subject == "" {
		return platformUser{}, errors.New("oidc subject required")
	}

	var u platformUser
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(n.namespace, '')
FROM auth_identities ai
JOIN users u ON u.id = ai.user_id AND u.deleted_at IS NULL
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE ai.provider = $1 AND ai.subject = $2`, provider, subject).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if err == nil {
		return s.ensureOIDCUserRoleAndNamespace(ctx, u, role)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return platformUser{}, err
	}
	if !validEmail(email) {
		return platformUser{}, errors.New("valid email required for oidc user")
	}

	userExists := true
	err = s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(n.namespace, '')
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE u.email = $1 AND u.deleted_at IS NULL`, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		u = platformUser{ID: uuid.NewString(), Email: email, Role: role}
		userExists = false
	} else if err != nil {
		return platformUser{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return platformUser{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if !userExists {
		if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,role) VALUES ($1,$2,$3)`, u.ID, u.Email, u.Role); err != nil {
			return platformUser{}, err
		}
	} else if role == roleAdmin && u.Role != roleAdmin {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, roleAdmin, u.ID); err != nil {
			return platformUser{}, err
		}
		u.Role = roleAdmin
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO auth_identities (user_id, provider, subject)
VALUES ($1, $2, $3)
ON CONFLICT (provider, subject)
DO UPDATE SET user_id = EXCLUDED.user_id`, u.ID, provider, subject); err != nil {
		return platformUser{}, err
	}
	if u.Namespace == "" {
		var seq int64
		if err = tx.QueryRowContext(ctx, `SELECT nextval('platform_namespace_seq')`).Scan(&seq); err != nil {
			return platformUser{}, err
		}
		u.Namespace = fmt.Sprintf("user-%d", seq)
		if _, err = tx.ExecContext(ctx, `INSERT INTO namespaces (id,user_id,namespace) VALUES ($1,$2,$3)`, uuid.NewString(), u.ID, u.Namespace); err != nil {
			return platformUser{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return platformUser{}, err
	}
	return u, nil
}

func (s *platformStore) ensureOIDCUserRoleAndNamespace(ctx context.Context, u platformUser, role string) (platformUser, error) {
	if role != roleAdmin && u.Namespace != "" {
		return u, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return platformUser{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if role == roleAdmin && u.Role != roleAdmin {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, roleAdmin, u.ID); err != nil {
			return platformUser{}, err
		}
		u.Role = roleAdmin
	}
	if u.Namespace == "" {
		var seq int64
		if err = tx.QueryRowContext(ctx, `SELECT nextval('platform_namespace_seq')`).Scan(&seq); err != nil {
			return platformUser{}, err
		}
		u.Namespace = fmt.Sprintf("user-%d", seq)
		if _, err = tx.ExecContext(ctx, `INSERT INTO namespaces (id,user_id,namespace) VALUES ($1,$2,$3)`, uuid.NewString(), u.ID, u.Namespace); err != nil {
			return platformUser{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return platformUser{}, err
	}
	return u, nil
}

func (s *platformStore) GetUser(ctx context.Context, userID string) (platformUser, bool, error) {
	var u platformUser
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(n.namespace, '')
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return platformUser{}, false, nil
	}
	if err != nil {
		return platformUser{}, false, err
	}
	return u, true, nil
}

func (s *platformStore) DeleteUser(ctx context.Context, userID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *platformStore) CreateAccessToken(u platformUser, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iss":       platformJWTIssuer,
		"aud":       platformJWTAudience,
		"sub":       u.ID,
		"email":     u.Email,
		"role":      u.Role,
		"namespace": u.Namespace,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *platformStore) AuthenticateJWT(token string) (principal, bool) {
	if s == nil || len(s.jwtSecret) == 0 {
		return principal{}, false
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", t.Method.Alg())
		}
		return s.jwtSecret, nil
	})
	if err != nil || !parsed.Valid {
		return principal{}, false
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != platformJWTIssuer {
		return principal{}, false
	}
	if !audienceMatches(claims["aud"], platformJWTAudience) {
		return principal{}, false
	}
	subject := strings.TrimSpace(fmt.Sprint(claims["sub"]))
	if subject == "" {
		return principal{}, false
	}
	p, err := s.principalForUserID(context.Background(), subject)
	if err != nil {
		return principal{}, false
	}
	p.AuthType = "platform_jwt"
	return p, true
}

func (s *platformStore) AuthenticateUserAPIKey(ctx context.Context, rawKey string) (principal, bool, error) {
	targetHash := hashAPIKey(rawKey)
	var keyID, userID string
	err := s.db.QueryRowContext(ctx, `
SELECT ak.id, ak.user_id
FROM api_keys ak
JOIN users u ON u.id = ak.user_id AND u.deleted_at IS NULL
WHERE ak.key_hash = $1 AND ak.revoked = false`, targetHash).
		Scan(&keyID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return principal{}, false, nil
	}
	if err != nil {
		return principal{}, false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, keyID)
	p, err := s.principalForUserID(ctx, userID)
	if err != nil {
		return principal{}, false, err
	}
	p.AuthType = "user_api_key"
	p.APIKeyID = keyID
	return p, true, nil
}

func (s *platformStore) principalForUserID(ctx context.Context, userID string) (principal, error) {
	var p principal
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, COALESCE(legacy.namespace, '')
FROM users u
LEFT JOIN LATERAL (
  SELECT n.namespace
  FROM namespaces n
  WHERE n.user_id = u.id
    AND n.deleted_at IS NULL
    AND COALESCE(n.scope, 'user') = 'user'
  ORDER BY n.created_at ASC
  LIMIT 1
) legacy ON true
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&p.Subject, &p.Email, &p.Role, &p.Namespace)
	if err != nil {
		return principal{}, err
	}
	teams, err := s.listTeamMembershipsForUser(ctx, userID)
	if err != nil {
		return principal{}, err
	}
	legacyNamespace := p.Namespace
	p.Teams = teams
	for _, team := range teams {
		if ns := strings.TrimSpace(team.Namespace); ns != "" {
			p.Namespace = ns
			break
		}
	}
	p.AllowedNamespaces = dedupeNamespaces(append(collectAllowedNamespaces(legacyNamespace, teams), sharedCatalogNamespace))
	if strings.TrimSpace(p.Namespace) == "" {
		p.Namespace = strings.TrimSpace(legacyNamespace)
	}
	return p, nil
}

func collectAllowedNamespaces(legacyNamespace string, teams []principalTeam) []string {
	namespaces := make([]string, 0, len(teams)+1)
	if ns := strings.TrimSpace(legacyNamespace); ns != "" {
		namespaces = append(namespaces, ns)
	}
	for _, team := range teams {
		if ns := strings.TrimSpace(team.Namespace); ns != "" {
			namespaces = append(namespaces, ns)
		}
	}
	return namespaces
}

func dedupeNamespaces(namespaces []string) []string {
	if len(namespaces) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(namespaces))
	out := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		out = append(out, namespace)
	}
	return out
}

func (s *platformStore) listTeamMembershipsForUser(ctx context.Context, userID string) ([]principalTeam, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.slug, t.display_name, COALESCE(n.namespace, ''), tm.role
FROM team_memberships tm
JOIN teams t ON t.id = tm.team_id AND t.deleted_at IS NULL
LEFT JOIN namespaces n ON n.team_id = t.id AND n.deleted_at IS NULL AND COALESCE(n.scope, 'team') = 'team'
WHERE tm.user_id = $1 AND tm.deleted_at IS NULL
ORDER BY t.slug ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := make([]principalTeam, 0)
	for rows.Next() {
		var team principalTeam
		if err := rows.Scan(&team.ID, &team.Slug, &team.Name, &team.Namespace, &team.Role); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (s *platformStore) ListUserAPIKeys(ctx context.Context, userID string) ([]userAPIKeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, revoked, revoked_at FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userAPIKeySummary
	for rows.Next() {
		var rec userAPIKeySummary
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *platformStore) CreateUserAPIKey(ctx context.Context, userID, name string) (userAPIKeySummary, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return userAPIKeySummary{}, "", errors.New("name required")
	}
	rawKey, err := generateAPIKeyValue()
	if err != nil {
		return userAPIKeySummary{}, "", err
	}
	rec := userAPIKeySummary{ID: "uk_" + uuid.NewString(), Name: name, Prefix: keyPrefix(rawKey), CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO api_keys (id,key_hash,user_id,name,prefix,created_at,revoked) VALUES ($1,$2,$3,$4,$5,$6,false)`,
		rec.ID, hashAPIKey(rawKey), userID, rec.Name, rec.Prefix, rec.CreatedAt)
	if err != nil {
		return userAPIKeySummary{}, "", err
	}
	return rec, rawKey, nil
}

func (s *platformStore) RevokeUserAPIKey(ctx context.Context, userID, id string) (userAPIKeySummary, error) {
	var rec userAPIKeySummary
	err := s.db.QueryRowContext(ctx, `
UPDATE api_keys
SET revoked = true, revoked_at = COALESCE(revoked_at, now())
WHERE user_id = $1 AND id = $2
RETURNING id, name, prefix, created_at, revoked, revoked_at`, userID, id).
		Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return userAPIKeySummary{}, sql.ErrNoRows
	}
	if err != nil {
		return userAPIKeySummary{}, err
	}
	return rec, nil
}

func (s *platformStore) ListRegistryCredentials(ctx context.Context, userID string) ([]userAPIKeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, revoked, revoked_at FROM registry_credentials WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userAPIKeySummary
	for rows.Next() {
		var rec userAPIKeySummary
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *platformStore) CreateRegistryCredential(ctx context.Context, userID, name string) (userAPIKeySummary, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return userAPIKeySummary{}, "", errors.New("name required")
	}
	rawKey, err := randomURLToken(32)
	if err != nil {
		return userAPIKeySummary{}, "", err
	}
	rawKey = "mcpr_" + rawKey
	rec := userAPIKeySummary{ID: "rk_" + uuid.NewString(), Name: name, Prefix: keyPrefix(rawKey), CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO registry_credentials (id,key_hash,user_id,name,prefix,created_at,revoked) VALUES ($1,$2,$3,$4,$5,$6,false)`,
		rec.ID, hashAPIKey(rawKey), userID, rec.Name, rec.Prefix, rec.CreatedAt)
	if err != nil {
		return userAPIKeySummary{}, "", err
	}
	return rec, rawKey, nil
}

func (s *platformStore) RevokeRegistryCredential(ctx context.Context, userID, id string) (userAPIKeySummary, error) {
	var rec userAPIKeySummary
	err := s.db.QueryRowContext(ctx, `
UPDATE registry_credentials
SET revoked = true, revoked_at = COALESCE(revoked_at, now())
WHERE user_id = $1 AND id = $2
RETURNING id, name, prefix, created_at, revoked, revoked_at`, userID, id).
		Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return userAPIKeySummary{}, sql.ErrNoRows
	}
	if err != nil {
		return userAPIKeySummary{}, err
	}
	return rec, nil
}

func (s *platformStore) ListNamespaces(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
  n.id,
  COALESCE(n.user_id::text, ''),
  COALESCE(u.email, ''),
  COALESCE(n.team_id::text, ''),
  COALESCE(t.slug, ''),
  COALESCE(t.display_name, ''),
  n.namespace,
  COALESCE(n.scope, 'user'),
  n.created_at
FROM namespaces n
LEFT JOIN users u ON u.id = n.user_id
LEFT JOIN teams t ON t.id = n.team_id
WHERE n.deleted_at IS NULL
ORDER BY n.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		item, err := scanNamespaceRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *platformStore) GetNamespace(ctx context.Context, namespace string) (map[string]any, bool, error) {
	namespace = strings.TrimSpace(namespace)
	var id, userID, email, teamID, teamSlug, teamName, scope string
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
SELECT
  n.id,
  COALESCE(n.user_id::text, ''),
  COALESCE(u.email, ''),
  COALESCE(n.team_id::text, ''),
  COALESCE(t.slug, ''),
  COALESCE(t.display_name, ''),
  COALESCE(n.scope, 'user'),
  n.created_at
FROM namespaces n
LEFT JOIN users u ON u.id = n.user_id
LEFT JOIN teams t ON t.id = n.team_id
WHERE n.deleted_at IS NULL AND n.namespace = $1
LIMIT 1`, namespace).Scan(&id, &userID, &email, &teamID, &teamSlug, &teamName, &scope, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return map[string]any{
		"id":         id,
		"user_id":    userID,
		"email":      email,
		"team_id":    teamID,
		"team_slug":  teamSlug,
		"team_name":  teamName,
		"scope":      scope,
		"namespace":  namespace,
		"created_at": createdAt,
		"is_shared":  namespace == sharedCatalogNamespace,
		"is_managed": strings.HasPrefix(namespace, teamNamespacePrefix),
	}, true, nil
}

func (s *platformStore) CreateTeam(ctx context.Context, slug, name, createdByUserID string) (teamRecord, error) {
	slug = normalizeTeamSlug(slug)
	if err := validateTeamSlug(slug); err != nil {
		return teamRecord{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = slug
	}
	namespace := teamNamespacePrefix + slug
	if err := validateTeamNamespace(namespace); err != nil {
		return teamRecord{}, err
	}
	teamID := uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return teamRecord{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
INSERT INTO teams (id, slug, display_name, created_by)
VALUES ($1, $2, $3, NULLIF($4, '')::uuid)`, teamID, slug, name, strings.TrimSpace(createdByUserID)); err != nil {
		return teamRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO namespaces (id, user_id, team_id, namespace, display_name, scope)
VALUES ($1, NULL, $2, $3, $4, $5)`, uuid.NewString(), teamID, namespace, name, namespaceScopeTeam); err != nil {
		return teamRecord{}, err
	}
	if strings.TrimSpace(createdByUserID) != "" {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO team_memberships (id, team_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, user_id) WHERE deleted_at IS NULL
DO UPDATE SET role = EXCLUDED.role, deleted_at = NULL`, uuid.NewString(), teamID, createdByUserID, teamRoleOwner); err != nil {
			return teamRecord{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return teamRecord{}, err
	}

	return teamRecord{
		ID:        teamID,
		Slug:      slug,
		Name:      name,
		Namespace: namespace,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *platformStore) ListTeams(ctx context.Context) ([]teamRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.slug, t.display_name, COALESCE(n.namespace, ''), t.created_at
FROM teams t
LEFT JOIN namespaces n ON n.team_id = t.id AND n.deleted_at IS NULL AND COALESCE(n.scope, 'team') = 'team'
WHERE t.deleted_at IS NULL
ORDER BY t.slug ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]teamRecord, 0)
	for rows.Next() {
		var team teamRecord
		if err := rows.Scan(&team.ID, &team.Slug, &team.Name, &team.Namespace, &team.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, team)
	}
	return out, rows.Err()
}

func (s *platformStore) ListUserTeams(ctx context.Context, userID string) ([]teamMembershipRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.slug, t.display_name, COALESCE(n.namespace, ''), tm.user_id::text, tm.role, tm.created_at
FROM team_memberships tm
JOIN teams t ON t.id = tm.team_id AND t.deleted_at IS NULL
LEFT JOIN namespaces n ON n.team_id = t.id AND n.deleted_at IS NULL AND COALESCE(n.scope, 'team') = 'team'
WHERE tm.user_id = $1 AND tm.deleted_at IS NULL
ORDER BY t.slug ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]teamMembershipRecord, 0)
	for rows.Next() {
		var membership teamMembershipRecord
		if err := rows.Scan(&membership.TeamID, &membership.TeamSlug, &membership.TeamName, &membership.TeamNamespace, &membership.UserID, &membership.Role, &membership.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, membership)
	}
	return out, rows.Err()
}

func (s *platformStore) GetTeamBySlug(ctx context.Context, slug string) (teamRecord, bool, error) {
	slug = normalizeTeamSlug(slug)
	var team teamRecord
	err := s.db.QueryRowContext(ctx, `
SELECT t.id, t.slug, t.display_name, COALESCE(n.namespace, ''), t.created_at
FROM teams t
LEFT JOIN namespaces n ON n.team_id = t.id AND n.deleted_at IS NULL AND COALESCE(n.scope, 'team') = 'team'
WHERE t.slug = $1 AND t.deleted_at IS NULL`, slug).
		Scan(&team.ID, &team.Slug, &team.Name, &team.Namespace, &team.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return teamRecord{}, false, nil
	}
	if err != nil {
		return teamRecord{}, false, err
	}
	return team, true, nil
}

func (s *platformStore) UpsertTeamMembership(ctx context.Context, teamSlug, userID, role string) (teamMembershipRecord, error) {
	teamSlug = normalizeTeamSlug(teamSlug)
	userID = strings.TrimSpace(userID)
	role = normalizeTeamMembershipRole(role)
	if userID == "" {
		return teamMembershipRecord{}, errors.New("userID is required")
	}
	if role == "" {
		return teamMembershipRecord{}, errors.New("membership role is required")
	}
	team, ok, err := s.GetTeamBySlug(ctx, teamSlug)
	if err != nil {
		return teamMembershipRecord{}, err
	}
	if !ok {
		return teamMembershipRecord{}, sql.ErrNoRows
	}
	if _, exists, err := s.GetUser(ctx, userID); err != nil {
		return teamMembershipRecord{}, err
	} else if !exists {
		return teamMembershipRecord{}, sql.ErrNoRows
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO team_memberships (id, team_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, user_id) WHERE deleted_at IS NULL
DO UPDATE SET role = EXCLUDED.role, deleted_at = NULL`, uuid.NewString(), team.ID, userID, role); err != nil {
		return teamMembershipRecord{}, err
	}
	return teamMembershipRecord{
		TeamID:        team.ID,
		TeamSlug:      team.Slug,
		TeamName:      team.Name,
		TeamNamespace: team.Namespace,
		UserID:        userID,
		Role:          role,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

func (s *platformStore) DeleteTeamMembership(ctx context.Context, teamSlug, userID string) error {
	team, ok, err := s.GetTeamBySlug(ctx, teamSlug)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	result, err := s.db.ExecContext(ctx, `UPDATE team_memberships SET deleted_at = now() WHERE team_id = $1 AND user_id = $2 AND deleted_at IS NULL`, team.ID, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *platformStore) ListUserActivity(ctx context.Context, filter adminOperationsFilter) ([]platformUserActivity, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := platformUserActivityWhere(filter)
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT u.id::text, u.email, u.role, COALESCE(n.namespace, ''), u.created_at,
       MAX(a.created_at) FILTER (WHERE a.action IN ('login', 'oidc_login') AND a.status = 'success') AS last_login_at,
       MAX(a.created_at) AS last_activity_at,
       COUNT(DISTINCT a.id) FILTER (WHERE a.action IN ('login', 'oidc_login') AND a.status = 'success') AS login_count,
       COUNT(DISTINCT a.id) FILTER (WHERE a.status IN ('denied', 'error')) AS failed_action_count,
       COUNT(DISTINCT rc.id) FILTER (WHERE rc.revoked = false) AS registry_credentials,
       COUNT(DISTINCT ak.id) FILTER (WHERE ak.revoked = false) AS api_keys
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
LEFT JOIN audit_logs a ON a.user_id = u.id
LEFT JOIN registry_credentials rc ON rc.user_id = u.id
LEFT JOIN api_keys ak ON ak.user_id = u.id
` + where + `
GROUP BY u.id, u.email, u.role, n.namespace, u.created_at
ORDER BY COALESCE(MAX(a.created_at), u.created_at) DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]platformUserActivity, 0, limit)
	for rows.Next() {
		var item platformUserActivity
		var lastLogin sql.NullTime
		var lastActivity sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Email,
			&item.Role,
			&item.Namespace,
			&item.CreatedAt,
			&lastLogin,
			&lastActivity,
			&item.LoginCount,
			&item.FailedActionCount,
			&item.RegistryCredentials,
			&item.APIKeys,
		); err != nil {
			return nil, err
		}
		item.LastLoginAt = nullTimePtr(lastLogin)
		item.LastActivityAt = nullTimePtr(lastActivity)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *platformStore) ListAuditLogs(ctx context.Context, filter adminOperationsFilter) ([]platformAuditLog, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := platformAuditWhere(filter)
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT a.id, COALESCE(a.user_id::text, ''), COALESCE(u.email, ''), a.action, a.resource,
       COALESCE(a.namespace, ''), a.status, COALESCE(a.message, ''),
       COALESCE(a.actor_ip, ''), COALESCE(a.request_id, ''),
       COALESCE(a.source, ''), COALESCE(a.auth_identity, ''),
       COALESCE(a.image_ref, ''), COALESCE(a.server_name, ''),
       COALESCE(a.deployment_target, ''), a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
` + where + `
ORDER BY a.created_at DESC, a.id DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]platformAuditLog, 0, limit)
	for rows.Next() {
		var item platformAuditLog
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.Email,
			&item.Action,
			&item.Resource,
			&item.Namespace,
			&item.Status,
			&item.Message,
			&item.ActorIP,
			&item.RequestID,
			&item.Source,
			&item.AuthIdentity,
			&item.ImageRef,
			&item.ServerName,
			&item.DeploymentTarget,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *platformStore) ListImageActivity(ctx context.Context, filter adminOperationsFilter) ([]platformImageActivity, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := platformAuditWhere(filter)
	if where == "" {
		where = "WHERE a.image_ref IS NOT NULL AND a.image_ref <> ''"
	} else {
		where += " AND a.image_ref IS NOT NULL AND a.image_ref <> ''"
	}
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT COALESCE(a.user_id::text, ''), COALESCE(u.email, ''), COALESCE(a.namespace, ''),
       a.image_ref, COALESCE(a.resource, ''), COALESCE(a.server_name, ''),
       COALESCE(a.deployment_target, ''), a.action, a.status,
       COALESCE(a.source, ''), a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
` + where + `
ORDER BY a.created_at DESC, a.id DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]platformImageActivity, 0, limit)
	for rows.Next() {
		var item platformImageActivity
		if err := rows.Scan(
			&item.UserID,
			&item.Email,
			&item.Namespace,
			&item.ImageRef,
			&item.SourceImage,
			&item.ServerName,
			&item.DeploymentTarget,
			&item.Action,
			&item.Status,
			&item.Source,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *platformStore) WriteAudit(ctx context.Context, ev auditEvent) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs (user_id,action,resource,namespace,status,message,actor_ip,request_id,source,auth_identity,image_ref,server_name,deployment_target) VALUES (NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		ev.UserID, ev.Action, ev.Resource, ev.Namespace, ev.Status, ev.Message, ev.ActorIP, ev.RequestID, ev.Source, ev.AuthIdentity, ev.ImageRef, ev.ServerName, ev.DeploymentTarget)
}

func platformUserActivityWhere(filter adminOperationsFilter) (string, []any) {
	conditions := []string{"u.deleted_at IS NULL"}
	args := make([]any, 0)
	if user := strings.TrimSpace(filter.User); user != "" {
		pattern := "%" + user + "%"
		args = append(args, user, pattern, pattern)
		conditions = append(conditions, fmt.Sprintf("(u.id::text = $%d OR u.email ILIKE $%d OR COALESCE(n.namespace, '') ILIKE $%d)", len(args)-2, len(args)-1, len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		conditions = append(conditions, fmt.Sprintf("(a.id IS NULL OR a.created_at >= $%d)", len(args)))
	}
	if !filter.Until.IsZero() {
		args = append(args, filter.Until)
		conditions = append(conditions, fmt.Sprintf("(a.id IS NULL OR a.created_at <= $%d)", len(args)))
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func platformAuditWhere(filter adminOperationsFilter) (string, []any) {
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if user := strings.TrimSpace(filter.User); user != "" {
		pattern := "%" + user + "%"
		args = append(args, user, pattern, pattern, pattern, pattern, pattern, pattern)
		conditions = append(conditions, fmt.Sprintf("(a.user_id::text = $%d OR COALESCE(u.email, '') ILIKE $%d OR COALESCE(a.namespace, '') ILIKE $%d OR COALESCE(a.resource, '') ILIKE $%d OR COALESCE(a.image_ref, '') ILIKE $%d OR COALESCE(a.server_name, '') ILIKE $%d OR COALESCE(a.deployment_target, '') ILIKE $%d)", len(args)-6, len(args)-5, len(args)-4, len(args)-3, len(args)-2, len(args)-1, len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		conditions = append(conditions, fmt.Sprintf("a.created_at >= $%d", len(args)))
	}
	if !filter.Until.IsZero() {
		args = append(args, filter.Until)
		conditions = append(conditions, fmt.Sprintf("a.created_at <= $%d", len(args)))
	}
	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func normalizeTeamSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func scanNamespaceRow(rows *sql.Rows) (map[string]any, error) {
	var id, userID, email, teamID, teamSlug, teamName, namespace, scope string
	var createdAt time.Time
	if err := rows.Scan(&id, &userID, &email, &teamID, &teamSlug, &teamName, &namespace, &scope, &createdAt); err != nil {
		return nil, err
	}
	return map[string]any{
		"id":         id,
		"user_id":    userID,
		"email":      email,
		"team_id":    teamID,
		"team_slug":  teamSlug,
		"team_name":  teamName,
		"scope":      scope,
		"namespace":  namespace,
		"created_at": createdAt,
		"is_shared":  namespace == sharedCatalogNamespace,
		"is_managed": strings.HasPrefix(namespace, teamNamespacePrefix),
	}, nil
}

func normalizeTeamMembershipRole(raw string) string {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case teamRoleOwner, teamRoleMember:
		return role
	default:
		return ""
	}
}

func validateTeamSlug(slug string) error {
	if slug == "" {
		return errors.New("team slug is required")
	}
	if err := sentinelaccess.ValidateResourceName("team", slug); err != nil {
		return err
	}
	return nil
}

func validateTeamNamespace(namespace string) error {
	if namespace == "" {
		return errors.New("namespace required")
	}
	if strings.TrimSpace(namespace) == sharedCatalogNamespace {
		return errors.New("shared catalog namespace is reserved")
	}
	reserved := []string{"default", "kube-system", "kube-public", "kube-node-lease", "mcp-runtime", "mcp-sentinel", "registry", "traefik"}
	for _, disallowed := range reserved {
		if namespace == disallowed {
			return fmt.Errorf("namespace %q is reserved", namespace)
		}
	}
	if err := sentinelaccess.ValidateResourceName("namespace", namespace); err != nil {
		return err
	}
	return nil
}

func validEmail(email string) bool {
	if len(email) > 254 || !strings.Contains(email, "@") {
		return false
	}
	host := email[strings.LastIndex(email, "@")+1:]
	return host != "" && net.ParseIP(host) == nil
}

const platformSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE SEQUENCE IF NOT EXISTS platform_namespace_seq;
CREATE TABLE IF NOT EXISTS users (
  id uuid primary key,
  email text unique not null,
  role text not null check (role in ('user','admin')),
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
CREATE TABLE IF NOT EXISTS auth_identities (
  user_id uuid references users(id) on delete cascade,
  provider text not null,
  subject text not null,
  password_hash text,
  created_at timestamptz not null default now(),
  primary key (provider, subject)
);
CREATE TABLE IF NOT EXISTS api_keys (
  id text primary key,
  key_hash text unique not null,
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  prefix text not null,
  created_at timestamptz not null default now(),
  last_used_at timestamptz,
  revoked boolean not null default false,
  revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE TABLE IF NOT EXISTS registry_credentials (
  id text primary key,
  key_hash text unique not null,
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  prefix text not null,
  created_at timestamptz not null default now(),
  last_used_at timestamptz,
  revoked boolean not null default false,
  revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_registry_credentials_user_id ON registry_credentials(user_id);
CREATE TABLE IF NOT EXISTS namespaces (
  id uuid primary key,
  user_id uuid references users(id) on delete cascade,
  team_id uuid,
  namespace text not null,
  display_name text,
  scope text not null default 'user',
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
ALTER TABLE namespaces ADD COLUMN IF NOT EXISTS team_id uuid;
ALTER TABLE namespaces ADD COLUMN IF NOT EXISTS display_name text;
ALTER TABLE namespaces ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'user';
ALTER TABLE namespaces ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE namespaces
  DROP CONSTRAINT IF EXISTS namespaces_scope_check;
ALTER TABLE namespaces
  ADD CONSTRAINT namespaces_scope_check CHECK (scope IN ('user', 'team'));
CREATE INDEX IF NOT EXISTS idx_namespaces_user_id ON namespaces(user_id);
CREATE INDEX IF NOT EXISTS idx_namespaces_team_id ON namespaces(team_id);
ALTER TABLE IF EXISTS namespaces
  DROP CONSTRAINT IF EXISTS namespaces_namespace_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_namespaces_active ON namespaces(namespace) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS teams (
  id uuid primary key,
  slug text unique not null,
  display_name text not null,
  created_by uuid references users(id),
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_teams_slug_active ON teams(slug) WHERE deleted_at IS NULL;
ALTER TABLE namespaces
  DROP CONSTRAINT IF EXISTS namespaces_team_id_fkey;
ALTER TABLE namespaces
  ADD CONSTRAINT namespaces_team_id_fkey FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE;
CREATE TABLE IF NOT EXISTS team_memberships (
  id uuid primary key,
  team_id uuid not null references teams(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  role text not null check (role in ('owner', 'member')),
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_team_memberships_active ON team_memberships(team_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_team_memberships_user_id ON team_memberships(user_id);
CREATE TABLE IF NOT EXISTS refresh_tokens (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  token_hash text unique not null,
  expires_at timestamptz not null,
  revoked boolean not null default false,
  user_agent text,
  client_ip inet
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE TABLE IF NOT EXISTS audit_logs (
  id bigserial primary key,
  user_id uuid references users(id),
  action text not null,
  resource text not null,
  namespace text,
  status text not null,
  message text,
  actor_ip text,
  request_id text,
  source text,
  auth_identity text,
  image_ref text,
  server_name text,
  deployment_target text,
  created_at timestamptz not null default now()
);
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'audit_logs'
      AND column_name = 'timestamp'
  ) AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'audit_logs'
      AND column_name = 'created_at'
  ) THEN
    EXECUTE 'ALTER TABLE audit_logs RENAME COLUMN "timestamp" TO created_at';
  END IF;
END
$$;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source text;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS auth_identity text;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS image_ref text;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS server_name text;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS deployment_target text;
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_image_ref ON audit_logs(image_ref);
`
