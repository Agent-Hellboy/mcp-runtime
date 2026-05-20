package platformstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func audienceMatches(audClaim any, expected string) bool {
	switch aud := audClaim.(type) {
	case string:
		return aud == expected
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	case []string:
		for _, item := range aud {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func (s *Store) CreatePasswordUser(ctx context.Context, email, password string, role string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = RoleUser
	}
	if role != RoleUser && role != RoleAdmin {
		return User{}, errors.New("role must be user or admin")
	}
	if !validEmail(email) {
		return User{}, errors.New("valid email required")
	}
	if len(password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return User{}, err
	}
	userID := uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,role) VALUES ($1,$2,$3)`, userID, email, role); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_identities (user_id,provider,subject,password_hash) VALUES ($1,$2,$3,$4)`, userID, passwordProvider, email, string(hash)); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: userID, Email: email, Role: role}, nil
}

func (s *Store) EnsurePasswordUser(ctx context.Context, email, password string, role string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = RoleUser
	}
	if role != RoleUser && role != RoleAdmin {
		return User{}, errors.New("role must be user or admin")
	}
	if !validEmail(email) {
		return User{}, errors.New("valid email required")
	}
	if len(password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return User{}, err
	}

	var u User
	err = s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, ''
FROM users u
WHERE u.email = $1 AND u.deleted_at IS NULL`, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return s.CreatePasswordUser(ctx, email, password, role)
	}
	if err != nil {
		return User{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, u.ID); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO auth_identities (user_id, provider, subject, password_hash)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, subject)
DO UPDATE SET user_id = EXCLUDED.user_id, password_hash = EXCLUDED.password_hash`, u.ID, passwordProvider, email, string(hash)); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	u.Role = role
	return u, nil
}

// EnsureTeamPasswordUser ensures a regular password-login account exists for
// team membership flows. Existing platform roles are preserved so an admin is
// not accidentally demoted when they are added to a team.
func (s *Store) EnsureTeamPasswordUser(ctx context.Context, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	if !validEmail(email) {
		return User{}, errors.New("valid email required")
	}
	if len(password) < 12 {
		return User{}, errors.New("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return User{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var u User
	err = tx.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, ''
FROM users u
WHERE u.email = $1 AND u.deleted_at IS NULL`, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		u = User{ID: uuid.NewString(), Email: email, Role: RoleUser}
		if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,role) VALUES ($1,$2,$3)`, u.ID, u.Email, u.Role); err != nil {
			return User{}, err
		}
	} else if err != nil {
		return User{}, err
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO auth_identities (user_id, provider, subject, password_hash)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, subject)
DO UPDATE SET user_id = EXCLUDED.user_id, password_hash = EXCLUDED.password_hash`, u.ID, passwordProvider, email, string(hash)); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) AuthenticatePassword(ctx context.Context, email, password string) (User, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, '', ai.password_hash
FROM auth_identities ai
JOIN users u ON u.id = ai.user_id AND u.deleted_at IS NULL
WHERE ai.provider = $1 AND ai.subject = $2`, passwordProvider, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, false, nil
	}
	return u, true, nil
}

