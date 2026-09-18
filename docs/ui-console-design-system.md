# MCP Sentinel console: design system and structure

This documents what the platform console actually does after the
`ui/servers_console_redesign` pass. It is a description of implemented
behaviour, not a plan.

Source: `services/ui/frontend/src`.

## Layout

```
src/
  styles/     tokens.css, base.css, components.css, app.css (imported by index.css)
  ui/         shared primitives: Button, Icon, Badge, Field, PageHeader,
              MetricCard, FilterBar, CopyButton, Tabs, DataTable, DetailSheet,
              ConfirmDialog, States (Empty/Error/Loading/Skeleton), ProportionBar
  routing/    hash route parsing and the useHashRoute hook
  lib/        formatting helpers (timestamps, ages, expiry, percentages)
  components/ AppShell, WorkspaceNavigation, SignInPanel, GoogleSignInButton,
              servers/, user/, usage/, admin/
```

Data fetching stays in `hooks/` and `api/`; nothing in `ui/` performs a request
or reads the principal.

## Tokens

All repeated visual decisions are semantic tokens in `styles/tokens.css`, with a
dark default and a `[data-theme="light"]` override. Components never reference a
raw hex value.

The palette keeps the navy / teal / blue identity. The starting values in the
research brief were adjusted after measuring rendered contrast in Chromium with
axe-core; the shipped values below are the measured ones.

| Token | Dark | Light |
| --- | --- | --- |
| `--canvas` | `#070b13` | `#eef3f9` |
| `--surface` / `--surface-raised` | `#0f1724` / `#131e2f` | `#ffffff` |
| `--text` | `#f4f7fb` | `#102033` |
| `--text-secondary` | `#a7b6ca` | `#455567` |
| `--accent` (brand teal) | `#7ce7d3` | `#065f56` |
| `--link` | `#93bcff` | `#2757ad` |
| `--success` / `--warning` / `--danger` | `#4ade80` / `#fbbf24` / `#fb8a8a` | `#11603a` / `#845700` / `#a01d14` |

Scale: 4px spacing steps, 6–8px control radii, 10–12px panel radii, 36px default
control height, 28px page titles, 17px section titles, 14px body, 12px metadata.
Space Grotesk for the application, DM Serif Display only for the wordmark and
brand mark, and a system monospace stack for identifiers, endpoints, and images.
Transitions are 150ms and collapse to 1ms under `prefers-reduced-motion`.

Ambient depth (the canvas washes, the dot grid, card gradients) sits behind
content in low-opacity decorative layers. No text contrast depends on it.

## Navigation and routing

Routing is hash-based (`routing/route.ts`). `services/ui/main.go` serves exactly
the files embedded under `static/` and 404s everything else, so path routes would
need an SPA fallback that also swallows unknown API and asset URLs. A hash keeps
deep links and browser back/forward working against the unmodified server.

| Route | Screen |
| --- | --- |
| `#/servers` | Servers home (default) |
| `#/servers?server=<ns>/<name>` | Servers home with the server inspector open |
| `#/servers?tool=<ns>/<server>/<tool>` | Servers home with the tool inspector open |
| `#/activity` | Tenant activity |
| `#/keys` | Personal API keys |
| `#/access` | Access control (any authenticated principal) |
| `#/admin/<section>` | teams, operations, platform, analytics |
| `#/signin` | Sign-in |

Primary navigation is Servers, Access control, Activity, API keys,
Administration, filtered by the same principal gates as before
(`components/WorkspaceNavigation.tsx`). Access control is deliberately not an
admin section: the backend serves `/runtime/grants` and `/runtime/sessions`
through plain `auth()`, not `adminOnly()`, so any authenticated principal sees
its own scope. A non-admin is defaulted to their first visible namespace,
because an empty namespace 403s for them.
Hiding a tab is presentation only: every panel re-checks the principal, and the
backend enforces it again. A deep link into a workspace the principal cannot use
is replaced with `#/servers`.

Administration uses a grouped rail (Organization / Platform) on desktop and a
section select below 900px.

## Screens

