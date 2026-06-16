# Sentinel API authn/authz matrix

This is the **source of truth** for which roles can call which endpoint on the
split Sentinel API services (`mcp-platform-api`, `mcp-runtime-control`,
`mcp-analytics-api`). The `security-audit-platform` skill (see
`.codex/skills/security-audit-platform/SKILL.md`, Step 2) compares the live
services against this table. A divergence in either direction is a finding:

- A route in `services/platform-api/routes.go`, `services/runtime-control/routes.go`,
  or `services/analytics-api/routes.go` that is missing from this table → add a
  row, then verify the expected status.
- A row whose expected status differs from the live response → fix the
  route or fix the matrix, but do not silently change one to match the
  other.

Source of routes: per-service `routes.go` files (public surface is `/api/v1/*`
only). Path items ending with `/` route to a handler that parses sub-paths
internally; include both the prefix and the meaningful sub-paths in the table.

> **Migration note:** Rows below still use legacy `/api/*` path prefixes from the
> monolith era. When auditing live clusters, prefix each path with `/api/v1` and
> route to the owning service per [`api-service-split.md`](../internals/api-service-split.md).

## Roles

| Role             | Credential                                                   | Source secret key                                             |
|------------------|--------------------------------------------------------------|---------------------------------------------------------------|
| `anon`           | none                                                         | n/a                                                           |
| `user-cookie`    | logged-in browser session (platform identity)                | seeded via `PLATFORM_DEV_*` in test mode, OIDC otherwise      |
| `user-key`       | `x-api-key` matching a user-scoped key                       | `mcp-sentinel-secrets.UI_API_KEY` (also dual-purposed for UI) |
| `admin-key`      | `x-api-key` matching `ADMIN_API_KEYS` entry                  | `mcp-sentinel-secrets.ADMIN_API_KEYS`                         |
| `ingest-key`     | `x-api-key` matching `INGEST_API_KEYS` entry                 | `mcp-sentinel-secrets.INGEST_API_KEYS`                        |

`admin-role` is enforced by `platformauth.Authenticator.RequireRole` wraps in
each split service's `routes.go`. `user-cookie` and `user-key` distinguish which
calls require browser identity vs accept a bearer-style key.
When `ADMIN_API_KEYS` is unset, static `API_KEYS` authenticate as `user`
unless the explicit legacy dev/test fallback is enabled.

Expected codes:

- **200/204** — allowed; handler returns successfully.
- **401** — auth missing/invalid.
- **403** — auth valid but role insufficient.
- **404** — handler returns not-found for valid auth (path-item endpoints).
- **405** — method not allowed (handler-side check).

## Public endpoints

| Path                | Methods | anon | user-cookie | user-key | admin-key | ingest-key | Notes |
|---------------------|---------|------|-------------|----------|-----------|------------|-------|
| `/health`           | GET     | 200  | 200         | 200      | 200       | 200        | Returns 503 when `runtimeInit != ""`. |
| `/api/auth/login`   | POST    | 200  | 200         | 200      | 200       | 200        | Local password login when `PLATFORM_DEV_*` enabled. |
| `/api/auth/oidc`    | POST    | 200  | 200         | 200      | 200       | 200        | OIDC code exchange. |
| `/api/auth/signup`  | POST    | 200  | 200         | 200      | 200       | 200        | Disabled in prod when configured; verify before deploy. |

> All four of these routes are reachable without auth by design. If any
> later mutates state on behalf of a caller without further checks, that
> is a Critical finding.

## User-authenticated endpoints (any authenticated identity)

