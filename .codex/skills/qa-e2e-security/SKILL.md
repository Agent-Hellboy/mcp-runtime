---
name: qa-e2e-security
description: Real-cluster security regression QA — backend auth enforcement, grant/session policy, gateway deny paths, audit emission, trust escalation, UI/API security headers, HTTPS redirect modes, and secret-leak scanning in live logs. Use when Codex is asked to verify a change did not regress auth, governance, gateway policy, audit, or UI security headers on a live cluster; or for pre-release security-regression sweeps. Complements static-only security-audit/security-audit-platform with real traffic. Assumes qa-cluster-bringup has run.
---

# QA — E2E Security (live cluster)

## Overview

`security-audit` and `security-audit-platform` are **static** reviews — diff,
code paths, scanner output. This skill is the **dynamic** counterpart: it
fires real requests at the live cluster and confirms the security
invariants actually hold at runtime. It catches policy-materialization
regressions, header middleware regressions, and audit-event regressions that
static review and unit tests miss.

Threat profiles in scope:

- **anonymous → user** (no key, bad key, expired session)
- **user → admin** (UI key vs admin key vs ingest-only key)
- **agent A → agent B** (grant scoping by `humanID`/`agentID`/`serverRef`)
- **low trust → high trust** tool (consent gating)
- **internal pod → secret** (reach API/proxy/operator secrets unprivileged)
  → for cluster RBAC/PSS coverage, defer to `k8s-hardening-audit`.

## Step 1 — Confirm precondition

```bash
kubectl config current-context | grep -qx kind-mcp-runtime \
  || { echo "Run qa-cluster-bringup first"; exit 1; }
./bin/mcp-runtime cluster doctor
```

Pull the three key types up front; **never echo them into the report**.

```bash
UI_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.UI_API_KEY}' | base64 -d)"
test -n "$UI_KEY" || { echo "Failed to retrieve UI_KEY"; exit 1; }
INGEST_KEY="$(kubectl get secret mcp-sentinel-secrets -n mcp-sentinel \
  -o jsonpath='{.data.INGEST_API_KEYS}' | base64 -d | cut -d, -f1)"
test -n "$INGEST_KEY" || { echo "Failed to retrieve INGEST_KEY"; exit 1; }
```

## Step 2 — Choose mode

- **head-only**. Run the full backend + UI security matrix.
- **git-range** (`BASE=<merge-base>`, default `origin/main`). Trim by diff.

Sub-suites by changed paths:

| Diff touches | Required sub-suites |
|---|---|
| `services/api/**`, `pkg/access/**` | A. Backend auth, B. Grants/sessions, C. Audit |
| `services/mcp-proxy/**`, `internal/operator/**` (policy/render) | B. Grants/sessions, D. Trust escalation, C. Audit |
| `services/ui/**` (middleware, login, proxy) | E. UI security headers, F. Login + lockout, G. UI→API proxy |
| `config/ingress/**`, `services/traefik-plugins/**` | H. Ingress + PII redactor, E (re-run) |
| `k8s/**` Secrets / SA / RBAC | Hand off to `k8s-hardening-audit` |

Always run **I. Live-log secret scan** regardless of diff.

## Step 3 — Sub-suite A: Backend auth enforcement

```bash
# Anonymous → 401 on admin paths.
curl -sS -o /dev/null -w "anon=%{http_code}\n" \
  http://localhost:18080/api/dashboard/summary           # want 401
curl -sS -o /dev/null -w "anon=%{http_code}\n" \
  http://localhost:18080/api/analytics/usage             # want 401

# Bad key → 401.
curl -sS -o /dev/null -w "bad=%{http_code}\n" \
  -H "x-api-key: NOPE" http://localhost:18080/api/dashboard/summary

# Ingest-only key on admin → 401/403 (must NOT be admin).
curl -sS -o /dev/null -w "ingest_only=%{http_code}\n" \
  -H "x-api-key: $INGEST_KEY" http://localhost:18080/api/dashboard/summary

# Admin/UI key → 200.
curl -sS -o /dev/null -w "admin=%{http_code}\n" \
  -H "x-api-key: $UI_KEY" http://localhost:18080/api/dashboard/summary

# Mutating admin endpoints require admin (not just user). Try a write with
# only the ingest key and confirm 401/403:
curl -sS -o /dev/null -w "ingest_write=%{http_code}\n" -X POST \
  -H "x-api-key: $INGEST_KEY" -H "content-type: application/json" \
  -d '{}' http://localhost:18080/api/runtime/grants
```

Any admin response code other than 200 with `$UI_KEY` is a finding. Any non-401/403
on the anonymous / bad-key / ingest-only paths is a **High** severity finding —
matches `requireRole(roleAdmin, …)` enforcement in `services/api/main.go`.

## Step 4 — Sub-suite B: Grants & sessions enforce on the gateway

Baseline traffic (should succeed):

