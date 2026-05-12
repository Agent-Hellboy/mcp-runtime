package platformstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func keyPrefix(raw string) string {
	if len(raw) <= 8 {
		return raw
	}
	return raw[:8]
}

func generateAPIKeyValue() (string, error) {
	token, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	return "mcpu_" + token, nil
}

func randomURLToken(rawBytes int) (string, error) {
	b := make([]byte, rawBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Store) ListUserAPIKeys(ctx context.Context, userID string) ([]APIKeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, revoked, revoked_at FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeySummary
	for rows.Next() {
		var rec APIKeySummary
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) CreateUserAPIKey(ctx context.Context, userID, name string) (APIKeySummary, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKeySummary{}, "", errors.New("name required")
	}
	rawKey, err := generateAPIKeyValue()
	if err != nil {
		return APIKeySummary{}, "", err
	}
	rec := APIKeySummary{ID: "uk_" + uuid.NewString(), Name: name, Prefix: keyPrefix(rawKey), CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO api_keys (id,key_hash,user_id,name,prefix,created_at,revoked) VALUES ($1,$2,$3,$4,$5,$6,false)`,
		rec.ID, hashAPIKey(rawKey), userID, rec.Name, rec.Prefix, rec.CreatedAt)
	if err != nil {
		return APIKeySummary{}, "", err
	}
	return rec, rawKey, nil
}

func (s *Store) RevokeUserAPIKey(ctx context.Context, userID, id string) (APIKeySummary, error) {
	var rec APIKeySummary
	err := s.db.QueryRowContext(ctx, `
UPDATE api_keys
SET revoked = true, revoked_at = COALESCE(revoked_at, now())
WHERE user_id = $1 AND id = $2
RETURNING id, name, prefix, created_at, revoked, revoked_at`, userID, id).
		Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKeySummary{}, sql.ErrNoRows
	}
	if err != nil {
		return APIKeySummary{}, err
	}
	return rec, nil
}

func (s *Store) ListRegistryCredentials(ctx context.Context, userID string) ([]APIKeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, revoked, revoked_at FROM registry_credentials WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeySummary
	for rows.Next() {
		var rec APIKeySummary
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) CreateRegistryCredential(ctx context.Context, userID, name string) (APIKeySummary, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKeySummary{}, "", errors.New("name required")
	}
	rawKey, err := randomURLToken(32)
	if err != nil {
		return APIKeySummary{}, "", err
	}
	rawKey = "mcpr_" + rawKey
	rec := APIKeySummary{ID: "rk_" + uuid.NewString(), Name: name, Prefix: keyPrefix(rawKey), CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO registry_credentials (id,key_hash,user_id,name,prefix,created_at,revoked) VALUES ($1,$2,$3,$4,$5,$6,false)`,
		rec.ID, hashAPIKey(rawKey), userID, rec.Name, rec.Prefix, rec.CreatedAt)
	if err != nil {
		return APIKeySummary{}, "", err
	}
	return rec, rawKey, nil
}

func (s *Store) RevokeRegistryCredential(ctx context.Context, userID, id string) (APIKeySummary, error) {
	var rec APIKeySummary
	err := s.db.QueryRowContext(ctx, `
UPDATE registry_credentials
SET revoked = true, revoked_at = COALESCE(revoked_at, now())
WHERE user_id = $1 AND id = $2
RETURNING id, name, prefix, created_at, revoked, revoked_at`, userID, id).
		Scan(&rec.ID, &rec.Name, &rec.Prefix, &rec.CreatedAt, &rec.Revoked, &rec.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKeySummary{}, sql.ErrNoRows
	}
	if err != nil {
		return APIKeySummary{}, err
	}
	return rec, nil
}
