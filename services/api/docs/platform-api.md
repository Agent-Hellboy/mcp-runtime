# Platform Identity, Deployment, and User API

This file documents the platform-specific routes served by `services/api`.
The public documentation pages are the canonical full references:

- `../../../docs/api.md` for the full API reference.
- `../../../docs/sentinel.md` for the Sentinel service-by-service HTTP surface.

## Enable platform identity

Enable the platform identity database with:

```bash
export POSTGRES_DSN='postgres://user:pass@postgres:5432/mcp_runtime?sslmode=disable'
export PLATFORM_JWT_SECRET='<32+ random bytes>'
```

`DATABASE_URL` is also accepted when `POSTGRES_DSN` is not set. The API applies
the SQL schema at startup; the same schema is available in
`services/api/migrations/001_platform_identity.sql`.

Optional bootstrap variables:

```bash
export PLATFORM_ADMIN_EMAIL='admin@example.com'
export PLATFORM_ADMIN_PASSWORD='change-me-now'
```

`PLATFORM_ADMIN_BOOTSTRAP_ONLY=1` runs only the admin bootstrap path and exits.
`PLATFORM_ADMIN_PASSWORD` should be cleared after bootstrap.

## Route summary

All routes below are served by `services/api` on `PORT` (default `8080`) unless
noted otherwise. Authenticated routes accept `Authorization: Bearer <token>` or
`x-api-key: <key>`.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | API health and runtime initialization status. |
| `GET` | `/metrics` | Prometheus metrics on `METRICS_PORT` (default `9090`). |
| `POST` | `/api/auth/signup` | Create a platform user. |
| `POST` | `/api/auth/login` | Exchange email/password for a platform bearer token. |
| `POST` | `/api/auth/oidc` | Exchange a configured OIDC ID token for a platform bearer token. |
| `GET` | `/api/auth/me` | Return the authenticated principal. |
| `GET`, `POST` | `/api/user/api-keys` | List or create caller-owned API keys. |
| `POST` | `/api/user/api-keys/{id}/revoke` | Revoke one caller-owned API key. |
| `GET` | `/api/user/analytics/usage` | Caller-scoped MCP server usage analytics for the user dashboard. |
| `GET`, `POST` | `/api/user/registry-credentials` | List or create registry credentials. |
| `POST` | `/api/user/registry-credentials/{id}/revoke` | Revoke one registry credential. |
| `*` | `/api/registry/authz` | Traefik forward-auth endpoint for bundled registry ingress. Admin role required. |
| `GET`, `POST` | `/api/deployments` | List or apply platform-managed deployments. |
| `DELETE` | `/api/deployments/{namespace}/{name}` | Delete a platform-managed deployment and service. |
| `GET` | `/api/admin/namespaces` | Admin namespace inventory. |
| `GET` | `/api/admin/deployments` | Admin deployment inventory, optionally filtered by `namespace`. |
| `GET` | `/api/admin/audit` | Admin audit timeline filtered by `user`, `since`, `until`, and `limit`. |
| `GET` | `/api/admin/operations` | Admin operations snapshot for user activity, image activity, deployments, and timeline events. |
| `POST` | `/api/user/activity/image-publish` | Record a successful image publish event for the authenticated user. |

The same API service also hosts admin analytics and runtime governance routes
such as `/api/events`, `/api/analytics/usage`, `/api/runtime/grants`, and
`/api/runtime/sessions`; keep those details in `../../../docs/api.md`.

## Signup and Login

Create a normal user:

```bash
curl -sS -X POST http://localhost:8080/api/auth/signup \
  -H 'content-type: application/json' \
  -d '{"email":"prince@example.com","password":"change-me-now"}'
```

Log in with email/password:

```bash
curl -sS -X POST http://localhost:8080/api/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"prince@example.com","password":"change-me-now"}'
```

Both return a bearer `access_token` with a 15-minute lifetime:

```json
{
  "access_token": "...",
  "token_type": "bearer",
  "expires_in": 900,
  "user": {
    "id": "...",
    "email": "prince@example.com",
    "role": "user",
    "namespace": "user-..."
  }
}
```

Admin signup requires an existing admin credential on the request:

```bash
curl -sS -X POST http://localhost:8080/api/auth/signup \
  -H "x-api-key: $ADMIN_KEY" \
  -H 'content-type: application/json' \
  -d '{"email":"admin2@example.com","password":"change-me-now","role":"admin"}'
```

OIDC login requires `OIDC_ISSUER`, `OIDC_AUDIENCE`, and `OIDC_JWKS_URL`:

```bash
curl -sS -X POST http://localhost:8080/api/auth/oidc \
  -H 'content-type: application/json' \
  -d '{"id_token":"'"$ID_TOKEN"'"}'
```

Inspect the current bearer token or API key:

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/auth/me
```

## API Keys

Create a caller-owned API key:

```bash
curl -sS -X POST http://localhost:8080/api/user/api-keys \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"laptop"}'
```

The cleartext key is returned once as `api_key` and `one_time_key` so browser
and API clients can show the value from an explicit one-time field. The
database stores only a SHA-256 hash.

List keys:

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/user/api-keys
```

Revoke a key:

```bash
curl -sS -X POST -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/user/api-keys/"$KEY_ID"/revoke
```

## Runtime MCP Servers

Apply an MCPServer through the platform API:

```bash
curl -sS -X POST http://localhost:8080/api/runtime/servers \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"demo","namespace":"user-1","spec":{"image":"registry.example.com/user-1/demo:latest"}}'
```

The response includes `publish_policy` on list calls. Admins configure the
active-server limit with `PLATFORM_MCP_ACTIVE_SERVER_LIMIT` (default `5`, `0`
disables) and per-server cooldown with `PLATFORM_MCP_PUSH_COOLDOWN` (default
`0s`, Go duration format). Quota or cooldown denials return `429`; cooldown
responses include `next_allowed_at` and `Retry-After`. The active-server limit
is enforced by the platform API before Kubernetes apply; strict serialization of
concurrent publishes would require a shared reservation or admission-control
layer.

Retire an MCPServer to free quota:

```bash
curl -sS -X DELETE -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/runtime/servers/user-1/demo
```

Fetch recent analytics for a server:

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/runtime/server-events?namespace=user-1&server=demo'
```

## Deployments

Normal users deploy only into their owned namespace. Admins may pass
`namespace`.

```bash
curl -sS -X POST http://localhost:8080/api/deployments \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"demo","image":"registry.example.com/user-1/demo","version":"v1","port":8088,"replicas":1}'
```

The API applies a Kubernetes `Deployment` and `Service` labelled as
platform-managed. `version` is appended as an image tag when `image` does not
already include a tag.

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/deployments
```

```bash
curl -sS -X DELETE -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/deployments/user-1/demo
```

## Registry Credentials

Registry credentials are separate from platform API keys so Docker-cached
credentials can be revoked independently.

```bash
curl -sS -X POST http://localhost:8080/api/user/registry-credentials \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"docker laptop"}'
```

Use the returned `username` and one-time `password` with the configured registry
host. The bundled public registry ingress accepts these Basic credentials only
when they belong to an admin user; non-admin image publishing should use the
platform deployment and registry workflows instead of direct registry API calls.

List registry credentials:

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/user/registry-credentials
```

Revoke one registry credential:

```bash
curl -sS -X POST -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/user/registry-credentials/"$CREDENTIAL_ID"/revoke
```

## Admin

Admin routes require an admin principal.

```bash
curl -sS -H "authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:8080/api/admin/namespaces
```

```bash
curl -sS -H "authorization: Bearer $ADMIN_TOKEN" \
  'http://localhost:8080/api/admin/deployments?namespace=user-1'
```