- **Servers** — namespace-scoped summary (servers, ready, tools, tools with
  drift, and the publish quota for a tenant principal), server search over
  server metadata, namespace and status filters, server cards, and the tool
  catalog with its own scoped search, risk and drift filters, sorting, and
  pagination. Cards carry the protocol inventory, connect-config copy,
  owner-scoped observability links, and a retire action behind a confirmation.
  A signed-out visitor to a `PLATFORM_MODE=public` deployment gets the
  read-only public catalog instead of a sign-in wall. Selecting a server or tool opens an
  inspector: docked beside the list on desktop, a modal full-screen view below
  900px.
- **Activity** (tenant) and **Usage analytics** (admin) share the metric,
  table, and ranking components but keep their own scope and authorization.
  Aggregate totals are shown as a ranked proportion comparison; there is no
  time-series chart because the usage API returns totals, not buckets.
- **API keys** — create form, one-time secret notice, key table, and a named
  revoke confirmation.
- **Access control** (top-level, any authenticated principal) — grants and agent sessions with summary counts, search,
  namespace scope, create forms backed by the authorized catalog, and per-record
  enable/disable and revoke behind a confirmation dialog.
- **Teams** — team directory with explicit selection driving a members panel.
- **Operations** — Users / Audit trail / Image activity as local tabs, with user
  and date filters and an event inspector.
- **Platform health** — component grid with per-component restart, Grafana and
  Prometheus links unchanged, and `Restart all` isolated in a disruptive-actions
  area.

## Truthfulness rules the UI follows

- A metric with no value renders `—`, never `0`.
- Kubernetes readiness is labelled as readiness, not as endpoint availability.
- `declared` drift with `live: false` means the live inventory was unavailable,
  and the tool inspector says so.
- A session is only "Active" when it is not revoked *and* its expiry is in the
  future; an unparseable expiry reads "Unknown expiry".
- "Analytics unavailable" is an error state, distinct from a successful read
  with no traffic.
- Operations pagination describes the loaded window, because the API returns at
  most 100 rows per collection.
- Declared risk is labelled as declared, with the server-side derivation rule
  explained rather than presented as an independent assessment.
- A 403 on the gateway decision log reads "Admin access required", not
  "analytics unavailable".

## Security invariants preserved

HttpOnly session cookie, same-origin BFF requests through `/api/ui/v1`, the
server-held upstream credential, the in-memory CSRF token on every unsafe
method, the request allowlists in `api/client.ts`, namespace-aware query keys,
and a full query-cache clear on identity change. One-time key material lives
only in React state for the life of its notice.

## Legacy dashboard

Already retired on `main` (#398): there are no `/legacy` assets, no iframe, and
no "More workspaces" entry. The inventory that gated the removal is in
[`ui-legacy-retirement-inventory.md`](./ui-legacy-retirement-inventory.md).

Google sign-in, which used to live only in that dashboard, is part of the React
sign-in panel. It renders only when the deployment sets `GOOGLE_CLIENT_ID`
(surfaced to the browser as `window.MCP_GOOGLE_CLIENT_ID` by `/config.js`);
with no client ID configured the button is omitted and email/password plus
API-key sign-in remain.

## Local development against a cluster

`npm run dev` proxies `/auth`, `/api/ui/v1`, `/api/v1`, `/config.js`,
`/grafana`, and `/prometheus` to `MCP_DEV_UPSTREAM` (default
`http://localhost:18080`). Point it at a locally run candidate UI service when
the deployed image is older than the source:

```bash
kubectl port-forward -n mcp-sentinel svc/mcp-platform-api  18090:8080
kubectl port-forward -n mcp-sentinel svc/mcp-runtime-api   18094:8084
kubectl port-forward -n mcp-sentinel svc/mcp-analytics-api 18095:8085

UI_KEY=$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d)

cd services/ui
PORT=18082 API_BASE=/api/v1 \
  API_UPSTREAM=http://localhost:18090 \
  RUNTIME_UPSTREAM=http://localhost:18094 \
  ANALYTICS_UPSTREAM=http://localhost:18095 \
  API_KEY="$UI_KEY" API_KEYS="$UI_KEY" ADMIN_API_KEYS="$UI_KEY" go run .

cd frontend && MCP_DEV_UPSTREAM=http://localhost:18082 npm run dev
```

The deployed pod only proxies the Phase 2 catalog paths, so admin, keys, and
analytics reads 404 against it. Run the candidate service for those.