```bash
BASE=http://localhost:18080/go-example-mcp/mcp
PROTO=2025-06-18
H=(-H "content-type: application/json" -H "accept: application/json, text/event-stream"
   -H "Mcp-Protocol-Version: $PROTO"
   -H "X-MCP-Human-ID: local-user" -H "X-MCP-Agent-ID: local-agent"
   -H "X-MCP-Agent-Session: local-session")
init() {
  SESSION="$(curl -si "${H[@]}" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' "$BASE" \
    | awk -F': ' 'tolower($1)=="mcp-session-id"{print $2}' | tr -d '\r')"
  curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
    -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$BASE" >/dev/null
}
call() {
  curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
    -d "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"tools/call\",\"params\":$1}" "$BASE"
}

init
call '{"name":"add","arguments":{"a":2,"b":3}}'      # want allow
```

Toggle the grant off → expect deny within a few seconds (sidecar reload):

```bash
kubectl annotate mcpaccessgrant go-example-local -n mcp-servers \
  qa.mcpruntime.org/disable="$(date +%s)" --overwrite
# CLI toggle is also available; use whichever the docs already exercise:
# ./bin/mcp-runtime access grant disable go-example-local --namespace mcp-servers

sleep 8
init
RESP="$(call '{"name":"add","arguments":{"a":2,"b":3}}')"
echo "$RESP" | grep -qiE 'denied|forbidden|policy' \
  || { echo "FAIL: disabled grant still allowed"; echo "$RESP"; }
```

Re-enable to leave the cluster in a working state for downstream skills:

```bash
kubectl annotate mcpaccessgrant go-example-local -n mcp-servers \
  qa.mcpruntime.org/disable- || true
kubectl apply -f /tmp/go-example-access.yaml
sleep 8
```

Revoke the session → expect deny:

```bash
# Apply a Revoked-state session and re-test.
kubectl patch mcpagentsession local-session -n mcp-servers --type=merge \
  -p '{"spec":{"revoked":true}}' 2>/dev/null \
  || kubectl annotate mcpagentsession local-session -n mcp-servers \
       qa.mcpruntime.org/revoke="$(date +%s)" --overwrite
sleep 8
init
call '{"name":"add","arguments":{"a":2,"b":3}}' | grep -qiE 'session|denied' \
  || echo "FAIL: revoked session still allowed"
kubectl patch mcpagentsession local-session -n mcp-servers --type=merge \
  -p '{"spec":{"revoked":false}}' 2>/dev/null || true
sleep 8
```

Cross-tenant scoping — try the call with a different `humanID`/`agentID` that
has no grant:

```bash
H2=("${H[@]/X-MCP-Human-ID: local-user/X-MCP-Human-ID: other-user}")
SESSION_OTHER="$(curl -si "${H2[@]}" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' "$BASE" \
  | awk -F': ' 'tolower($1)=="mcp-session-id"{print $2}' | tr -d '\r')"
curl -sS "${H2[@]}" -H "Mcp-Session-Id: $SESSION_OTHER" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$BASE" >/dev/null
RESP="$(curl -sS "${H2[@]}" -H "Mcp-Session-Id: $SESSION_OTHER" \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":1}}}' "$BASE")"
echo "$RESP" | grep -qiE 'denied|forbidden|no.*grant' \
  || echo "FAIL: ungranted subject allowed"
```

## Step 5 — Sub-suite C: Audit emission for allow + deny

Allow + deny paths must both emit audit events; missing audit on deny is a
common regression.

```bash
ADMIN_KEY="$UI_KEY"
BEFORE="$(curl -sS -H "x-api-key: $ADMIN_KEY" \
  "http://localhost:18080/api/events/filter?server=go-example-mcp&limit=100" \
  | jq '.events | length // length // 0')"
# fire one allow + one deny (tool not in policy)
init
call '{"name":"add","arguments":{"a":1,"b":1}}'               >/dev/null
call '{"name":"definitely-not-a-tool","arguments":{}}'        >/dev/null
sleep 3
AFTER="$(curl -sS -H "x-api-key: $ADMIN_KEY" \
  "http://localhost:18080/api/events/filter?server=go-example-mcp&limit=100" \
  | jq '.events | length // length // 0')"
[ "$AFTER" -ge "$((BEFORE + 2))" ] || echo "FAIL: missing audit events"
```

## Step 6 — Sub-suite D: Trust escalation

`upper` is configured `requiredTrust: medium`. Try it with a session
`consentedTrust: low` and expect deny:

```bash
kubectl patch mcpagentsession local-session -n mcp-servers --type=merge \
  -p '{"spec":{"consentedTrust":"low"}}'
sleep 8
init
RESP="$(call '{"name":"upper","arguments":{"message":"hi"}}')"
echo "$RESP" | grep -qiE 'trust|denied|forbidden' \
  || echo "FAIL: low-trust session called medium-trust tool"
kubectl patch mcpagentsession local-session -n mcp-servers --type=merge \
  -p '{"spec":{"consentedTrust":"high"}}'
sleep 8
```

## Step 7 — Sub-suite E: UI security headers

The UI middleware in `services/ui/main.go` sets baseline security headers
on every response, HSTS only on forwarded HTTPS, and Cache-Control on `/api`.

