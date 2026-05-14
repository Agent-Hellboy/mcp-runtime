---
name: qa-e2e-ui
description: Browser-first real-cluster Sentinel UI/dashboard QA - role-based navigation, auth flows, every tab, forms, filters, destructive actions, rendered data, network/API evidence, console evidence, responsive/accessibility checks, cleanup, static assets, and public-host defenses. Use when Codex is asked to QA UI changes, dashboard regressions, login/admin/tenant flows, browser-visible API behavior, or copyable MCP connect config. Complements qa-e2e-security with feature-correctness and browser interaction checks. Assumes qa-cluster-bringup has run.
---

# QA - E2E UI (live cluster)

## Overview

This skill validates that the **Sentinel dashboard actually works as a user
experience** against the real UI + API + Traefik stack. Curl checks are useful
smoke gates, but they are not a substitute for browser interaction. Use
Playwright and Chrome DevTools MCP whenever available to collect:

- accessibility snapshots for actionable element names and roles
- browser console messages
- network request/response evidence for UI-triggered API calls
- screenshots for visual regressions and responsive layout failures
- keyboard interaction evidence for reachable controls

`services/ui/main_test.go` covers handler-level behavior. This skill covers
browser-visible workflows: role-gated navigation, session transitions, forms,
filters, tables, modals, copy controls, destructive actions, public-host
defenses, and whether rendered UI data matches backend truth.

Header / CSP / lockout / secret-leak checks live in `qa-e2e-security`; do not
duplicate them here unless the symptom is visible in the UI.

## Step 1 - Confirm preconditions

Do not reinstall the platform or run codegen as part of UI QA. The live
cluster is the source of truth.

```bash
kubectl config current-context | grep -qx kind-mcp-runtime \
  || { echo "Run qa-cluster-bringup first"; exit 1; }

curl -fsS -o /dev/null http://localhost:18080/ \
  || { echo "Traefik port-forward not running; run: kubectl port-forward -n traefik svc/traefik 18080:8000"; exit 1; }

./bin/mcp-runtime status
```

Use a unique scratch directory for temporary artifacts:

```bash
QA_TMP="$(mktemp -d)"
trap 'rm -rf "$QA_TMP"' EXIT
```

If the live cluster has unrelated user changes, work with them. Do not retire
or mutate non-temporary objects unless the user explicitly approves.

## Step 2 - Choose audit mode

- **full-ui**: every visible capability and every role. Default for broad UI
  QA requests.
- **git-range** (`BASE=<merge-base>`, default `origin/main`): trim by diff,
  but still run Dashboard Load and at least one login/logout flow.
- **smoke**: only dashboard load, one successful login, one protected action,
  and console/network sanity. Use only when the user asks for a quick check.

Diff guidance:

| Diff touches | Required sub-suites |
|---|---|
| `services/ui/main.go` | Dashboard Load, Auth, UI->API Proxy, affected tabs |
| `services/ui/static/**` | Browser Matrix, Static Assets, Responsive/A11y |
| `services/api/internal/runtimeapi/**` | Catalog, Governance, Operations, API Contract |
| `services/api/internal/platformstore/**` or auth code | Auth, API Keys, Role Gating |
| `config/ingress/**`, `k8s/**` UI ingress | Public-host Defense |
| `docs/**`, `website/**` only | Dashboard Load smoke, then report docs-only scope |

## Step 3 - Browser instrumentation is required

Prefer MCP browser tools in Codex sessions:

1. Use Playwright MCP for navigation, snapshots, clicks, form fill, screenshots,
   viewport resizing, and network request lists.
2. Use Chrome DevTools MCP for a second console/network pass when available,
   especially when investigating 401/403, redirects, cookie/session behavior,
   or copy/clipboard failures.
3. Treat browser console errors and failed network requests as findings unless
   they are explained by an intentional negative test.

If no browser automation is available, mark browser checks as **blocked** and
run only curl/API smoke checks. Do not report a full UI pass without browser
evidence.

Capture the following evidence per role:

```text
role=<signed-out|tenant-user|admin>
selected_tab=<tab>
console_errors=<count and messages>
failed_network=<method url status/error>
visible_assertions=<snapshot text or screenshot path>
```

## Step 4 - Required UI feature matrix

The final report must include a matrix with this exact shape:

```text
Page | Feature | Field/control | Action | Expected result | Actual result | Network/API evidence | Console evidence | Status
```

Use one row per meaningful capability. Do not collapse "Governance works" into
one row; forms, filters, enable/disable, revoke/unrevoke, empty states, and
error states are separate capabilities.

Minimum pages:

- Global App Shell
- Authentication
- Server Catalog
- My Activity
- API Keys
- Admin Dashboard
- Governance
- Operations
- Platform
- Responsive / Accessibility
- Public-host Defense, when applicable

