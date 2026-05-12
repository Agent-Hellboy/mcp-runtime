---
name: qa-e2e-ui
description: Real-cluster UI/dashboard QA — dashboard load, login flow (test + admin creds), API proxy on same origin, MCP Servers tab connect config, static asset integrity, and the defense check that Grafana/Prometheus are not publicly exposed. Use when Codex is asked to QA UI changes, dashboard regressions, login or admin flows, or the connect-config returned to the browser. Complements qa-e2e-security (which covers headers, lockout, secret leaks) with feature-correctness checks. Assumes qa-cluster-bringup has run.
---

# QA — E2E UI (live cluster)

## Overview

This skill validates that the **dashboard actually works** from a browser-like
client against the real Sentinel UI + API stack. `services/ui/main_test.go`
covers handler-level unit tests; this skill covers what those cannot:
end-to-end browser flows, real Traefik routing, real session cookies, the
copyable MCP connect config returned by the Servers tab, and the public-host
defense (Grafana/Prometheus not exposed when `MCP_PLATFORM_DOMAIN` is set).

Header / CSP / lockout / secret-leak checks live in `qa-e2e-security`; do not
duplicate them here.

## Step 1 — Confirm precondition

```bash
kubectl config current-context | grep -qx kind-mcp-runtime \
  || { echo "Run qa-cluster-bringup first"; exit 1; }
curl -fsS -o /dev/null http://localhost:18080/ \
  || { echo "Traefik port-forward not running"; exit 1; }
```

## Step 2 — Choose mode

- **head-only**. Full UI matrix.
- **git-range** (`BASE=<merge-base>`, default `origin/main`). Trim by diff:

| Diff touches | Required sub-suites |
|---|---|
| `services/ui/main.go` (handlers, login, proxy) | A. Dashboard load, B. Login, C. UI→API proxy, D. Servers tab |
| `services/ui/static/**` (app.js / index.html / css) | A, E. Static-asset integrity |
| `services/api/main.go` endpoints used by UI (dashboard summary, analytics, events, runtime servers) | C, D, F. API contract |
| `config/ingress/**`, `k8s/**` UI ingress | G. Public-host defense |
| `docs/**`, `website/**` only | Skip |

Always run **A. Dashboard load** as a smoke gate.

## Step 3 — Sub-suite A: Dashboard load

```bash
curl -sS -o /tmp/index.html -w "%{http_code} %{size_download}\n" \
  http://localhost:18080/                              # want 200 + non-trivial size
grep -q '<div id="app"' /tmp/index.html || echo "FAIL: app root missing"
grep -q 'app.js'        /tmp/index.html || echo "FAIL: bundle script missing"

# Static asset reachable, correct content-type.
curl -sSI http://localhost:18080/static/app.js | tr -d '\r' \
  | grep -qiE '^Content-Type: (text|application)/javascript' \
  || echo "FAIL: app.js content-type"

# The /config browser bootstrap must exist and must NOT include any API key.
curl -sS http://localhost:18080/config | jq -e '.' >/dev/null \
  || echo "FAIL: /config not JSON"
curl -sS http://localhost:18080/config | jq -e \
  '[paths(scalars) | join(".")] | map(test("api.?key";"i")) | any | not' \
  >/dev/null || echo "FAIL: /config exposes an apiKey-shaped field"
```

## Step 4 — Sub-suite B: Login (test + admin)

Use `PLATFORM_DEV_LOGIN` creds from `docs/getting-started.md#3-contributor-test-mode-cluster`.
Read the actual login route shape from `services/ui/main.go` before asserting
field names — do not hard-code `/login` body keys without confirming.

```bash
LOGIN_PATH="$(grep -oE 'http\.HandleFunc\("[^"]*login[^"]*"' services/ui/main.go \
  | head -1 | sed -E 's/.*"([^"]*)".*/\1/')"
LOGIN_PATH="${LOGIN_PATH:-/login}"

# Test user.
rm -f /tmp/c_user.txt /tmp/c_admin.txt
USER_CODE="$(curl -sS -o /tmp/user_body.txt -w "%{http_code}" \
  -c /tmp/c_user.txt -H "content-type: application/json" \
  -d '{"email":"test@mcpruntime.org","password":"test@123"}' \
  "http://localhost:18080${LOGIN_PATH}")"
echo "test_user_login=$USER_CODE"
test -s /tmp/c_user.txt || echo "FAIL: no session cookie for test user"

# Admin user.
ADMIN_CODE="$(curl -sS -o /tmp/admin_body.txt -w "%{http_code}" \
  -c /tmp/c_admin.txt -H "content-type: application/json" \
  -d '{"email":"admin@mcpruntime.org","password":"admin@123"}' \
  "http://localhost:18080${LOGIN_PATH}")"
echo "admin_login=$ADMIN_CODE"

# Admin-only endpoint with each cookie. Expect the admin cookie to succeed
# and the plain-user cookie to be 401/403 (unless platform policy intentionally
# grants the role; if it does, that should be documented in services/api).
curl -sS -o /dev/null -w "user_to_grants=%{http_code}\n" \
  -b /tmp/c_user.txt -X POST -H "content-type: application/json" \
  -d '{}' http://localhost:18080/api/runtime/grants
curl -sS -o /dev/null -w "admin_to_grants=%{http_code}\n" \
  -b /tmp/c_admin.txt -X POST -H "content-type: application/json" \
  -d '{}' http://localhost:18080/api/runtime/grants
```

## Step 5 — Sub-suite C: UI→API proxy

The UI must reach `/api/*` on the same origin from a logged-in browser
session and must not require the user to know the API key.