```bash
# Dashboard (HTTP path) — must have nosniff + frame-ancestors none, no HSTS.
curl -sSI http://localhost:18080/ | tr -d '\r' > /tmp/h-root.txt
grep -q '^X-Content-Type-Options: nosniff$' /tmp/h-root.txt    || echo "FAIL: nosniff"
grep -qi "Content-Security-Policy:.*frame-ancestors 'none'" /tmp/h-root.txt \
  || echo "FAIL: CSP frame-ancestors"
grep -i "script-src.*unsafe-inline" /tmp/h-root.txt && echo "FAIL: CSP allows unsafe-inline"
grep -i '^Strict-Transport-Security' /tmp/h-root.txt && echo "FAIL: HSTS on plain HTTP"

# Simulate TLS terminator → HSTS must appear.
curl -sSI -H "X-Forwarded-Proto: https" http://localhost:18080/ \
  | tr -d '\r' | grep -qi '^Strict-Transport-Security: max-age=' \
  || echo "FAIL: HSTS missing on forwarded HTTPS"

# /api responses must be uncacheable.
curl -sSI -H "x-api-key: $UI_KEY" http://localhost:18080/api/health \
  | tr -d '\r' | grep -qi '^Cache-Control:.*no-store' \
  || echo "FAIL: /api Cache-Control"
```

## Step 8 — Sub-suite F: Login + lockout

`PLATFORM_DEV_LOGIN` seeds `test@mcpruntime.org`/`test@123` and
`admin@mcpruntime.org`/`admin@123`. Verify success and lockout. The exact
login endpoint shape is owned by `services/ui/main.go` — read it before
asserting shape; do not invent fields.

```bash
# Correct creds → 200 + session cookie.
curl -sS -i -c /tmp/c.txt -H "content-type: application/json" \
  -d '{"email":"test@mcpruntime.org","password":"test@123"}' \
  http://localhost:18080/login | head -1

# Wrong password 6× → lockout (handler increments per-IP failure counter).
for i in 1 2 3 4 5 6; do
  curl -sS -o /dev/null -w "$i=%{http_code}\n" -H "content-type: application/json" \
    -d '{"email":"test@mcpruntime.org","password":"WRONG"}' \
    http://localhost:18080/login
done
# At least one of those should be a lockout/429 response; never a 5xx.
```

## Step 9 — Sub-suite G: UI→API proxy

Browser-origin requests proxy through the UI; direct API-key clients also
work. Both should be enforced, neither should leak the API key to the
browser.

```bash
# Confirm browser config endpoint does NOT include the API key.
curl -sS http://localhost:18080/config | jq . | grep -i apiKey \
  && echo "FAIL: api key leaked via /config" || echo "OK: no key in /config"

# Authenticated browser session can reach /api.
curl -sS -b /tmp/c.txt http://localhost:18080/api/dashboard/summary | jq -e '.servers // .summary // 0' >/dev/null \
  || echo "FAIL: authed browser cannot reach /api"

# Direct API-key client should also work.
curl -sS -H "x-api-key: $UI_KEY" http://localhost:18080/api/dashboard/summary \
  | jq -e '.' >/dev/null || echo "FAIL: direct key client"
```

## Step 10 — Sub-suite H: Ingress + PII redactor (only when those changed)

```bash
kubectl get ingress -A
kubectl logs -n traefik deploy/traefik --tail=120 \
  | grep -iE 'middleware.*does not exist|panic|error' || echo OK
# PII redactor on the documented dev overlay should redact known patterns in
# request/response bodies. Probe with a synthetic SSN/email payload to a tool
# that echoes; verify the audit body in /api/events does not contain the raw value.
```

## Step 11 — Sub-suite I: Live-log secret scan (always)

Regressions where tokens leak into logs are common after refactors.

```bash
for ns in mcp-runtime mcp-sentinel mcp-servers traefik registry; do
  for d in $(kubectl get pods -n "$ns" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); do
    kubectl logs -n "$ns" "$d" --all-containers --since=10m 2>/dev/null \
      | grep -aE 'Bearer [A-Za-z0-9._-]{20,}|sk-[A-Za-z0-9]{16,}|eyJ[A-Za-z0-9._-]{20,}|x-api-key:\s*[A-Za-z0-9_-]{12,}'
  done
done
# Any hit is a High severity finding. Do NOT copy the matched value into the
# report — describe the pod + line number range only.
```

## Step 12 — Report

Use the rubric and template in `../_shared/FINDINGS-TEMPLATE.md` exactly. Each
finding should include:

- The **trust boundary** it crosses (anon→user, user→admin, agent A→agent B,
  low→high trust, pod→secret).
- The **command + response code or body fragment** that demonstrates the
  failure (with secrets redacted).
- A **regression test** suggestion that lands in `services/api/main_test.go`,
  `services/ui/main_test.go`, `services/mcp-proxy/...`, or `pkg/access/...`.

Cross-link to `security-audit` / `security-audit-platform` for any static
counterpart, to `k8s-hardening-audit` for cluster-policy gaps, and to
`supply-chain-audit` for any dependency-CVE angle uncovered.
