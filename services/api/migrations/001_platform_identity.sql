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

CREATE TABLE IF NOT EXISTS namespaces (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  namespace text unique not null,
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  token_hash text unique not null,
  expires_at timestamptz not null,
  revoked boolean not null default false,
  user_agent text,
  client_ip inet
);

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
  timestamp timestamptz not null default now()
);
