# MCP Sentinel platform redesign: research and implementation prompt

Status: implementation is in progress on the current redesign worktree (2026-09-18). This file is
kept as the research, parity, and requirements record; see
[`ui-console-design-system.md`](./ui-console-design-system.md) for what the
console actually does now, including where the shipped result deviates from the
starting values below.

## Research direction

The references are adjacent developer products, not MCP marketplaces. The
recommended synthesis is Vercel's clear resource context, Supabase's data and
form patterns, Linear's restrained application chrome, and Railway/Render's
service-oriented inspection. Keep MCP Sentinel's existing navy/teal/blue
identity. The dimensions and screen requirements below are proposed design
decisions for this repository, not claims about those products' exact tokens.

| Reference | Observed pattern | Application here |
| --- | --- | --- |
| [Vercel navigation](https://vercel.com/changelog/dashboard-navigation-redesign-rollout) | Collapsible sidebar, consistent navigation across scopes, workflow ordering | Stable app context; simple Servers home; grouped administration |
| [Vercel log filtering](https://vercel.com/changelog/redesigned-search-and-filtering-for-runtime-logs) | Removable visual filters and values informed by actual data | Explicit catalog/audit filters with removable chips |
| [Supabase layout](https://supabase.com/design-system/docs/ui-patterns/layout) | Different widths for focused forms and dense data surfaces | Narrow forms, wide tables, compact page headers |
| [Supabase tables](https://supabase.com/design-system/docs/ui-patterns/tables) | Semantic tables plus TanStack behavior for interactive datasets | Shared pagination, sorting, filters, and row actions |
| [Supabase forms](https://supabase.com/design-system/docs/ui-patterns/forms) | Consistent field layouts, dirty-state handling, errors near their source | Predictable creation/editing flows across teams, access, and keys |
| [Supabase empty states](https://supabase.com/design-system/docs/ui-patterns/empty-states) | Distinguishes first use from filtered zero results | Useful onboarding and clear-filter recovery |
| [Linear redesign](https://linear.app/now/how-we-redesigned-the-linear-ui) | Consistent headers, panels, spacing, and stronger visual hierarchy | Compact, calm chrome throughout the product |
| [Railway metrics](https://docs.railway.com/observability/metrics) | Metrics inspected in a service's context | Keep relevant server context during inspection |
| [Render metrics and events](https://render.com/changelog/in-dashboard-metrics-now-display-service-events) | Events associated with metric changes | Related activity alongside status; only chart data actually returned |
| [Vercel modal](https://vercel.com/geist/modal) / [drawer](https://vercel.com/geist/drawer) | Different treatment for inspection and blocking decisions | Detail sheets for inspection, explicit confirmation for disruptive actions |

## Verified repository and environment context

- Source repository: `/Users/proshan/mcp-runtime`.
- Existing task worktree: `/Users/proshan/mcp-runtime-worktrees/servers-console-redesign`.
- The checked-out branch currently contains the existing React redesign plus the
  implementation slice described below; the final legacy retirement gate is not
  complete.
- Frontend: React 19, TypeScript, Vite, TanStack Query and Table, semantic CSS.
- Baseline: 11 frontend test files / 124 tests passed; production build passed.
- Baseline build: JS 349.06 kB / 103.64 kB gzip; CSS 15.79 kB / 3.98 kB gzip.
- The local `kind-mcp-runtime` deployment is reachable at `http://localhost:18080/`.
- At inspection, that local deployment rendered an older iframe dashboard.
  Its sign-in attempt exposed additional tabs but returned to a sign-in prompt
  with console errors. Successful authenticated catalog behavior was not
  established. Do not count that as a passing baseline or assume the checked-out
  React UI is already deployed.
- Current source includes React admin mutations and CSRF handling. Some migration
  documentation still describes earlier phases; check current code before acting.
- This pass also updates the React implementation with typed analytics
  time-series/recent-activity data, a truthful usage chart, client-specific
  server connection tabs, server recent-activity states, and a user recent-
  activity table. Full legacy retirement is still pending. No production
  deployment was performed.

## Copy-ready implementation prompt

You are a senior product designer and React engineer implementing a complete
redesign of MCP Sentinel, the MCP Runtime platform console. Build the result in
the existing repository. Cover every current platform workflow, including its
loading, empty, error, mobile, and permission states.

### 1. Working context

Use the existing worktree:
`/Users/proshan/mcp-runtime-worktrees/servers-console-redesign`
on branch `ui/servers_console_redesign`.

Check git status and preserve other work. If this worktree is unavailable,
create an isolated task worktree from the appropriate current source; do not
reuse an unrelated working directory. Read AGENTS.md and relevant local UI and
development guidance.

Use the local Kind dashboard at `http://localhost:18080/` for baseline inspection.
The context should be `kind-mcp-runtime`. The source frontend and deployed image
may differ. Record the difference. Serve the candidate from a separate local
UI instance connected to the Kind APIs, or use the repository's authorized
local candidate-deployment workflow. Verify which build the browser is testing.
Do not deploy to production as part of this task.

Do the implementation, verification, and visual refinement. A plan, screenshots
of a prototype, or a restyled home page alone does not complete this task.

### 2. Product requirements

- Servers remains the default home page.
- Signed-out primary navigation contains only Servers.
- Remove the "More workspaces" entry and nested dashboard presentation.
- Keep server health, server browsing, tool inventory, filters, and tool
  inspection inside Servers; do not add separate global Overview or Tools pages.
- Preserve role-authorized access to Activity, API keys, and Administration.
- Redesign all administration sections: access grants, agent sessions, access
  details, teams, membership, operations, audit activity, image activity,
  platform health, and usage analytics.
- Retain the existing permission model. Navigation should reflect server-backed
  authorization; presentation changes must not grant new permissions.
- Do not add unused navigation, placeholder pages, fake notifications, pretend
  environment switchers, or nonfunctional buttons.

### 3. Design direction and references

Create a precise, calm developer console. Use the research links above as
interaction references, then adapt them to this product's own entities.

Use Vercel for resource context and readable actions, Supabase for tables and
forms, Linear for alignment and low-noise chrome, and Railway/Render for keeping
operational details associated with the selected service.

Study their application screenshots and component examples. Avoid deriving an
operational dashboard from marketing-site hero layouts. Do not reproduce logos,
screenshots, branding, or proprietary component source in the application.

### 4. Visual specification

Keep the existing brand colors and define semantic aliases centrally:

Dark:
- canvas: #080d16
- surface: #0f1724
- raised surface: #131e2f
- selected surface: #142238
- primary text: #f4f7fb
- secondary text: #9aaac0
- brand teal: #7ce7d3
- supporting blue: #73a7ff
- success: #4ade80
- warning: #fbbf24
- danger: #f87171

Light:
- canvas: #f3f7fb
- surface: #ffffff
- selected surface: #edf4fb
- primary text: #102033
- secondary text: #53657b
- brand teal: #087f73
- supporting blue: #356dcc
- success: #167446
- warning: #9a6700
- danger: #b42318

Use semantic color pairs and verify rendered contrast. Do not blindly reuse
dark-theme foregrounds on light-theme backgrounds.

Layout and typography:
- A compact topbar around 56–64px high.
- A 4px spacing scale, mostly 8/12/16/24/32px.
- Desktop page padding around 24–32px; mobile around 16px.
- Normal controls around 36–40px high; comfortable touch targets on mobile.
- Controls with 6–8px radii; panels around 10–12px; pills reserved for badges.
- Page titles around 26–30px; section titles 16–18px; body 14px; metadata 12px.
- Space Grotesk for the application. Restrict any existing display serif to a
  small brand treatment, not large operational page headings.
- Monospace for IDs, endpoints, image references, and code. Tabular numerals
  for metrics and comparable numeric columns.
- Restrained borders and clear surface levels. Reserve shadows for overlays.
- One consistent icon family, with 16–20px icons and matching stroke weights.
- Teal for primary actions and selection; blue for links; red for failures and
  destructive actions. Keep gradients limited to subtle brand accents.
- Transitions around 120–180ms, respecting reduced motion.

Use named tokens for all repeated design decisions. Refactor conflicting styles
rather than accumulating an override stylesheet that fights previous rules.

### 5. Application shell and navigation

Use a compact branded topbar with account context, documentation, theme, and
account controls. Show real user/team/namespace information where supported.
If only a namespace filter exists, label it Namespace; do not invent an
organization switcher or a Production environment.

Keep top-level navigation short: Servers, Activity, API keys, Administration,
filtered by the existing principal gates. Servers is first and opens at `/`.
On mobile, use an accessible compact menu when these items do not fit.

Within Administration, use a compact vertical section rail on desktop and a
usable section selector on mobile. Group Governance, Organization, and
Operations visually while retaining the existing section behavior.

Provide shareable navigation state and working browser back/forward. Inspect
the Go static-serving behavior before choosing path routes. Use a compatible
hash strategy or implement and test a tightly scoped SPA fallback; do not turn
unknown API or asset paths into index.html responses.

Each screen uses one consistent page-header pattern: breadcrumb when useful,
title, concise description, contextual actions. Dense views get wide content;
forms get a focused reading width.

### 6. Servers home

Make Servers useful immediately after sign-in:
1. Page heading and refresh action with real refresh state.
2. Compact namespace-scoped summary: total servers, ready servers, tools, and
   tools with known drift. Define the drift categories explicitly from the API.
3. Server search, namespace selector, and All / Ready / Needs attention filters.
4. Responsive server cards and a compact list presentation.
5. Tool catalog within the same page, with its own clearly scoped filters.
6. Server/tool inspector preserving list position and active filters.

Summary counts must state whether they describe the namespace or filtered
results. Avoid a healthy-looking zero while loading or on failure. Unknown
readiness is not equivalent to offline. Kubernetes readiness is not proof of
end-to-end endpoint availability.

Server cards show name, namespace, readiness, replica count, description,
tool count, and endpoint when present. Use supported image, age, and auth-mode
metadata where helpful. Truncate long endpoint display with a working copy
control; preserve access to the full value.

Use explicit View details / Show tools actions. Define search behavior:
server search matches server metadata; tool search matches tool metadata.
If one combined search is chosen, both visible servers and tools must follow
the same documented semantics. Never label a control "Search servers" when
it only filters tool rows.

Show selected filters as removable chips, a result count, and Clear filters.
Reset dependent selections when namespace/status changes invalidate them.

Server inspection shows identity, description, actual status, replica readiness,
endpoint, auth mode, image, age, and related tools where available. Related
activity/access links must use existing authorized API data. Omit unsupported
actions. A missing deploy or retire API is not permission to invent one.

### 7. Tool catalog and inspector

Keep semantic sortable columns for Tool, Server, Trust, Side effect, Risk, Drift.
Preserve ordinal risk/trust sorting. Make descriptions secondary, clamp long
rows, and expose full descriptions in the inspector.

Add scoped search, risk/drift filters, result counts, pagination for long lists,
and a clear selected state. Preserve sorting/filter state during inspection.

Use a desktop side sheet or split inspector and a full-screen mobile detail
view. Include tool identity, description, server, namespace, declared/live
state, labels, endpoint, and exact governance metadata. Explain jargon with
brief help text verified against the policy code. Do not present declared
risk as an independently computed assessment.

Every overlay needs a clear title, predictable close control, Escape behavior,
correct focus handling, scroll containment, and restoration of focus to its
trigger. Choose modal versus nonmodal semantics deliberately.

### 8. Authentication

Redesign the signed-out Servers view as a polished access state with a short
explanation of server discovery, tool governance, and runtime visibility.
Use one clear sign-in action in the content plus the normal header action.
Do not display fake server counts or imply private data is public.

Give email/password and API-key authentication clearly separated modes. Use
persistent field labels, autocomplete, inline errors, disabled/busy submit,
and predictable focus. Successful sign-in enters Servers.

Check configured Google/OIDC behavior before removing any legacy dependency.
If used, migrate it to the same styled authentication flow and preserve its
server verification. Do not remove an authentication method merely because
it was only exposed in the older dashboard.

### 9. Activity and usage analytics

Redesign tenant Activity and admin Analytics with common metric, filter, and
table components while preserving their different authorization and scope.

Provide supported server/time/row-limit controls and real refresh feedback.
Summaries show available request, allow, deny, server, and actor totals with
explicit scope. Use an accessible proportion bar or ranked comparison for
aggregate data. Add a time-series chart only when the API provides timestamped
buckets; never fabricate a trend from totals.

Distinguish no traffic from unavailable analytics. Independent team membership
content should remain usable if usage loading fails. Keep filter options stable
when a filtered response no longer contains the full original server list.

Redesign membership cards with team name, namespace, and role hierarchy.

### 10. API keys

Use a clear page heading, create action, and compact table of name, prefix,
created time, status, and revoke action. Put creation in a focused form.

Make the one-time secret state unmistakable, keyboard accessible, and easy to
copy. Report clipboard failure honestly and leave the value selectable. Raw
key material must never enter URLs, local/session storage, query caches, logs,
or persistent fixtures. Clear it according to the existing navigation/dismissal
contract. Confirm revocation using the selected key's name and consequences.

### 11. Access control

Use clear Grants and Agent sessions sections or local tabs with consistent
summary counts and filters. Keep the exact underlying policy semantics.

Grant detail: identity, namespace, server, subject, trust ceiling, allowed side
effects, status, and associated decisions.
Session detail: identity, namespace, server, subject, trust, expiry, revocation,
and associated decisions. An expired session must not be labeled active merely
because revoked=false; represent unknown or invalid timestamps safely.

Redesign create forms with grouped fields, accessible validation, and a stable
footer. Use verified catalog choices where authorized instead of forcing users
to memorize server/namespace values. Expose only fields the API accepts.
Make submitted defaults visible, especially namespace and allowed side effects.

Replace window.confirm flows with shared accessible confirmation dialogs.
Preserve pending/error/success behavior and prevent duplicate submissions.
Capture event.currentTarget before awaiting if it is needed after submission.

### 12. Teams and membership

Redesign the teams list and selected-team members as one coherent flow.
Use explicit team selection, clear role badges, copyable technical IDs, and
row-level member actions. Team creation and member addition use focused forms.

During team changes, avoid showing the previous team's members under the new
team name. Handle loading, fetch failure, and stale responses independently.
Distinguish creating a user from inviting an existing user; label what the
current API actually does. Preserve existing ownership and removal rules.

### 13. Operations, audit, and image activity

Replace the single endless stack of tables with local sections/tabs for Users,
Audit trail, and Image activity. Keep filters close to the data they affect.
Use user and date filters only when supported by the API.

Add pagination, readable timestamps with absolute-time access, clear decision
badges, wrapped/clamped resource identifiers, and an event inspector for details.
Client pagination must describe loaded results honestly; do not imply the client
has the complete audit history if the server returned a limited window.

### 14. Platform health

Present component identity, status, replicas, resource, and actionable message
in a scannable component list or grid. Health summaries use fetched data and
retain a clear unknown/loading/error state.

Place individual restart actions with their components. Put Restart all in a
visibly separated disruptive-actions area with an explicit confirmation.
Refreshing health is a separate safe action. Preserve existing observability
link behavior and deployment-mode restrictions for Grafana/Prometheus.

### 15. Components and implementation boundaries

Inspect and evolve:
- src/App.tsx and components/AppShell.tsx
- components/AccountBar.tsx and WorkspaceNavigation.tsx
- components/SignInPanel.tsx
- components/servers/*
- components/user/*
- components/admin/*
- hooks/useCatalog.ts and hooks/useAdminData.ts
- api/types.ts, client.ts, catalog.ts, admin.ts, userWorkflows.ts
- styles/global.css, admin-workflows.css, user-workflows.css

Create only shared primitives used by real screens: Button, IconButton,
StatusBadge, Field, PageHeader, MetricCard, FilterBar, CopyButton, Tabs,
DataTable/Pagination, DetailSheet, ConfirmDialog, Skeleton, EmptyState,
ErrorState, and action feedback.

Keep React, Vite, TypeScript, TanStack Query, and the installed TanStack Table
version. Verify library APIs before adapting examples. Keep semantic CSS unless
a specific dependency earns its cost. Use proven accessible primitives for
complex interactions where helpful, themed with the product tokens. A move to
Next.js or a wholesale CSS-framework migration is not part of this task.

Keep data fetching and mutations out of presentation primitives. Preserve
namespace-aware query keys, cache clearing on identity change, CSRF protection,
HttpOnly sessions, and server-held upstream credentials. UI hiding is not an
authorization boundary.

Inventory legacy-only capabilities before removing its route or assets.
Remove More workspaces from normal navigation immediately, migrate required
flows into the new interface, and retire assets only after parity is verified.
Do not expose migration internals in ordinary user-facing copy.

### 16. Responsive behavior and verification

Test at 1440px, 1024px, 768px, and 390px in both themes. On mobile, collapse
secondary filters, preserve essential row information, use full-screen detail
views, and keep forms usable with the software keyboard. Only wide tables may
scroll horizontally inside named bounded regions. Do not use overflow-x:hidden
to conceal broken layout.

Run the existing baseline tests/build, then meaningful regression tests for
new behavior: navigation, authorization gates, filter dependencies, pagination,
drawer focus/closing, confirmations, stale responses, secret copying, and form
errors. Preserve existing security assertions.

Use the local browser to validate signed-out, tenant, admin, and no-user-identity
sessions. Exercise successful and failing requests, refresh, back/forward,
logout, and expired sessions. Use temporary local QA entities for mutations;
never restart unrelated workloads merely to test styling.

Verify candidate build identity, real UI-to-API requests, console output,
keyboard behavior, accessibility, light/dark contrast, and mobile overflow.
Use mock fixtures only for controlled edge cases and label that evidence.
Capture and inspect screenshots of every redesigned screen. Fix visual defects
found in the browser before calling the work complete.

Run npm test and npm run build in services/ui/frontend and relevant Go UI
tests. Regenerate the embedded static assets through the build. Report bundle
changes. Update the migration/design docs to reflect actual behavior.

### 17. Completion criteria

Every current platform screen uses the new design system. Servers remains the
home view; tools stay within it; More workspaces and nested-dashboard navigation
are gone. All authorized workflows remain reachable and functional. Data,
permission, error, and empty states are truthful. Both themes and mobile views
are visually reviewed.

Provide the worktree/branch, local preview URL and how to run it, implemented
screen list, representative screenshots, test/build results, and any verified
remaining gaps. Explicitly distinguish completed redesign work from backend
capabilities that would require a separate feature. Do not claim deployment or
live validation that did not happen.

## Legacy-to-React migration supplement

This section is mandatory. Do not remove the legacy iframe or its embedded
assets until this inventory has been checked against the current source and
each row has an accepted React replacement. The old dashboard is not merely a
visual reference: it contains product behavior that must be preserved or
explicitly retired with a product decision.

### Legacy surface inventory

The legacy implementation is `services/ui/frontend/public/legacy/index.html`
and `services/ui/frontend/public/legacy/app.js` (also copied into
`services/ui/static/legacy/`). The JavaScript is a 4,610-line imperative
dashboard with role-gated tabs, auto-refresh, scoped inventory, connection
configuration, observability links, activity drill-downs, and mutations.

| Legacy surface or behavior | React destination | Current React state | Migration requirement |
| --- | --- | --- | --- |
| Servers catalog | `ServersWorkspace` home | Partially migrated | Keep as home; add missing inventory and server actions below. |
| Namespace/scope selector | App shell + Servers scope control | Partially migrated | Preserve catalog/all/user/team/shared/public scope semantics; never call it an environment switcher. |
| Server status/search filters | Servers filter bar | Migrated, needs UX refinement | Search server metadata and expose result count/chips. |
| Live inventory polling and merge | Server inspector/catalog API model | Missing in React types/UI | Migrate tools, prompts, resources, tasks, declared/live/ungoverned/missing drift, and `liveInventoryError`. |
| Server card selection and detail | Server detail sheet | Partially migrated | Replace inline appended detail with a deep-linkable sheet/full-screen mobile detail. |
| Server endpoint and copy URL | Server detail | Partial | Add reliable copy feedback, selectable fallback, and truthful missing-endpoint state. |
| Connect config | Server detail > Connect | Missing in React | Port Claude Desktop, Cursor, VS Code, and Raw JSON tabs from `renderServerConnectConfig`; preserve the exact generated JSON and copy actions. |
| Server labels/image/auth mode/age | Server detail metadata | Incomplete | Add all API-backed fields; use code styling for image, endpoint, and identifiers. |
| Server recent events | Server detail Activity section | Missing in React | Use `GET /runtime/server-events` with server/namespace scope; show analytics unavailable separately from no events. |
| Scoped Grafana link | Server detail Observability action | Missing in React | Use API-provided `observability.grafana` only when available; preserve direct-admin restrictions and reason copy. |
| Scoped Prometheus links/queries | Server detail Observability action | Missing in React | Present returned query names/descriptions and open the authorized URL; do not create arbitrary query inputs. |
| Retire server | Server detail destructive action | Backend exists, React route missing | Add only where the principal can use it; expose through the session BFF with CSRF, confirmation, pending, success, forbidden, and failure states. |
| Publish policy/quota | Servers summary/action area | Missing in React | Display only returned `publish_policy`; distinguish quota disabled, available, and exhausted. Do not invent a publish button if apply/deploy UX is not in scope. |
| Tool catalog | Servers tool section | Migrated, needs parity | Preserve trust, side effect, risk, drift, labels, declared/live values, sorting, and detail. |
| Tool risk and metadata filtering | Servers tool filters | Partial | Add drift and metadata filters only when backed by loaded fields; keep dependent selection stable. |
| Public catalog signed-out mode | Servers auth/scope state | Missing/unclear in React | Respect `PLATFORM_MODE=public`: allow only the server/catalog GET behavior intended by the backend, and never show private analytics or mutation controls. |
| User dashboard server summary | Activity | Partially migrated | Move server list and summary metrics into Activity, not a separate workspace. Keep server links and supported observability actions. |
| User usage analytics | Activity | Partially migrated | Preserve server/tool/recent tables and add backend `series` chart data; retain 1/7/30/90-day and server filters. |
| Auto-refresh toggle | Activity/Admin analytics | Missing in React | Add only where useful, default off for expensive analytics, show last refreshed time, pause when hidden, and never refresh the full Servers DOM. |
| Admin usage summary | Administration > Analytics | Partially migrated | Preserve totals, servers, actors, tools, decisions, `series`, `recent`, window, and filters. Client types currently omit `series` and `recent`; fix that. |
| Analytics decision meter | Administration > Analytics | Missing in React | Use an accessible labeled allow/deny proportion, not color alone; show unavailable state when ClickHouse/analytics fails. |
| Analytics server/actor/tool/decision tables | Administration > Analytics local tabs | Partially migrated | Keep all four datasets, add scope/window/filter controls supported by the API, pagination or honest limit copy, and row detail where useful. |
| Gateway events table | Administration > Operations/Audit | Missing in React | Migrate recent policy decisions with human/agent, target, decision, policy reason, grant/session links, and event detail. |
| Grant creation | Administration > Access | Migrated but incomplete | Restore agent subject, namespace, server namespace, policy version, allowed side effects, and tool-rule editing. Never silently force only `read` if the API supports more. |
| Grant enable/disable/delete | Administration > Access | Migrated partially | Replace `window.confirm` with accessible confirmation; preserve PATCH/DELETE semantics and audit feedback. |
| Grant activity detail | Access detail sheet | Migrated as page | Make it a contextual sheet/deep link with last-seven-days filtering and clear analytics unavailable state. |
| Session creation | Administration > Access | Migrated but incomplete | Restore agent/team subjects, server namespace, policy version, trust, expiry, UTC hint, and validation. |
| Session revoke/unrevoke/delete | Administration > Access | Migrated partially | Show expiry-aware status, use accessible destructive confirmation, and preserve PATCH/DELETE semantics. |
| Session timeline | Access detail sheet | Migrated as page | Preserve namespace matching, tool/RPC name, decision, reason, and full timeline state. |
| Team list and selected team | Administration > Teams | Migrated | Redesign as list/detail; preserve selected-team state through refresh and deep links. |
| Team member list | Team detail | Migrated | Preserve role, member identity, created time, and stale-response protection. |
| Create team | Administration > Teams | Migrated | Use a focused sheet with validation, dirty-close behavior, pending/error/success states. |
| Create team user | Administration > Teams | Migrated but misleading | Label this as creating a user, not inviting one, unless the backend is changed. Never imply an invitation email is sent. |
| Change role/remove member | Team detail | Migrated partially | Use confirmation for removal, show owner/member semantics, and guard against stale selected-team responses. |
| API key list/create/revoke | API keys | Migrated | Improve the one-time secret notice, clipboard fallback, revoke confirmation, and identity-gated copy. |
| Operations user directory | Administration > Operations | Migrated but dense | Make Users, Audit, Image activity local tabs with filters and detail sheets. |
| User detail | Operations user detail | Missing in current React | Port role, namespace, IDs, login/activity/failure counts, and filtered audit activity. |
| Audit filters | Administration > Operations | Incomplete | Port user, since, until, and limit parameters already supported by `readOperations`; display the loaded-window boundary honestly. |
| Image/deployment activity | Operations > Image activity | Migrated | Preserve image ref, deployment target, server, source, action, status, and detail. Do not add deployment controls without a verified mutation API. |
| MCP server operations inventory | Operations | Missing as a distinct local section | Keep fleet server health and selected-server inspector associated with Operations where admin scope is required. |
| Platform component health | Administration > Platform | Migrated | Add per-component restart beside component identity; keep restart-all isolated and explicitly disruptive. |
| Refresh components | Platform | Migrated | Add refresh busy/last-updated state without replacing healthy rows with a page spinner. |
| Grafana/Prometheus platform links | Platform | Migrated | Preserve `/grafana` and `/prometheus` behavior and deployment restrictions. |
| Admin team detail | Team detail sheet | Missing in current React | Port team namespace, ID, created timestamp, member table, back behavior, and direct navigation. |
| Admin user detail | User detail sheet | Missing in current React | Port user header metadata and audit activity, including back context to Teams or Operations. |
| Google sign-in | Styled SignInPanel | Not confirmed in React | Check `MCP_GOOGLE_CLIENT_ID`; port the Google Identity Services callback into the React auth state if configured. |
| Toasts/inline errors/copy feedback | Shared feedback primitives | Inconsistent | Replace legacy `showToast`, inline DOM errors, and swallowed clipboard failures with one accessible system. |
| Role tab visibility | App/navigation guards | Migrated | Keep server-backed gates; hide navigation and also guard direct routes. |
| Legacy tabs and iframe | No React destination | Must retire | Remove `legacy` workspace, `/legacy/index.html`, `public/legacy`, embedded copies, CSP frame allowance, and tests only after parity evidence. |

### Important legacy behaviors that are easy to lose

1. **Inventory is broader than tools.** The runtime probe returns tools,
prompts, and resources, while the server model also carries tasks and declared
policy metadata. The legacy UI merges live inventory with declared inventory:
live-only entries are `ungoverned`, declared-only entries are `missing`, and
matched entries retain policy metadata. React must model this explicitly rather
than reducing everything to `tools?: { name }[]`.

2. **Scope is a security and comprehension concept.** Legacy scope entries can
represent a catalog, a shared namespace, a public preview, a user namespace,
team namespaces, or an admin fleet. Preserve those distinctions in labels and
request parameters. Do not replace them with a fake “workspace” picker.

3. **Analytics has independent datasets.** The analytics response includes
`totals`, `servers`, `actors`, `tools`, `decisions`, `series`, `recent`,
`window_days`, and `filters`. A partial response must not be presented as zero
data. If the backend returns a failed whole response, show unavailable; if the
API later supports partial sections, render section-level errors.

4. **Server observability is scoped.** Prometheus URLs and Grafana availability
come from authorized runtime responses. Keep the namespace/server parameters
and direct-admin flags; never construct arbitrary monitoring URLs in the
browser.

5. **Legacy has two different auth experiences.** Email/password and API-key
login are alternatives, and Google may be conditionally configured. Keep the
normal React auth state as the single source of truth and clear sensitive input
after successful login. Public catalog mode is a separate anonymous GET-only
case, not an authenticated user state.

6. **Legacy auto-refresh was deliberately selective.** Admin summary/analytics
and governance events refresh periodically, while full server cards do not
because re-rendering them caused focus and expansion flicker. React should use
query invalidation or silent section refreshes and visibly disclose refresh
time.

7. **Connection JSON is a user workflow.** The generated config differs by
client: Claude Desktop/Cursor use `mcpServers`, VS Code uses `servers`, and Raw
JSON exposes the server-provided access blob. Keep copyable code blocks and
client-specific path hints, but never render credentials or secret headers.

### React/API parity work required before legacy removal

Before deleting legacy files, update the source contracts as needed:

- Extend `ServerSummary` with live inventory, prompts, resources, tasks,
  labels, access JSON, observability, and live-inventory error fields.
- Extend `UsageResponse` with `series`, `recent`, and complete filter fields;
  add typed `TimePoint` and `RecentActivity` records.
- Add catalog/admin/user API functions for server events, observability links,
  server retirement, deployment inventory where read-only, and the exact
  legacy-supported filters. Keep every route in the Go session-proxy allowlist
  before calling it from React.
- Add safe write routes for server retirement only if product scope approves it;
  add CSRF tests and upstream authorization tests. Do not call a direct `/api/v1`
  mutation from browser JavaScript.
- Replace `window.confirm` with `ConfirmDialog`/`AlertDialog`; destructive
  confirmations must name the exact resource and consequence.
- Replace `event.currentTarget` reads after awaited mutations with values captured
  before the await or controlled form state. Avoid unmounted-form DOM access.
- Ensure expired sessions invalidate all queries and return the user to Servers,
  not to a blank or stale admin page.
- Add route/state persistence for selected namespace, selected server, selected
  tool, admin section, and detail context. Back should close a sheet before
  leaving the underlying page.

### Legacy retirement gate

The implementation agent must produce a parity checklist with one row per
legacy tab and one row per legacy action. For each row attach:

- React route/component and API function;
- signed-out, tenant, admin, no-user-identity, unauthorized, empty, and error
  behavior where relevant;
- desktop and 390px screenshot;
- browser evidence of the request URL, status, and console state;
- keyboard/focus evidence for dialogs, sheets, menus, tables, and copy controls;
- mutation evidence with CSRF and authorization checks where relevant.

Only after all rows are accepted may the agent:

1. remove `legacy` from `WorkspaceId`, navigation, and `App.tsx` fallback;
2. remove `LegacyWorkspace.tsx` and `LegacyDashboard.tsx`;
3. remove `services/ui/frontend/public/legacy/*` and regenerate
   `services/ui/static/*` through the Vite build;
4. remove iframe-specific CSP allowances and stale legacy asset tests;
5. update `docs/ui-react-dashboard-migration-plan.md` so its phase claims match
   current code rather than historical migration status.

Do not delete legacy assets as a cleanup shortcut. They are the rollback path
until React has demonstrated behavior parity.

## Reference pack for the implementation agent

Use these direct links for interaction behavior and component study. Borrow
principles and information architecture, not branding or copied source.

- [Vercel dashboard navigation redesign](https://vercel.com/changelog/dashboard-navigation-redesign-rollout) — persistent resource/team context, collapsible navigation, and mobile navigation ideas.
- [Vercel runtime log filtering](https://vercel.com/changelog/redesigned-search-and-filtering-for-runtime-logs) — removable filter pills, data-informed suggestions, and query clarity.
- [Vercel Geist introduction](https://vercel.com/geist/introduction) — catalog of tables, sheets, drawers, status dots, code blocks, copy buttons, pagination, and skeletons.
- [Geist Sheet](https://vercel.com/geist/sheet) — contextual inspection rules, explicit close behavior, focus restoration, and when a sheet should not be used for destructive confirmation.
- [Geist Modal](https://vercel.com/geist/modal) — blocking decision behavior and destructive confirmation semantics.
- [Geist Table](https://vercel.com/geist/table) — dense table hierarchy, sortable controls, placeholders, and tabular data treatment.
- [Geist Copy Button](https://vercel.com/geist/copy-button) — copy interaction feedback for endpoint/config/key workflows.
- [Supabase layout patterns](https://supabase.com/design-system/docs/ui-patterns/layout) — page containers, headers, sections, narrow forms, and wide data regions.
- [Supabase navigation patterns](https://supabase.com/design-system/docs/ui-patterns/navigation) — compact primary/secondary navigation structure.
- [Supabase table patterns](https://supabase.com/design-system/docs/ui-patterns/tables) — choosing semantic tables versus heavier grid behavior.
- [Supabase table component](https://supabase.com/design-system/docs/components/table) — horizontal scroll regions, sortable headers, and row-action ambiguity guidance.
- [Supabase form patterns](https://supabase.com/design-system/docs/ui-patterns/forms) — grouped fields, contextual validation, pending actions, and submission errors.
- [Supabase modality patterns](https://supabase.com/design-system/docs/ui-patterns/modality) — dialog versus sheet, dirty-form dismissal, and destructive-action speed bumps.
- [Supabase filter bar](https://supabase.com/design-system/docs/fragments/filter-bar) — advanced filter conditions and async option patterns; adapt only the complexity users need.
- [Supabase empty states](https://supabase.com/design-system/docs/ui-patterns/empty-states) — initial empty state versus no-results-after-filter distinction.
- [Supabase chart patterns](https://supabase.com/design-system/docs/ui-patterns/charts) — chart loading/empty/error states, timestamped data, tooltips, and accessible chart-plus-table treatment.
- [Linear UI redesign](https://linear.app/now/how-we-redesigned-the-linear-ui) — compact application chrome, hierarchy, alignment, and light/dark surface discipline.
- [Railway metrics](https://docs.railway.com/observability/metrics) — keeping metrics in service context and tying operational data to a selected resource.
- [Render metrics with service events](https://render.com/changelog/in-dashboard-metrics-now-display-service-events) — associating operational events with metric timelines when the data exists.
- [Radix accessibility overview](https://www.radix-ui.com/primitives/docs/overview/accessibility) — accessible interaction primitives and implementor responsibility for names/labels.
- [Radix Dialog](https://www.radix-ui.com/primitives/docs/components/dialog) — focus trapping, Escape, controlled state, title, and description behavior.
- [Lucide React guide](https://lucide.dev/guide/react) — consistent open-source icon usage and selective imports.
- [WAI-ARIA modal dialog pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/) — focus cycle and return-focus requirements.
- [WCAG contrast minimum](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html) — verify text contrast in both themes.
- [WCAG target size minimum](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html) — use the 24px AA minimum as a floor and design more comfortable mobile targets where possible.

The desired synthesis is a calm, high-density infrastructure console: Vercel's
resource context, Supabase's honest data/form states, Linear's quiet alignment,
Railway/Render's service inspection, and MCP Sentinel's navy/teal identity.
Do not turn the platform into a marketing landing page or a generic “AI
dashboard.”
