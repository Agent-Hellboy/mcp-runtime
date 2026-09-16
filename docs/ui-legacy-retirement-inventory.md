# Legacy dashboard retirement inventory

Phase 5 acceptance criterion #1 (`docs/ui-react-dashboard-migration-plan.md`):
map every legacy tab/action to an accepted React route or an explicitly
removed product behavior, before any legacy asset is deleted.

Built 2026-09-16 by comparing `services/ui/static/legacy/index.html` +
`app.js` gating/behavior against the current React app on `main` (post
PR #396), tab by tab, plus a live cluster check for the role-gating claims.

## Method

For each legacy tab: the exact `data-*` visibility gate, its React
`WorkspaceNavigation`/section equivalent, and any admin-only or user-only
sub-panel inside it that the top-level gate doesn't already cover.

## Tabs

| Legacy tab | Gate | React route | Status |
|---|---|---|---|
| Servers | none | `ServersWorkspace` | Ported. **Was a gap - fixed** (tenant publish-quota stat, connect-config copy, protocol inventory, observability links, anonymous public-mode browsing - all below). |
| Activity (`userdashboard`) | `data-user-only` (tenant, not admin) | `ActivityWorkspace` (`visible: isTenantUser`) | Ported, gate matches. |
| Keys (`userkeys`) | `data-auth-required` + `data-user-identity-required` | `ApiKeysWorkspace` (`visible: hasUserIdentity`) | Ported, gate matches. |
| Analytics (`dashboard`) | `data-admin-only` | `AdminWorkspace` → Analytics section (`UsageAnalyticsPanel`) | Ported, gate matches. |
| Teams | `data-admin-only` | `AdminWorkspace` → Teams section (`TeamsPanel`) | Ported. Member management is inline (select a team, manage members below) instead of a separate team-detail route - equivalent capability, different navigation shape. Legacy's separate `#tab-admin-user` detail view has no React equivalent; the Operations users table already shows the same fields (role, logins, failures, keys, last activity) inline, just without a dedicated drill-down page. **Recommend: explicitly descoped**, not a blocking gap. |
| Access Control (`governance`) | `data-auth-required` (any authenticated user) | `AccessWorkspace` (`visible: auth.authenticated`) | **Was a real gap - fixed in PR #396** (previously admin-gated, live-verified now matching legacy for both tenant and admin). One remaining piece: legacy's admin-only "Policy Decisions" live gateway-audit sub-panel (auto-refreshing allow/deny feed with a health card) has no direct React equivalent. `UsageAnalyticsPanel`'s allow/denied totals and per-tool breakdown cover the same underlying decision data in aggregate form, not as a live feed. **Recommend: explicitly descoped** given the data is available elsewhere, unless live-feed framing specifically matters to admins in practice. |
| Operations | `data-admin-only` | `AdminWorkspace` → Operations section (`OperationsPanel`) | Ported, gate matches. |
| Platform (`platform`) | `data-admin-only` | `AdminWorkspace` → Platform section (`PlatformHealthPanel`) | Ported, gate matches (component health, Grafana/Prometheus links). |
| Header Grafana quick-link (`data-admin-only`, in the hero bar, not tab-specific) | admin-only | none | **Gap, low severity**: same destination (`/grafana`) is one extra click away via Platform → Platform Health. **Recommend: explicitly descoped** as a navigation convenience, not a capability loss. |

## Fixed in this PR: tenant server-publish quota

Legacy's Servers tab shows a `count/limit` (or `off`) publish-quota stat for
tenant (non-admin) users, sourced from `GET /runtime/servers`'s
`publish_policy` field
(`services/runtime-api/internal/runtimeapi/servers.go:85,98`,
`active_server_limit_enabled`/`active_server_count`/`active_server_limit`).
The React `listServers()` (`api/catalog.ts`) previously discarded this field -
`ServerSummary` had no `publish_policy`, and `ServersWorkspace`'s stat row had
no quota entry, so a tenant user near or at their active-server limit had no
way to see that in the React shell.

**Fixed here**: `listServers()` now returns `publish_policy` alongside the
server list, `ServerSummary` carries it, and `ServersWorkspace`'s stat row
gets a `formatPublishQuota` entry gated the same way legacy's was - visible
only when authenticated and limit-enabled.

## Fixed in this PR: four more gaps caught by review

A second review pass on this PR (after the removal itself was already
written) found four more places where the React `ServersWorkspace` was
missing capability the legacy Servers tab had - all closed here:

- **Copyable connect config.** Legacy rendered a copy-to-clipboard action for
  the full MCP client config (`access_json`/`connect_config`, already
  returned by `GET /runtime/servers` and `/runtime/tools` -
  `services/runtime-api/internal/runtimeapi/servers.go:715`,
  `tools.go:32`). The React server cards discarded those fields entirely.
  Now a "Copy connect config" action appears on any server card whose
  response includes one.
- **Non-tool protocol inventory.** For servers exposing prompts, resources,
  or tasks, legacy showed those collections merged from the MCPServer's
  declared spec and its live MCP session (`liveInventory`). The React card
  showed only a tool count. Server cards now show merged declared/live
  prompt and resource counts (union by name) plus declared task counts, in
  an expandable "Protocol inventory" detail.
- **Owner-scoped observability.** The backend already computes
  `server.observability` (Grafana/Prometheus links) for any principal who
  can observe that server, not just admins
  (`serverInfoObservableByPrincipal`) - React had no consumer of it at all,
  only `PlatformHealthPanel`'s admin-only global links. Server cards now
  surface the Grafana dashboard link and per-query Prometheus links when the
  backend includes them.
- **Anonymous public-mode browsing.** In a `PLATFORM_MODE=public`
  deployment, legacy let a signed-out visitor browse the catalog anonymously
  (matching `PublicCatalogFallback` in
  `services/runtime-api/internal/runtimeapi/platform_mode.go`). The React
  signed-out state always showed a sign-in prompt regardless of platform
  mode, which combined with deleting the legacy fallback would have made
  public deployments un-browsable while signed out. Added a credential-free
  proxy route (`services/ui/public_catalog_proxy.go`) and a read-only
  `PublicServersPreview` that uses it only when platform mode is public.

A fifth, much smaller finding from the same pass: retiring the server that
was the active "Show tools" filter left that filter pointing at a key that
no longer existed, so the tool catalog looked empty until the user manually
cleared it. The retire handler now clears the filter itself.

## Recommendation

Every tab has an accepted React route with matching role-gating. The three
items marked "explicitly descoped" above are navigation/UX differences or
aggregate-vs-live-feed data differences, not capability loss for any role -
they're a reasonable call to make now rather than block removal on. The
tenant quota stat and the four gaps above were the genuine information/
capability losses, and all are now closed.

Given the above, legacy retirement (removing the iframe fallback and
`services/ui/static/legacy/**`) is safe to proceed, provided full
browser/API/accessibility/security evidence is captured against the final
React-only shell before the legacy assets are deleted, per the plan's own
Phase 5 gate.