## Step 5 - Role and session flows

Run these browser flows in order. Use the test-mode credentials only in local
Kind test mode.

Signed out:

- Header shows product title and Sign In.
- Catalog is visible.
- Protected tabs/actions open auth modal and do not switch to protected data.
- Namespace selector behavior matches platform mode.
- No Google button appears when `MCP_GOOGLE_CLIENT_ID` is empty.

Tenant user (`test@mcpruntime.org` / `test@123`):

- Visible tabs: Server Catalog, My Activity, API Keys, Governance.
- Hidden tabs: Dashboard, Operations, Platform.
- Logout resets active tab and clears protected data.
- A 401 from any UI API call resets auth state or shows a clear unauthorized
  state without leaving mixed signed-in/signed-out controls.

Admin (`admin@mcpruntime.org` / `admin@123`):

- Visible tabs: Server Catalog, API Keys, Dashboard, Governance, Operations,
  Platform.
- Hidden tab fallback works after logout.
- Auto-refresh starts only for admin dashboard and stops on logout.
- Grafana and Prometheus header links appear only for admin in dev path mode.

API-key login:

- Retrieve the UI key only when needed and fail fast if it is missing:

```bash
UI_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel -o jsonpath='{.data.UI_API_KEY}' | base64 -d)"
test -n "$UI_KEY" || { echo "FAIL: UI_API_KEY is empty"; exit 1; }
```

- Valid UI API key logs in as admin and clears the key input after success.
- Invalid API key returns a visible error and does not clear the field until
  the user edits it or retries.

Negative auth cases:

- blank form
- email without password
- password without email
- invalid password
- backend unavailable or network failure, if safely reproducible

## Step 6 - Server Catalog coverage

Drive this from the browser, then cross-check with CLI/Kubernetes.

Namespace selector:

- signed-out tenant mode
- tenant aggregate catalog
- user namespace
- team namespace
- shared/org namespace
- admin namespace list
- public preview namespace in public mode, if configured

Catalog data and filters:

- total servers, ready servers, tool count, publish quota
- status filters: All, Ready, Issues
- search by server name, namespace, description, endpoint, status
- search by tools, prompts, resources, tasks
- search by inventory labels and server labels
- no-match state

Server cards:

- title, namespace, description, status, ready pods, endpoint
- tool/prompt/resource/task counts
- created date and labels
- inventory details for string and object forms
- access JSON / connect config
- long names/descriptions/labels do not break layout

Actions:

- Details opens the detail panel and loads recent activity.
- Copy URL and Copy JSON show success or fallback/error feedback.
- Card Enter/Space selection works where the card is keyboard focusable.
- Retire is tested only against a temporary `qa-audit-*` MCPServer.

Retire flow:

- Create or use only a `qa-audit-*` server.
- Cancel leaves the server unchanged.
- Confirm removes the server.
- Detail panel resets after removal.
- Tenant My Activity refreshes after tenant-owned retire, if tested.

Cross-check:

```bash
./bin/mcp-runtime server list
kubectl get mcpservers -A
```

Any UI/CLI count mismatch is a finding unless the UI label clearly describes a
different metric, such as historical active servers.

## Step 7 - My Activity coverage

Tenant user only:

- Metrics: My Servers, Ready, Requests, Denied, Deny Rate.
- Deployed Servers table: identity, namespace, status, inventory, endpoint,
  Analytics action, Copy URL, tenant Retire.
- Usage controls: All servers, per-server selector, 24h/7d/30d/90d, Refresh.
- Usage tables: per-server usage, top tools, recent activity.
- Empty state: no personal servers.
- Shared catalog servers are excluded from personal count.
- Analytics empty response and API error render clear states.
- If a selected server disappears, selector and tables recover cleanly.

## Step 8 - API Keys coverage

Table:

- name, prefix, created timestamp, active/revoked state, revoke action.

Create:

- empty name validation produces visible feedback or native validation.
- normal name
- long name
- special characters
- duplicate-looking name
- one-time key display
- input clearing after success
- list refresh preserves or intentionally clears the one-time key; record the
  expected behavior from implementation.
- automatic one-time key timeout, if implemented.

Revoke:

- modal cancel leaves row active.
- modal confirm revokes row.
- error toast or visible error on failed revoke.

Cleanup:

- Revoke all temporary `qa-audit-*` keys before finishing.

## Step 9 - Admin Dashboard coverage

Admin only:

- Summary metrics: Total Events, Active Servers, Active Grants, Active Sessions.
- Usage metrics: Events, Allowed, Denied, Humans, Agents, Allow Rate, Deny Rate.
- Decision meter widths match allow/deny rates.
- Row limit controls: Top 10, Top 25, Top 50.
- Analytics tables: MCP Servers, Users & Agents, Tools, Decisions.
- Decision Audit table: time/source/type, human, agent, server/namespace/action,
  decision badge, policy reason/trust/session.
