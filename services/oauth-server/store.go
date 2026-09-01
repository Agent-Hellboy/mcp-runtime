package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

const oauthSchemaSQL = `
CREATE TABLE IF NOT EXISTS oauth_clients (
  client_id text primary key,
  client_secret_hash text,
  client_name text not null,
  redirect_uris jsonb not null,
  grant_types jsonb not null,
  response_types jsonb not null,
  token_endpoint_auth_method text not null,
  created_at timestamptz not null default now()
);
CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
  code_hash text primary key,
  client_id text not null,
  redirect_uri text not null,
  user_id uuid not null references users(id) on delete cascade,
  code_challenge text not null,
  code_challenge_method text not null,
  resource text not null,
  scope text not null default '',
  expires_at timestamptz not null,
  used_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expiry ON oauth_authorization_codes(expires_at);
CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
  token_hash text primary key,
  client_id text not null,
  user_id uuid not null references users(id) on delete cascade,
  resource text not null,
  scope text not null default '',
  expires_at timestamptz not null,
  revoked boolean not null default false,
  created_at timestamptz not null default now()
);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_user ON oauth_refresh_tokens(user_id);
`

type oauthClient struct {
	ID                    string
	SecretHash            string
	Name                  string
	RedirectURIs          []string
	GrantTypes            []string
	ResponseTypes         []string
	TokenEndpointAuthMode string
}

type authorizationCode struct {
	Hash              string
	ClientID          string
	RedirectURI       string
	UserID            string
	CodeChallenge     string
	CodeChallengeMode string
	Resource          string
	Scope             string
	ExpiresAt         time.Time
}

type refreshToken struct {
	Hash      string
	ClientID  string
	UserID    string
	Resource  string
	Scope     string
	ExpiresAt time.Time
	Revoked   bool
}

type oauthUser struct {
	ID    string
	Email string
}

type oauthStore struct {
	db *sql.DB

	mu       sync.Mutex
	clients  map[string]oauthClient
	codes    map[string]authorizationCode
	refresh  map[string]refreshToken
	users    map[string]oauthUser
	password map[string]string
}

func openOAuthStore(ctx context.Context, dsn string) (*oauthStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, oauthSchemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &oauthStore{db: db}, nil
}

func newMemoryOAuthStore(users ...oauthUser) *oauthStore {
	s := &oauthStore{
		clients:  map[string]oauthClient{},
		codes:    map[string]authorizationCode{},
		refresh:  map[string]refreshToken{},
		users:    map[string]oauthUser{},
		password: map[string]string{},
	}
	for _, user := range users {
		s.users[user.Email] = user
	}
	return s
}