func (s *Store) EnsureOIDCUser(ctx context.Context, provider, subject, email, role string) (User, error) {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if role == "" {
		role = RoleUser
	}
	if role != RoleUser && role != RoleAdmin {
		return User{}, errors.New("role must be user or admin")
	}
	if provider == "" {
		return User{}, errors.New("oidc provider required")
	}
	if subject == "" {
		return User{}, errors.New("oidc subject required")
	}

	var u User
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, ''
FROM auth_identities ai
JOIN users u ON u.id = ai.user_id AND u.deleted_at IS NULL
WHERE ai.provider = $1 AND ai.subject = $2`, provider, subject).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if err == nil {
		return s.ensureOIDCUserRole(ctx, u, role)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	if !validEmail(email) {
		return User{}, errors.New("valid email required for oidc user")
	}

	userExists := true
	err = s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, ''
FROM users u
WHERE u.email = $1 AND u.deleted_at IS NULL`, email).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		u = User{ID: uuid.NewString(), Email: email, Role: role}
		userExists = false
	} else if err != nil {
		return User{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if !userExists {
		if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,email,role) VALUES ($1,$2,$3)`, u.ID, u.Email, u.Role); err != nil {
			return User{}, err
		}
	} else if role == RoleAdmin && u.Role != RoleAdmin {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, RoleAdmin, u.ID); err != nil {
			return User{}, err
		}
		u.Role = RoleAdmin
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO auth_identities (user_id, provider, subject)
VALUES ($1, $2, $3)
ON CONFLICT (provider, subject)
DO UPDATE SET user_id = EXCLUDED.user_id`, u.ID, provider, subject); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) ensureOIDCUserRole(ctx context.Context, u User, role string) (User, error) {
	if role != RoleAdmin {
		return u, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if role == RoleAdmin && u.Role != RoleAdmin {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, RoleAdmin, u.ID); err != nil {
			return User{}, err
		}
		u.Role = RoleAdmin
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, userID string) (User, bool, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role, ''
FROM users u
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&u.ID, &u.Email, &u.Role, &u.Namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return u, true, nil
}

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
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

func (s *Store) CreateAccessToken(u User, ttl time.Duration) (string, error) {
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

func (s *Store) AuthenticateJWT(token string) (Principal, bool) {
	if s == nil || len(s.jwtSecret) == 0 {
		return Principal{}, false
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", t.Method.Alg())
		}
		return s.jwtSecret, nil
	})
	if err != nil || !parsed.Valid {
		return Principal{}, false
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != platformJWTIssuer {
		return Principal{}, false
	}
	if !audienceMatches(claims["aud"], platformJWTAudience) {
		return Principal{}, false
	}
	subject := strings.TrimSpace(fmt.Sprint(claims["sub"]))
	if subject == "" {
		return Principal{}, false
	}
	p, err := s.PrincipalForUserID(context.Background(), subject)
	if err != nil {
		return Principal{}, false
	}
	p.AuthType = "platform_jwt"
	return p, true
}

func (s *Store) AuthenticateUserAPIKey(ctx context.Context, rawKey string) (Principal, bool, error) {
	targetHash := hashAPIKey(rawKey)
	var keyID, userID string
	err := s.db.QueryRowContext(ctx, `
SELECT ak.id, ak.user_id
FROM api_keys ak
JOIN users u ON u.id = ak.user_id AND u.deleted_at IS NULL
WHERE ak.key_hash = $1 AND ak.revoked = false`, targetHash).
		Scan(&keyID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, false, nil
	}
	if err != nil {
		return Principal{}, false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, keyID)
	p, err := s.PrincipalForUserID(ctx, userID)
	if err != nil {
		return Principal{}, false, err
	}
	p.AuthType = "user_api_key"
	p.APIKeyID = keyID
	return p, true, nil
}

func (s *Store) PrincipalForUserID(ctx context.Context, userID string) (Principal, error) {
	var p Principal
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.email, u.role
FROM users u
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&p.Subject, &p.Email, &p.Role)
	if err != nil {
		return Principal{}, err
	}
	teams, err := s.listTeamMembershipsForUser(ctx, userID)
	if err != nil {
		return Principal{}, err
	}
	p.Teams = teams
	for _, team := range teams {
		if ns := strings.TrimSpace(team.Namespace); ns != "" {
			p.Namespace = ns
			break
		}
	}
	p.AllowedNamespaces = dedupeNamespaces(append(collectAllowedNamespaces(teams), SharedCatalogNamespace))
	return p, nil
}

func collectAllowedNamespaces(teams []PrincipalTeam) []string {
	namespaces := make([]string, 0, len(teams))
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

func (s *Store) listTeamMembershipsForUser(ctx context.Context, userID string) ([]PrincipalTeam, error) {
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

	teams := make([]PrincipalTeam, 0)
	for rows.Next() {
		var team PrincipalTeam
		if err := rows.Scan(&team.ID, &team.Slug, &team.Name, &team.Namespace, &team.Role); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}
func validEmail(email string) bool {
	if len(email) > 254 || !strings.Contains(email, "@") {
		return false
	}
	host := email[strings.LastIndex(email, "@")+1:]
	return host != "" && net.ParseIP(host) == nil
}