- Auto-refresh enabled starts polling; disabled stops polling; logout stops it.
- Non-admin never starts dashboard polling.
- Empty/zero, allow-only, deny-only, mixed totals, malformed timestamps, and API
  errors render usable states when safely reproducible.

Cross-check dashboard current inventory metrics against:

```bash
./bin/mcp-runtime server list
./bin/mcp-runtime access grant list --namespace mcp-servers
./bin/mcp-runtime access session list --namespace mcp-servers
```

If dashboard metrics are historical rather than current, the UI label must make
that clear.

## Step 10 - Governance coverage

Summary:

- Active Grants, Disabled Grants, Active Sessions, Revoked Sessions.

Grant list and filters:

- name, namespace, server reference, server namespace fallback
- human, agent, team, and combined subjects
- max trust, allowed side effects, active/disabled state
- filter by grant name, server, human ID, agent ID, team ID, no-match state

Grant form validation:

- missing name
- missing server
- no subject
- no side effects
- valid `tool:allow`
- valid `tool:deny`
- valid `tool:allow:medium`
- valid tool name containing `:`
- invalid decision
- invalid trust
- malformed line

Grant actions:

- create `qa-audit-*`
- refresh
- disable cancel/confirm
- enable cancel/confirm
- API error feedback
- policy render cross-check:

```bash
./bin/mcp-runtime server policy inspect <server> --namespace <namespace>
```

Session form validation:

- missing name
- missing server
- no subject
- valid local datetime converted to UTC
- invalid datetime
- empty expiry allowed

Session actions:

- create `qa-audit-*`
- refresh
- revoke cancel/confirm
- unrevoke cancel/confirm
- API error feedback
- policy/session cross-check:

```bash
./bin/mcp-runtime access session get <name> --namespace <namespace>
kubectl get mcpagentsession <name> -n <namespace> -o yaml
```

## Step 11 - Operations coverage

Admin only:

- Summary metrics: MCP Servers, Ready, Issues, Recent Logins, Users, Images,
  Deployments, MCP Events.
- Filters: user/tenant, since datetime, until datetime, Apply, Clear, invalid
  datetime behavior.
- Logged-in Users table.
- MCP Server Health table.
- Activity Timeline table.
- MCP Activity table.
- Images and Deployments table, including long image refs.
- MCP Server Inspector empty and selected states.

Cross-check Operations server counts with:

```bash
./bin/mcp-runtime server list
kubectl get mcpservers -A
```

Missing namespaces or stale server counts are product findings.

## Step 12 - Platform coverage

Admin only:

- Platform Health component cards: display name, status, ready count, namespace,
  resource/key, message.
- Styling for Ready, Degraded, NotReady, unknown.
- Fleet Health: total servers, ready servers, issues, server table.
- Fleet empty/error states, if safely reproducible.
- Component restart selector includes every restartable component documented by
  the UI. If health shows Grafana/Prometheus but restart omits them, either the
  UI copy must explain why or it is a finding.
- Restart no-selection validation must show visible feedback.
- Restart modal cancel works.
- Confirm restart only for an explicitly safe temporary target or skip with a
  documented reason.
- Open Operations activates Operations and triggers data load.

## Step 13 - Responsive and accessibility coverage

Run at least:

- desktop: 1200x900
- mobile: 390x844

Check:

- no body-level horizontal overflow unless intentional and documented
- tab bar scroll behavior on mobile
- text does not overlap controls or table cells
- modals fit viewport and remain keyboard usable
- buttons, inputs, selects, tabs, and dialogs have accessible names/roles
- Enter/Space work on keyboard-reachable actions
- focus does not remain trapped in hidden modals after close

Record accessibility snapshot evidence for each major tab.

## Step 14 - Platform mode and extensibility passes

When the task is a broad UI audit, cover:

- tenant mode against current Kind
- org mode through local UI-server config pass
- public mode through local UI-server config pass

Extensibility cases:

- duplicate or empty namespace list
- user, team, shared/org, and public preview namespaces
- string vs object tools/prompts/resources/tasks
- server labels and inventory labels
- unknown status values
- missing endpoint or missing access JSON
- new component returned by API appears without code changes
- platform JWT, UI API-key, OIDC/Google, and direct API-key clients

If a mode or case cannot be exercised, mark it skipped with a reason and the
command or fixture needed to cover it later.

## Step 15 - Curl smoke and API contract evidence

Use curl to support browser findings, not replace them.

Dashboard load:

```bash
curl -sS -o "$QA_TMP/index.html" -w "%{http_code} %{size_download}\n" \
  http://localhost:18080/
grep -q '<div id="app"' "$QA_TMP/index.html" || echo "FAIL: app root missing"
grep -q 'app.js' "$QA_TMP/index.html" || echo "FAIL: bundle script missing"

curl -sSI http://localhost:18080/static/app.js | tr -d '\r' \
  | grep -qiE '^Content-Type: (text|application)/javascript' \
  || echo "FAIL: app.js content-type"

curl -sS http://localhost:18080/config | jq -e '.' >/dev/null \
  || echo "FAIL: /config not JSON"
curl -sS http://localhost:18080/config | jq -e \
  '[paths(scalars) | join(".")] | map(test("api.?key";"i")) | any | not' \
  >/dev/null || echo "FAIL: /config exposes an apiKey-shaped field"
```

Static assets:

```bash
node --check services/ui/static/app.js
curl -sS -o "$QA_TMP/app.js" http://localhost:18080/static/app.js
node --check "$QA_TMP/app.js"
```

Discover API routes from implementation and browser network traffic. Do not
invent endpoints. If an endpoint path differs from the skill examples, record
the live route in the report.

## Step 16 - Automated regression checks

For broad UI audits or UI/API/CLI changes, run the automated checks that match
the touched surface. Do not run codegen, formatters, setup reinstall, or any
command that rewrites tracked files during the audit.

```bash
(cd services/ui && go test ./... -race -count=1)
node --check services/ui/static/app.js
go test ./internal/cli/... ./cmd/mcp-runtime/... -count=1
go test ./test/golden/... -count=1
E2E_CACHE_MODE=1 \
  E2E_SCENARIOS=smoke-auth,governance \
  CLUSTER_NAME=mcp-runtime \
  E2E_KEEP_CLUSTER=1 \
  bash test/e2e/kind.sh
```

If a check is unsafe or too expensive for the requested scope, skip it with a
specific reason. For example, skip Kind e2e when the live contributor cluster is
busy with unrelated user work or when the user requested a read-only audit.

## Step 17 - Public-host defense

In test mode `MCP_PLATFORM_DOMAIN` is usually unset and `/grafana` +
`/prometheus` route through the dev path-based gateway. The invariant under
test: **when** `MCP_PLATFORM_DOMAIN` is set, the public platform ingress must
not expose Grafana or Prometheus.

```bash
kubectl get ingress -n mcp-sentinel -o yaml \
  | yq '.items[] | select(.metadata.name=="mcp-sentinel-platform-ui") | .spec.rules' 2>/dev/null \
  || kubectl get ingress -n mcp-sentinel mcp-sentinel-platform-ui -o yaml \
       | grep -E 'path:|grafana|prometheus'
```

If `mcp-sentinel-platform-ui` does not exist, mark this sub-suite **N/A in
test mode** and recommend a `MCP_PLATFORM_DOMAIN=*` rerun.

## Step 18 - Safe mutation and cleanup

Only mutate temporary resources:

- `qa-audit-*` MCPServers
- `qa-audit-*` grants
- `qa-audit-*` sessions
- `qa-audit-*` API keys

Required cleanup evidence:

```bash
kubectl get mcpservers,mcpaccessgrants,mcpagentsessions -A | grep qa-audit || true
./bin/mcp-runtime server list
```

Also confirm:

- no active `qa-audit-*` API keys remain
- temporary credentials or cookie files were removed
- temporary port-forwards were stopped
- temporary e2e/example workloads created during the audit were removed

If another command recreates temporary workloads after cleanup, delete them
again and wait until pods/deployments/services/ingresses disappear before
finishing.

## Step 19 - Report

For UI QA, use this finding shape instead of the security template unless the
finding is actually security-sensitive:

```text
[SEV-{High|Medium|Low|Info}] <short title>
UI path: <tab / modal / workflow>
Affected API/CLI path: <endpoint or command, if any>
User impact: <what the user cannot do or might misunderstand>
Repro steps:
  1. <browser action>
  2. <browser action>
Expected:
Actual:
Network/API evidence:
Console evidence:
Likely implementation area: <services/ui/static/app.js | services/ui/main.go | services/api/...>
Recommended regression test:
Status: Open
```

Final report sections:

1. Summary: scope, commit SHA, cluster context, browser tools used, counts by
   status/severity.
2. UI feature matrix.
3. Severity-ranked findings.
4. CLI/Kubernetes cross-checks and mismatches.
5. Checks run and checks skipped.
6. Cleanup evidence.
7. Fix plan grouped by data correctness, missing UI coverage, role/auth
   behavior, form validation, destructive-action safety,
   responsive/accessibility, and automated regression tests.

Cross-link to `qa-e2e-security` for header/lockout findings, to
`qa-e2e-operations` for rollout/ingress findings, and to `qa-e2e-perf` when a
UI complaint is really a latency complaint.
