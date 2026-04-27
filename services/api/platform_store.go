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
)

const (
	platformJWTIssuer   = "mcp-runtime"
	platformJWTAudience = "platform"
	passwordProvider    = "password"
	defaultDBMaxConns   = 10
	defaultDBMaxIdle    = 5
)

const oidcProviderPrefix = "oidc:"

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

type auditEvent struct {
	UserID    string
	Action    string
	Resource  string
	Namespace string
	Status    string
	Message   string
	ActorIP   string
	RequestID string
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

	var p principal
	err = s.db.QueryRow(`
SELECT u.id, u.email, u.role, COALESCE(n.namespace, '')
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE u.id = $1 AND u.deleted_at IS NULL`, subject).
		Scan(&p.Subject, &p.Email, &p.Role, &p.Namespace)
	if err != nil {
		return principal{}, false
	}
	p.AuthType = "platform_jwt"
	return p, true
}

func (s *platformStore) AuthenticateUserAPIKey(ctx context.Context, rawKey string) (principal, bool, error) {
	targetHash := hashAPIKey(rawKey)
	var keyID, userID, email, role, namespace string
	err := s.db.QueryRowContext(ctx, `
SELECT ak.id, ak.user_id, u.email, u.role, COALESCE(n.namespace, '')
FROM api_keys ak
JOIN users u ON u.id = ak.user_id AND u.deleted_at IS NULL
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
WHERE ak.key_hash = $1 AND ak.revoked = false`, targetHash).
		Scan(&keyID, &userID, &email, &role, &namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return principal{}, false, nil
	}
	if err != nil {
		return principal{}, false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, keyID)
	return principal{Role: role, Subject: userID, Email: email, Namespace: namespace, AuthType: "user_api_key", APIKeyID: keyID}, true, nil
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
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.user_id, u.email, n.namespace, n.created_at FROM namespaces n JOIN users u ON u.id = n.user_id WHERE n.deleted_at IS NULL ORDER BY n.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, userID, email, namespace string
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &email, &namespace, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "user_id": userID, "email": email, "namespace": namespace, "created_at": createdAt})
	}
	return out, rows.Err()
}

func (s *platformStore) WriteAudit(ctx context.Context, ev auditEvent) {
	if s == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs (user_id,action,resource,namespace,status,message,actor_ip,request_id) VALUES (NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,$7,$8)`,
		ev.UserID, ev.Action, ev.Resource, ev.Namespace, ev.Status, ev.Message, ev.ActorIP, ev.RequestID)
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
  user_id uuid not null references users(id) on delete cascade,
  namespace text not null,
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_namespaces_user_id ON namespaces(user_id);
ALTER TABLE IF EXISTS namespaces
  DROP CONSTRAINT IF EXISTS namespaces_namespace_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_namespaces_active ON namespaces(namespace) WHERE deleted_at IS NULL;
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
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
`
