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

ALTER TABLE IF EXISTS namespaces
  DROP CONSTRAINT IF EXISTS namespaces_namespace_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_namespaces_active
ON namespaces(namespace)
WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_namespaces_user_id ON namespaces(user_id);

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
