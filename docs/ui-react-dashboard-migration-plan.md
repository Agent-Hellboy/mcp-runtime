# Sentinel Dashboard React Migration Plan

Status: complete. All five phases shipped; the legacy dashboard is removed.

Tracking issue: [#388](https://github.com/Agent-Hellboy/mcp-runtime/issues/388)

This plan turns the broad dashboard UX request in [#47](https://github.com/Agent-Hellboy/mcp-runtime/issues/47) into a staged migration that can be reviewed and shipped one small pull request at a time.

The target is the Sentinel dashboard at `/`, not the articles site. The articles React work in [#289](https://github.com/Agent-Hellboy/mcp-runtime/issues/289) is tracked as a related workstream because it can share frontend conventions and CI improvements, but it must not block dashboard delivery.

## Why this needs a staged migration

The current root UI is a React/Vite shell that renders the complete dashboard through `services/ui/static/legacy/index.html` in an iframe. The legacy dashboard already owns authentication, server/catalog rendering, activity, keys, analytics, teams, access control, operations, and settings.

The live dashboard review found three concrete problems that the migration must address:

1. A successful UI login creates the `mcp_ui_session` cookie, but protected runtime requests from the browser do not carry a runtime API credential. The signed-in dashboard therefore receives `401 Unauthorized` and stays on the sign-in state even though runtime data exists. Phase 1 closes this for catalog reads through `GET /api/ui/v1/runtime/{namespaces,servers,tools}`.
2. The mobile tool table is wider than a 390px viewport and the page-level `overflow-x: hidden` masks the clipped Connect column instead of providing a usable horizontal table region.
3. Issue #47 has no acceptance criteria, so “improve UX” cannot currently be verified or reviewed consistently.

## Recommended architecture

Use an incremental strangler migration:

- React becomes the top-level dashboard shell.
- A small same-origin UI backend-for-frontend (BFF) translates the HttpOnly UI session into the server-held runtime credential. Browser JavaScript never receives the upstream bearer token or service API key.
- React owns one vertical slice at a time, starting with the Servers/catalog workspace.
- Unmigrated dashboard areas remain reachable through an explicit legacy-dashboard entry point.
- Each migrated surface has its own API client, loading/error/empty states, component tests, and browser evidence before the corresponding legacy surface is retired.

Do not do a big-bang rewrite. It would combine authentication, routing, API ownership, accessibility, responsive layout, and every dashboard workflow into one high-blast-radius change.

## Phase plan and acceptance criteria

### Phase 0 — Baseline, tracking, and design contract

Deliver a tracking issue, this plan, a short component/API boundary note, and a reproducible UI baseline.

Acceptance criteria:

- [x] The tracking issue links #47 and #289 and lists this plan as the source of truth.
- [x] Browser evidence records signed-out, signed-in, admin, mobile, console, and network behavior before each migration phase. Full for Phases 1, 2, and 5; Phases 3 and 4 shipped without a live browser pass at merge time and were covered retroactively by Phase 5's final sweep instead of their own before/after evidence.
- [x] The React app has a documented API-client boundary and does not read credentials from `window`, local storage, or session storage.
- [x] ~~The legacy dashboard remains the fallback~~ — superseded: every surface reached its phase gate and the fallback itself is removed (Phase 5).

### Phase 1 — Fix the authenticated UI data path

Make the session created by `/auth/login` usable by the first React surface without exposing upstream credentials.

Acceptance criteria:

- [x] The UI service proxies only explicitly approved migrated API paths to their owning service.
- [x] A valid UI session is translated server-side to its stored bearer/API-key credential.
- [x] Browser requests use `credentials: same-origin`; no bearer token or service API key appears in JavaScript, HTML, browser storage, or rendered DOM.
- [x] Missing, expired, or invalid sessions return `401` and the React app returns to a clear sign-in state.
- [x] Proxy tests cover session credential injection, credential non-leakage, query preservation, upstream status/body/header propagation, and unauthenticated requests.
- [x] Existing direct API routes and adapter/mTLS traffic retain their current ingress ownership.

Phase 1 gate evidence from 2026-09-15:

- Before deployment: the signed-out iframe dashboard loaded without console
  errors, while authenticated catalog reads used the direct API path and
  returned `401`.
- Candidate deployment: the UI pod ran the arm64 image built from this branch,
  with `RUNTIME_UPSTREAM` and `ANALYTICS_UPSTREAM` configured.
- After deployment: signed-in catalog requests to namespaces, servers, and
  tools returned `200`; search, risk/status filters, logout, and the signed-out
  `401` path passed; at 390px the tool table used its bounded scroll region and
  the document itself did not overflow.
- The live cluster's analytics service currently has no ready endpoints and the
  existing dashboard summary returns `500`; those unrelated dependencies make
  Analytics/Governance fallback checks `BLOCKED`, while the Servers/catalog
  workflow is passing.

### Phase 2 — React shell and Servers/catalog vertical slice

Port the dashboard shell and the highest-value read-only workspace first.

Acceptance criteria:

- [x] The root page renders a React header, workspace navigation, account state, and responsive content layout without an iframe for the Servers view.
- [x] Signed-out users see a useful explanation and a clear sign-in action; authenticated users see the server workspace.
- [x] The workspace loads namespaces, servers, and tools from the scoped API client.
- [x] The view has explicit loading, error, unauthorized/session-expired, empty, and no-filter-match states.
- [x] Server cards show name, namespace, readiness/status, description, tool count, and endpoint when available.
- [x] Tool catalog supports search, namespace, server-status, and risk filters while preserving the API’s trust, side-effect, risk, and drift fields.
- [x] The tool table is usable on mobile through a bounded horizontal scroll region; page-level overflow does not hide content.
- [x] Keyboard focus, labels, button names, table headers, and alert semantics pass the targeted accessibility checks.
- [x] React unit/component tests cover auth states, filters, API failures, empty data, and server/tool rendering.
- [x] Browser QA covers desktop and 390px mobile with no console errors and successful network responses.

Phase 2 gate evidence from 2026-09-15 (`mcp-sentinel-ui:phase1-session-bff-20260915-r2`
before, `mcp-sentinel-ui:phase2-react-servers-20260915-191447` after, both
`linux/arm64` on the Kind node):

- Catalog parity: both deployments rendered 3 servers and 14 tools for the admin
  session; the search term `aaa-pi` matched 3 tools in each; risk buckets after
  the port were low 10 / medium 4 / high 0, and the legacy pass's single
  high-risk row was its "no match" placeholder, not a tool.
- Reads stayed on the session-backed proxy: `GET /api/ui/v1/runtime/namespaces`,
  `/runtime/servers`, `/runtime/tools` all `200`, and namespace scoping issued
  `…/runtime/servers?namespace=<ns>`. React issues one catalog read per scope
  where the legacy page issued five `/runtime/servers` polls.
- No credential reached browser JavaScript: `localStorage`, `sessionStorage`,
  and JS-visible `document.cookie` were all empty, the session cookie stayed
  `HttpOnly; SameSite=Strict`, `/config.js` carried no key, and the only
  credential-shaped string in the bundle is the client's own header-stripping
  code.
- States were visibly distinct: loading, `role="alert"` error with a retry, the
  session-expired state, "No MCP servers match this scope.", and
  "No tools match these filters."
- At 390px `document.documentElement.scrollWidth` stayed 390 while the 624px
  table scrolled inside its bounded region. Zero console errors across
  signed-out, login, catalog, filter, selection, mobile, and logout steps.
- Legacy fallback: the "More workspaces" tab loaded the legacy dashboard with
  its own role-gated tabs (Servers, Keys, Analytics, Teams, Access Control, Ops,
  Settings for admin), `Role: admin`, and 3 server cards. Keys, Teams, and
  Settings were clean.
- Library choice: the slice uses two headless libraries and keeps the Sentinel
  CSS tokens — TanStack Query for the namespace-scoped catalog reads (caching,
  request dedup, a 401-aware retry policy, invalidation) and TanStack Table v9
  for the tool table (sorting with ordinal risk/trust ranking, tree-shaken
  features). Native `<select>` and hand-rolled presentational components were
  kept deliberately: a component kit would have re-themed the shell away from
  the legacy dashboard it still sits above until Phase 5. Base UI was evaluated
  and rejected for now because it is still `1.0.0-rc.0`. Cost: the bundle grew
  from 216 kB to 292 kB raw (67 kB to 91 kB gzipped). Browser evidence: sorting
  works by mouse and keyboard with correct `aria-sort`, and returning to an
  already-fetched namespace scope issued zero network requests.

- BLOCKED, unchanged from Phase 1 and unrelated to this change: the cluster's
  analytics path is down (`mcp-analytics-api` not ready, ClickHouse/Kafka/
  processor in `CrashLoopBackOff`), so legacy Analytics and the Governance
  analytics panel return `502` on `/events` and `/analytics/usage` and `500` on
  `/dashboard/summary`. Governance grants/sessions and Operations inventory
  still rendered.

### Phase 3 — User workflows

Migrate user-owned read/write workflows after the read-only catalog is stable: activity, API keys, user analytics, and team membership views.

Cookie-backed mutating BFF routes must add CSRF protection before they are exposed. Phase 1 is GET-only and relies on `SameSite=Strict`.

Acceptance criteria:

- [x] Every mutating action has disabled/busy, success, validation, forbidden, and failure states.
- [x] One-time API-key material is only rendered in the intended one-time state and is not retained after refresh/navigation.
- [x] User-only data is hidden for admins and users without an identity, matching current authorization behavior.
- [x] Browser tests verify refresh, logout, and expired session (live-verified: a hard refresh mid-session re-authenticates and keeps every role-gated tab correctly offered). "Direct navigation to each route" doesn't apply as written — the shell has one URL and switches workspaces via client state, not routing; there is nothing to deep-link to.
- [x] Legacy and React views cannot issue duplicate writes during the migration.

CSRF decision: implemented, not deferred. `services/ui/csrf.go` adds a
session-bound synchroniser token minted in `createSession`, returned in the
`/auth/login` and `/auth/status` bodies, and required back in `X-CSRF-Token` on
every unsafe method reaching the session proxy. The authoritative copy lives in
the server-side session record, so an attacker who can set cookies on the origin
still cannot forge a matching pair - which a double-submit cookie would not
prevent. An `Origin` check runs first as an independent signal, and
`SameSite=Strict` stays as defense in depth. A separate `sessionProxyWriteRoutes`
allowlist governs writes, so widening reads can never silently widen writes; it
currently contains exactly `POST /user/api-keys` and `DELETE /user/api-keys/{id}`.
Unknown methods are treated as unsafe, and a session with no token can never
write, so both fail closed.

React is the only write path for these mutations; there is no longer a second dashboard that could duplicate a write.

### Phase 4 — Admin governance and operations

Migrate admin-only surfaces: teams, access control, operations, audit links, deployments, settings, and observability entry points.

Acceptance criteria:

- [x] Navigation and route guards fail closed based on the authenticated principal returned by the server.
- [x] Non-admin users cannot render or invoke admin-only controls, even by direct URL/navigation.
- [x] Destructive operations require an explicit confirmation and show the affected namespace/server/resource. Every one does: grant/session toggle and team-member remove name the resource; component restart names the component; server retire names both namespace and server explicitly.
- [x] Audit, grant, and session links preserve the current detail context and provide a usable back path.
- [x] Grafana and other external/admin links retain their existing forward-auth behavior.
- [x] Security regression QA covers role boundaries, `401`/`403`, CSRF-sensitive actions, and secret exposure. Live-verified: no cookie → 401; valid cookie, no CSRF token, unsafe method → 403; valid cookie with a cross-origin `Origin` header → 403; the shipped bundle contains no credential beyond its own header-stripping code.

Admin mutations landed once Phase 3's CSRF-protected write route existed:
disabling a grant, revoking a session, creating a team or removing a member,
restarting a component, and (closing the last gap Phase 5's re-check found)
retiring a server. Each is scoped by the same runtime API authorization as
its read counterpart, not gated to admin specifically — a non-admin owner can
retire their own server or manage grants/sessions the backend already lets
them see.

Role gating fails closed in three places: the admin tab is filtered out of the
navigation for any non-admin role, the App route resets to Servers if the
principal is not an admin, and `AdminGuard` refuses to render admin children
regardless of how the route was reached. A non-admin session issues no admin
request at all, because every admin query is `enabled` on the server-returned
principal.

BLOCKED: the grant and session drill-down activity panels read gateway decision
events from `/events`, which is the analytics path that is unavailable on the
contributor cluster (`502`). They render an explicit "activity unavailable"
error rather than an empty table, and are covered by mocked component tests.
They cannot be validated against live data until the analytics service is
restored.

### Phase 5 — Legacy retirement and React completion

Remove the iframe and legacy dashboard only after every workflow has an accepted React replacement.

Acceptance criteria:

- [x] A route/workflow inventory maps every legacy tab and action to an accepted React route or an explicitly removed product behavior (`docs/ui-legacy-retirement-inventory.md`).
- [x] No production navigation points to `/legacy/index.html`.
- [x] Legacy static assets and bridge code are deleted only after browser, API, accessibility, and security evidence is green.
- [x] The root UI remains deep-linkable, responsive, and compatible with the supported deployment ingress paths.
- [x] The final PR updates user/developer docs and closes/supersedes #47 with links to the shipped surfaces.

Two gaps found during the inventory were closed before removal: non-admin
access to grants/sessions (previously wrongly admin-gated) and the tenant
publish-quota stat (previously dropped). A third, Google Sign-In, had no
React implementation at all and was built from scratch
(`GoogleSignInButton`) rather than dropped, since it's a full sign-in method,
not a UI convenience.

## PR and branch sequence

Keep the changes reviewable and low blast-radius:

1. `docs/ui_react_migration_plan` — plan, tracking issue, and acceptance criteria.
2. `fix/ui_session_runtime_bridge` — scoped BFF and auth-path tests.
3. `feat/ui_react_servers_workspace` — React shell, Servers/catalog, and component/browser QA.
4. `feat/ui_react_user_workflows` — activity, keys, analytics, and team user flows.
5. `feat/ui_react_admin_workflows` — governance, operations, audit, deployments, and settings.
6. `refactor/ui_remove_legacy_dashboard` — final inventory check, asset removal, docs, and release-readiness evidence.

The articles migration in #289 should use its own branch/PR sequence. It may share frontend lint/build/test conventions and documented design tokens, but dashboard API/auth work should not be bundled into the articles PR.

## Test and review gates

For each phase, run the narrowest checks first and then the relevant full checks:

- Frontend: `npm test -- --run` and `npm run build` in `services/ui/frontend`.
- UI service: `go test ./services/ui/...`.
- Static/security: verify no credential material is emitted in `config.js`, bundles, DOM, or browser storage.
- Browser: use the `qa-e2e-ui` workflow for signed-out, signed-in, role, responsive, console, network, and accessibility evidence.
- Cross-boundary changes: run the applicable security-audit and release-readiness checks before merge.

## Definition of done for the full migration

Done. All five phases passed their acceptance criteria; the iframe and legacy assets are removed.
