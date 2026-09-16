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
| Servers | none | `ServersWorkspace` | Ported. **Was a gap - fixed in this PR** (tenant publish-quota stat, below). |
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

## Recommendation

Every tab has an accepted React route with matching role-gating. The three
items marked "explicitly descoped" above are navigation/UX differences or
aggregate-vs-live-feed data differences, not capability loss for any role -
they're a reasonable call to make now rather than block removal on. The
tenant quota stat was the one genuine, if narrow, information loss, and it is
now closed (above).

Given the above, legacy retirement (removing the iframe fallback and
`services/ui/static/legacy/**`) is safe to proceed, provided full
browser/API/accessibility/security evidence is captured against the final
React-only shell before the legacy assets are deleted, per the plan's own
Phase 5 gate.