func (s *oauthStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func hashOpaque(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *oauthStore) saveClient(ctx context.Context, client oauthClient) error {
	if s.db == nil {
		s.mu.Lock()
		s.clients[client.ID] = client
		s.mu.Unlock()
		return nil
	}
	redirects, _ := json.Marshal(client.RedirectURIs)
	grants, _ := json.Marshal(client.GrantTypes)
	responses, _ := json.Marshal(client.ResponseTypes)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_clients (client_id,client_secret_hash,client_name,redirect_uris,grant_types,response_types,token_endpoint_auth_method)
VALUES ($1,NULLIF($2,''),$3,$4,$5,$6,$7)`, client.ID, client.SecretHash, client.Name, redirects, grants, responses, client.TokenEndpointAuthMode)
	return err
}

func (s *oauthStore) client(ctx context.Context, clientID string) (oauthClient, bool, error) {
	if s.db == nil {
		s.mu.Lock()
		client, ok := s.clients[clientID]
		s.mu.Unlock()
		return client, ok, nil
	}
	var client oauthClient
	var redirects, grants, responses []byte
	err := s.db.QueryRowContext(ctx, `SELECT client_id,COALESCE(client_secret_hash,''),client_name,redirect_uris,grant_types,response_types,token_endpoint_auth_method FROM oauth_clients WHERE client_id=$1`, clientID).
		Scan(&client.ID, &client.SecretHash, &client.Name, &redirects, &grants, &responses, &client.TokenEndpointAuthMode)
	if errors.Is(err, sql.ErrNoRows) {
		return oauthClient{}, false, nil
	}
	if err != nil {
		return oauthClient{}, false, err
	}
	if err := json.Unmarshal(redirects, &client.RedirectURIs); err != nil {
		return oauthClient{}, false, err
	}
	if err := json.Unmarshal(grants, &client.GrantTypes); err != nil {
		return oauthClient{}, false, err
	}
	if err := json.Unmarshal(responses, &client.ResponseTypes); err != nil {
		return oauthClient{}, false, err
	}
	return client, true, nil
}

func (s *oauthStore) authenticatePassword(ctx context.Context, email, password string) (oauthUser, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.db == nil {
		s.mu.Lock()
		user, ok := s.users[email]
		hash := s.password[email]
		s.mu.Unlock()
		if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
			return oauthUser{}, false, nil
		}
		return user, true, nil
	}
	var user oauthUser
	var hash string
	err := s.db.QueryRowContext(ctx, `
SELECT u.id,u.email,ai.password_hash
FROM users u JOIN auth_identities ai ON ai.user_id=u.id
WHERE u.email=$1 AND u.deleted_at IS NULL AND ai.provider='password'`, email).
		Scan(&user.ID, &user.Email, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return oauthUser{}, false, nil
	}
	if err != nil {
		return oauthUser{}, false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return oauthUser{}, false, nil
	}
	return user, true, nil
}

func (s *oauthStore) saveCode(ctx context.Context, code authorizationCode) error {
	if s.db == nil {
		s.mu.Lock()
		s.codes[code.Hash] = code
		s.mu.Unlock()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_authorization_codes (code_hash,client_id,redirect_uri,user_id,code_challenge,code_challenge_method,resource,scope,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, code.Hash, code.ClientID, code.RedirectURI, code.UserID, code.CodeChallenge, code.CodeChallengeMode, code.Resource, code.Scope, code.ExpiresAt)
	return err
}

func (s *oauthStore) consumeCode(ctx context.Context, hash, clientID, redirectURI, verifier string) (authorizationCode, bool, error) {
	if s.db == nil {
		s.mu.Lock()
		code, ok := s.codes[hash]
		if ok && (code.ClientID != clientID || code.RedirectURI != redirectURI || code.ExpiresAt.Before(time.Now()) || !verifyPKCE(verifier, code.CodeChallenge)) {
			ok = false
		}
		if ok {
			delete(s.codes, hash)
		}
		s.mu.Unlock()
		return code, ok, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return authorizationCode{}, false, err
	}
	defer tx.Rollback()
	var code authorizationCode
	err = tx.QueryRowContext(ctx, `SELECT code_hash,client_id,redirect_uri,user_id,code_challenge,code_challenge_method,resource,scope,expires_at FROM oauth_authorization_codes WHERE code_hash=$1 AND client_id=$2 AND redirect_uri=$3 AND used_at IS NULL AND expires_at > now() FOR UPDATE`, hash, clientID, redirectURI).
		Scan(&code.Hash, &code.ClientID, &code.RedirectURI, &code.UserID, &code.CodeChallenge, &code.CodeChallengeMode, &code.Resource, &code.Scope, &code.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return authorizationCode{}, false, nil
	}
	if err != nil {
		return authorizationCode{}, false, err
	}
	if !verifyPKCE(verifier, code.CodeChallenge) {
		return authorizationCode{}, false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_authorization_codes SET used_at=now() WHERE code_hash=$1`, hash); err != nil {
		return authorizationCode{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return authorizationCode{}, false, err
	}
	return code, true, nil
}

func (s *oauthStore) saveRefresh(ctx context.Context, token refreshToken) error {
	if s.db == nil {
		s.mu.Lock()
		s.refresh[token.Hash] = token
		s.mu.Unlock()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_refresh_tokens (token_hash,client_id,user_id,resource,scope,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`, token.Hash, token.ClientID, token.UserID, token.Resource, token.Scope, token.ExpiresAt)
	return err
}

func (s *oauthStore) consumeRefresh(ctx context.Context, hash, clientID string) (refreshToken, bool, error) {
	if s.db == nil {
		s.mu.Lock()
		token, ok := s.refresh[hash]
		if ok && (token.ClientID != clientID || token.Revoked || token.ExpiresAt.Before(time.Now())) {
			ok = false
		}
		if ok {
			revoked := token
			revoked.Revoked = true
			s.refresh[hash] = revoked
		}
		s.mu.Unlock()
		return token, ok, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return refreshToken{}, false, err
	}
	defer tx.Rollback()
	var token refreshToken
	err = tx.QueryRowContext(ctx, `SELECT token_hash,client_id,user_id,resource,scope,expires_at,revoked FROM oauth_refresh_tokens WHERE token_hash=$1 AND client_id=$2 AND revoked=false AND expires_at > now() FOR UPDATE`, hash, clientID).
		Scan(&token.Hash, &token.ClientID, &token.UserID, &token.Resource, &token.Scope, &token.ExpiresAt, &token.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return refreshToken{}, false, nil
	}
	if err != nil {
		return refreshToken{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_refresh_tokens SET revoked=true WHERE token_hash=$1`, hash); err != nil {
		return refreshToken{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return refreshToken{}, false, err
	}
	return token, true, nil
}