```bash
for path in \
  /api/dashboard/summary \
  "/api/analytics/usage?limit=5" \
  "/api/events/filter?server=go-example-mcp&limit=5" \
  /api/runtime/servers ; do
  code="$(curl -sS -o /tmp/last.json -w "%{http_code}" \
    -b /tmp/c_user.txt "http://localhost:18080${path}")"
  printf "%s -> %s\n" "$path" "$code"
done
```

A 308 redirect loop here means the UI's `UI_REQUIRE_HTTPS` middleware is
matching the dev host — check the deployment env per `CLAUDE.md`
**Dashboard 308 redirect loop in dev**.

## Step 6 — Sub-suite D: Servers tab + connect config

The Servers tab returns a copyable JSON the user pastes into a client. In
test mode the URL must use the **same reachable local origin**, not a
public hostname.

```bash
CONFIG="$(curl -sS -H "Host: localhost:18080" -b /tmp/c_user.txt \
  http://localhost:18080/api/runtime/servers/go-example-mcp/connect-config 2>/dev/null \
  || curl -sS -b /tmp/c_user.txt http://localhost:18080/api/runtime/servers)"
echo "$CONFIG" | jq -e '.. | strings | select(test("http://localhost:18080/.*/mcp"))' \
  >/dev/null || echo "FAIL: connect config does not use local origin"
echo "$CONFIG" | jq -e '.. | strings | select(test("https://mcp\\.|https://platform\\."))' \
  >/dev/null && echo "FAIL: connect config leaks production hostname in test mode"
```

If the endpoint path differs from `/api/runtime/servers/<name>/connect-config`,
grep `services/ui/main.go` and `services/api/main.go` for the actual route
shape (the route is owned by the API; the UI forwards the browser origin).
Do not invent endpoints — record a **blocker** and stop the sub-suite if you
cannot locate it.

## Step 7 — Sub-suite E: Static asset integrity

```bash
node --check services/ui/static/app.js                    # parse-clean
curl -sS -o /tmp/app.js http://localhost:18080/static/app.js
node --check /tmp/app.js                                  # served bundle parses
diff -q services/ui/static/app.js /tmp/app.js || echo "Note: bundle differs from source (expected if build step exists)"
```

## Step 8 — Sub-suite F: API contract sanity

```bash
curl -sS -b /tmp/c_user.txt http://localhost:18080/api/dashboard/summary \
  | jq -e 'type == "object"' >/dev/null || echo "FAIL: dashboard summary shape"

curl -sS -b /tmp/c_user.txt "http://localhost:18080/api/analytics/usage?limit=5" \
  | jq -e 'type == "object" or type == "array"' >/dev/null \
  || echo "FAIL: analytics usage shape"

curl -sS -b /tmp/c_user.txt "http://localhost:18080/api/events/filter?server=go-example-mcp&limit=5" \
  | jq -e '.events // .items // [] | type == "array"' >/dev/null \
  || echo "FAIL: events filter shape"
```

Field-level expectations should come from `services/api/main.go` handler
responses, not from memory.

## Step 9 — Sub-suite G: Public-host defense (only when ingress changed)

In test mode `MCP_PLATFORM_DOMAIN` is unset and `/grafana` + `/prometheus`
route through the dev path-based gateway. The invariant under test:
**when** `MCP_PLATFORM_DOMAIN` is set, the `mcp-sentinel-platform-ui`
Ingress must not expose `/grafana` or `/prometheus` on the public host.
Validate by inspecting the live ingress shape:

```bash
kubectl get ingress -n mcp-sentinel -o yaml \
  | yq '.items[] | select(.metadata.name=="mcp-sentinel-platform-ui") | .spec.rules' 2>/dev/null \
  || kubectl get ingress -n mcp-sentinel mcp-sentinel-platform-ui -o yaml \
       | grep -E 'path:|grafana|prometheus'
# Hard fail if any rule path for the platform host points at grafana or
# prometheus services.
```

If `mcp-sentinel-platform-ui` does not exist (test mode without a platform
domain), record this sub-suite as **N/A in test mode** and recommend running
it again in a `MCP_PLATFORM_DOMAIN=*` cluster, or with the user explicitly
setting `MCP_PLATFORM_INGRESS_HOST`.

## Step 10 — Optional: headless browser sweep

When `playwright` or `chromium-headless-shell` is available, capture a
smoke screenshot per tab. This catches CSS regressions and JS exceptions
that handler tests miss. Skip silently if neither is installed — do not
auto-install.

```bash
command -v npx >/dev/null && command -v chromium-headless-shell >/dev/null && {
  npx --yes playwright@1 install chromium >/dev/null 2>&1
  cat > /tmp/ui-smoke.mjs <<'EOF'
import { chromium } from 'playwright';
const b = await chromium.launch();
const p = await b.newPage();
const errors = [];
p.on('pageerror', e => errors.push(String(e)));
p.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
await p.goto('http://localhost:18080/', { waitUntil: 'networkidle' });
await p.screenshot({ path: '/tmp/ui-dashboard.png', fullPage: true });
console.log(JSON.stringify({ title: await p.title(), errors }));
await b.close();
EOF
  node /tmp/ui-smoke.mjs
} || echo "headless browser not available — skipping"
```

Any `pageerror` or console-error entry is a finding.

## Step 11 — Report

Use `_shared/FINDINGS-TEMPLATE.md`. UI findings should always include:

- The user-visible symptom (blank tab, 308 loop, broken Servers tab JSON).
- The pinpoint in `services/ui/main.go`, `services/ui/static/app.js`, or
  `services/api/main.go` likely responsible.
- A regression-test suggestion targeting `services/ui/main_test.go` or a new
  handler test in `services/api`.

Cross-link to `qa-e2e-security` for header/lockout findings, to
`qa-e2e-operations` for rollout/ingress findings, and to `qa-e2e-perf`
when a UI complaint is really a latency complaint.
