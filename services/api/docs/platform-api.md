# Platform Identity and Deployment API

Enable the platform identity database with:

```bash
export POSTGRES_DSN='postgres://user:pass@postgres:5432/mcp_runtime?sslmode=disable'
export PLATFORM_JWT_SECRET='<32+ random bytes>'
```

`DATABASE_URL` is also accepted when `POSTGRES_DSN` is not set. The API applies the SQL schema at startup; the same schema is available in `services/api/migrations/001_platform_identity.sql`.

## Signup and Login

```bash
curl -sS -X POST http://localhost:8080/api/auth/signup \
  -H 'content-type: application/json' \
  -d '{"email":"prince@example.com","password":"change-me-now"}'
```

```bash
curl -sS -X POST http://localhost:8080/api/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"prince@example.com","password":"change-me-now"}'
```

Both return a bearer `access_token`. Admin signup requires an existing admin credential on the request.

Example (admin creates another admin user using an admin API key):

```bash
curl -sS -X POST http://localhost:8080/api/auth/signup \
  -H "x-api-key: $ADMIN_KEY" \
  -H 'content-type: application/json' \
  -d '{"email":"admin2@example.com","password":"change-me-now","role":"admin"}'
```

## API Keys

```bash
curl -sS -X POST http://localhost:8080/api/user/api-keys \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"laptop"}'
```

The cleartext API key is returned once. The database stores only a SHA-256 hash.

## Deployments

Normal users deploy only into their owned namespace. Admins may pass `namespace`.

```bash
curl -sS -X POST http://localhost:8080/api/deployments \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"demo","image":"registry.example.com/user-1/demo","version":"v1","port":8088,"replicas":1}'
```

```bash
curl -sS -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/deployments
```

```bash
curl -sS -X DELETE -H "authorization: Bearer $TOKEN" \
  http://localhost:8080/api/deployments/user-1/demo
```

## Registry Credentials

Registry credentials are separate from platform API keys so Docker-cached credentials can be revoked independently.

```bash
curl -sS -X POST http://localhost:8080/api/user/registry-credentials \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"docker laptop"}'
```

Use the returned `username` and one-time `password` with the configured registry host.

## Admin

```bash
curl -sS -H "authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:8080/api/admin/namespaces
```

```bash
curl -sS -H "authorization: Bearer $ADMIN_TOKEN" \
  'http://localhost:8080/api/admin/deployments?namespace=user-1'
```