| Path                                                  | Methods       | anon | user-cookie | user-key | admin-key | ingest-key | Notes |
|-------------------------------------------------------|---------------|------|-------------|----------|-----------|------------|-------|
| `/api/auth/me`                                        | GET           | 401  | 200         | 200      | 200       | 401/403    | Returns identity claims; ingest-only keys are not API auth identities. |
| `/api/user/registry-credentials`                      | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | Ingest-only key must NOT manage user creds. Verify rejection. |
| `/api/user/registry-credentials/{id}`                 | GET, PUT, DEL | 401  | 200         | 200      | 200       | 401/403    | Same. |
| `/api/user/activity/image-publish`                    | GET           | 401  | 200         | 200      | 200       | 401/403    | User-scoped read; ingest key should not see other users. |
| `/api/user/api-keys`                                  | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | Lifecycle for user-owned keys. |
| `/api/user/api-keys/{id}`                             | GET, DEL      | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/servers`                                | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | List/create MCP servers. |
| `/api/runtime/observability/links`                    | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Normal users are limited to team namespaces or caller-owned catalog servers. |
| `/api/runtime/observability/grafana/dashboard`        | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Renders a server-scoped dashboard through the API. |
| `/api/runtime/observability/prometheus/query`         | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | PromQL is allowlisted and server-scoped by the API. |
| `/api/runtime/teams`                                  | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/teams`                                  | POST          | 401  | 403         | 403      | 200       | 401/403    | Admin-only team + namespace provisioning. |
| `/api/runtime/teams/{id}`                             | GET           | 401  | 200         | 200      | 200       | 401/403    | Team members can read only their teams; admins can read all teams. |
| `/api/runtime/teams/{id}/members`                     | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | POST requires admin or team owner. |
| `/api/users`                                          | POST          | 401  | 403         | 403      | 200       | 401/403    | Admin-only password user create. |
| `/api/runtime/namespaces`                             | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/namespaces/{name}`                      | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/deployments`                                    | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/deployments/{id}`                               | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/server-events`                          | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Full event details only for admin, server owner, or team owner; regular namespace readers are forbidden. |
| `/api/runtime/grants`                                 | GET           | 401  | 200         | 200      | 200       | 401/403    | Lists only grants for servers the caller can administer; regular team/catalog readers receive an empty scoped set. |
| `/api/runtime/grants`                                 | POST          | 401  | 200/403     | 200/403  | 200       | 401/403    | Create/update requires admin, server owner, or team owner. |
| `/api/runtime/grants/{ns}/{name}`                     | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Full grant summary only for admin, server owner, or team owner. |
| `/api/runtime/grants/{ns}/{name}`                     | DELETE        | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; same owner/team-owner gate as apply. |
| `/api/runtime/grants/{ns}/{name}/enable`              | POST          | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; same owner/team-owner gate as apply. |
| `/api/runtime/grants/{ns}/{name}/disable`             | POST          | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; same owner/team-owner gate as apply. |
| `/api/runtime/sessions`                               | GET           | 401  | 200         | 200      | 200       | 401/403    | Lists only sessions for servers the caller can administer. |
| `/api/runtime/sessions`                               | POST          | 401  | 403         | 403      | 200       | 401/403    | Direct session apply is admin/internal-only; users should use `/api/runtime/adapter/sessions`. |
| `/api/runtime/sessions/{ns}/{name}`                   | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Full session summary only for admin, server owner, or team owner. |
| `/api/runtime/sessions/{ns}/{name}`                   | DELETE        | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; requires admin, server owner, or team owner. |
| `/api/runtime/sessions/{ns}/{name}/revoke`            | POST          | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; requires admin, server owner, or team owner. |
| `/api/runtime/sessions/{ns}/{name}/unrevoke`          | POST          | 401  | 200/403     | 200/403  | 200       | 401/403    | Mutating; requires admin, server owner, or team owner. |
| `/api/runtime/policy`                                 | GET           | 401  | 200/403     | 200/403  | 200       | 401/403    | Rendered policy is visible only to admin, server owner, or team owner. |

## Admin-only endpoints (`requireRole(roleAdmin, …)`)

| Path                              | Methods       | anon | user-cookie (non-admin) | user-key | admin-key | ingest-key | Notes |
|-----------------------------------|---------------|------|-------------------------|----------|-----------|------------|-------|
| `/api/events`                     | GET           | 401  | 403                     | 403      | 200       | 403        | Admin event log. |
| `/api/stats`                      | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/sources`                    | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/event-types`                | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/analytics/usage`            | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/events/filter`              | POST          | 401  | 403                     | 403      | 200       | 403        | |
| `/api/dashboard/summary`          | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/admin/namespaces`           | GET, POST     | 401  | 403                     | 403      | 200       | 403        | |
| `/api/admin/audit`                | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/admin/operations`           | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/admin/deployments`          | GET           | 401  | 403                     | 403      | 200       | 403        | |
| `/api/runtime/components`         | GET           | 401  | 403                     | 403      | 200       | 403        | Cluster component/workload health and errors. |
| `/api/runtime/actions/restart`    | POST          | 401  | 403                     | 403      | 200       | 403        | Cluster-affecting. |

## Method gating

`http.NewServeMux` does not enforce HTTP methods; the handler does. For each
row above, the auditor must also confirm:

- The handler rejects methods not listed with **405**.
- A method-confused request (e.g., `OPTIONS` followed by `POST`) cannot
  bypass the role check.
- Unknown sub-paths under `/api/runtime/grants/` and similar prefix routes
  return **404**, not 200 with a default action.

## Drift check

The platform audit harness in
`.codex/skills/security-audit-platform/SKILL.md` (Step 2) reads this table
and exercises each row. To regenerate the machine-readable form for that
harness, hand-translate this markdown to `docs/security/authz-matrix.json`
with one object per row:

```json
{ "path": "/api/events", "method": "GET", "role": "ingest", "expect": 403 }
```

Keep the JSON and the markdown in sync; the markdown is canonical for
review, the JSON is canonical for the harness.

## Open follow-ups

- Add Go tests in `services/platform-api/`, `services/runtime-control/`, and
  `services/analytics-api/` that load `authz-matrix.json` and assert each row
  against the live `http.Handler` using `httptest.NewServer`. Today this matrix
  is verified manually; the test should fail when a route is added without a
  matrix entry.
- Confirm whether `user-key` and `user-cookie` should be merged for
  authorization purposes, or whether some routes (e.g., billing, admin
  bootstrap) intentionally accept only one credential type.
- Document whether `/api/runtime/*` mutations are scoped to the calling
  user's tenancy, and how that tenancy is derived (subject claim,
  namespace label, etc.). The audit cannot judge cross-tenant safety
  without that anchor.
