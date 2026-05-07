# Sentinel API authn/authz matrix

This is the **source of truth** for which roles can call which endpoint on
`mcp-sentinel-api`. The `security-audit-platform` skill (see
`.codex/skills/security-audit-platform/SKILL.md`, Step 2) compares the live
service against this table. A divergence in either direction is a finding:

- A route in `services/api/main.go` that is missing from this table → add a
  row, then verify the expected status.
- A row whose expected status differs from the live response → fix the
  route or fix the matrix, but do not silently change one to match the
  other.

Source of routes: `services/api/main.go:269-332` (top-level mux). Path
items ending with `/` route to a handler that parses sub-paths internally;
include both the prefix and the meaningful sub-paths in the table.

## Roles

| Role             | Credential                                                   | Source secret key                                             |
|------------------|--------------------------------------------------------------|---------------------------------------------------------------|
| `anon`           | none                                                         | n/a                                                           |
| `user-cookie`    | logged-in browser session (platform identity)                | seeded via `PLATFORM_DEV_*` in test mode, OIDC otherwise      |
| `user-key`       | `x-api-key` matching a user-scoped key                       | `mcp-sentinel-secrets.UI_API_KEY` (also dual-purposed for UI) |
| `admin-key`      | `x-api-key` matching `ADMIN_API_KEYS` entry                  | `mcp-sentinel-secrets.ADMIN_API_KEYS`                         |
| `ingest-key`     | `x-api-key` matching `INGEST_API_KEYS` entry                 | `mcp-sentinel-secrets.INGEST_API_KEYS`                        |

`admin-role` is enforced by the `requireRole(roleAdmin, …)` wrap in
`services/api/main.go`. `user-cookie` and `user-key` distinguish which
calls require browser identity vs accept a bearer-style key.

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
| `/api/auth/me`                                        | GET           | 401  | 200         | 200      | 200       | 200        | Returns identity claims. |
| `/api/user/registry-credentials`                      | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | Ingest-only key must NOT manage user creds. Verify rejection. |
| `/api/user/registry-credentials/{id}`                 | GET, PUT, DEL | 401  | 200         | 200      | 200       | 401/403    | Same. |
| `/api/user/activity/image-publish`                    | GET           | 401  | 200         | 200      | 200       | 401/403    | User-scoped read; ingest key should not see other users. |
| `/api/user/api-keys`                                  | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | Lifecycle for user-owned keys. |
| `/api/user/api-keys/{id}`                             | GET, DEL      | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/servers`                                | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | List/create MCP servers. |
| `/api/runtime/teams`                                  | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/teams/{id}`                             | GET, PUT, DEL | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/namespaces`                             | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/namespaces/{name}`                      | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/deployments`                                    | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/deployments/{id}`                               | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/grants`                                 | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | Mutating; serverRef cross-ns checks are best-effort (CLAUDE.md). |
| `/api/runtime/grants/{ns}/{name}`                     | DELETE        | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/grants/{ns}/{name}/enable`              | POST          | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/grants/{ns}/{name}/disable`             | POST          | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/sessions`                               | GET, POST     | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/sessions/{ns}/{name}`                   | DELETE        | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/sessions/{ns}/{name}/revoke`            | POST          | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/sessions/{ns}/{name}/unrevoke`          | POST          | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/components`                             | GET           | 401  | 200         | 200      | 200       | 401/403    | |
| `/api/runtime/policy`                                 | GET           | 401  | 200         | 200      | 200       | 401/403    | |

> **Open question** for the next audit pass: which of the
> `/api/runtime/*` mutating routes (grants/sessions create, enable,
> disable, revoke, unrevoke) should be admin-only? `services/api/main.go`
> wraps them with `server.auth(...)` only, not `requireRole(roleAdmin, …)`,
> meaning any authenticated user can mutate governance state. If that is
> by design, document the per-tenant scoping invariant; if not, this is
> a **Critical** governance finding.

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

- Add a Go test in `services/api/` that loads `authz-matrix.json` and
  asserts each row against the live `http.Handler` using
  `httptest.NewServer`. Today this matrix is verified manually; the test
  should fail when a route is added without a matrix entry.
- Confirm whether `user-key` and `user-cookie` should be merged for
  authorization purposes, or whether some routes (e.g., billing, admin
  bootstrap) intentionally accept only one credential type.
- Document whether `/api/runtime/*` mutations are scoped to the calling
  user's tenancy, and how that tenancy is derived (subject claim,
  namespace label, etc.). The audit cannot judge cross-tenant safety
  without that anchor.
