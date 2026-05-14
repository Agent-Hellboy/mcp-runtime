---
name: mcp-spec-compliance
description: Validate MCP Runtime against the upstream Model Context Protocol specification at github.com/modelcontextprotocol/modelcontextprotocol — protocol version negotiation, Streamable HTTP transport headers, JSON-RPC 2.0 envelope, initialize/tools/resources/prompts/sampling shapes, session semantics, error codes, and merged + open SEPs. Runs static checks against the runtime code and dynamic checks against the live Kind cluster. Use when Codex is asked to validate MCP compliance, audit transport/protocol behavior, prepare for an upstream protocol bump, or compare runtime behavior against a specific spec revision. Complements qa-e2e-security (auth/governance) and qa-e2e-operations (operator/CLI) with protocol-level conformance.
---

# MCP Spec Compliance Audit

## Overview

This skill audits MCP Runtime — the proxy/gateway, example servers, agent
adapters, and the CLI probe — against the **upstream Model Context Protocol
specification** at <https://github.com/modelcontextprotocol/modelcontextprotocol>.
The spec is versioned (`2024-11-05`, `2025-03-26`, `2025-06-18`, `draft`); the
runtime currently pins `2025-06-18` (see
`internal/agentadapter/config.go: DefaultProtocolVersion`).

Goals:

- Confirm the runtime correctly implements the spec revision it **claims** to
  support — protocol-version negotiation, Streamable HTTP, JSON-RPC 2.0
  envelope, tools/resources/prompts/sampling shapes, session semantics, error
  codes, and capability advertisement.
- Detect drift from the upstream schema (fields renamed, fields removed,
  required-fields changes) when bumping pinned revision.
- Surface relevant merged SEPs and open SEP PRs that affect runtime behavior.
- Distinguish "spec says X, runtime does X" from "spec says X, runtime does Y"
  from "spec is silent, runtime does Y by choice."

Non-goals:

- Auth/grant/session policy enforcement → `qa-e2e-security`.
- Operator/CLI/setup regressions → `qa-e2e-operations`.
- Cluster RBAC/PSS hygiene → `k8s-hardening-audit`.

## Step 1 — Pin the spec revision under audit

```bash
SPEC_REPO="${SPEC_REPO:-https://github.com/modelcontextprotocol/modelcontextprotocol}"
SPEC_REV="${SPEC_REV:-$(grep -oE '"20[0-9]{2}-[0-9]{2}-[0-9]{2}"' \
  internal/agentadapter/config.go | head -1 | tr -d '"')}"
SPEC_REF="${SPEC_REF:-main}"     # upstream branch or tag to fetch from
echo "Auditing runtime against MCP spec rev=$SPEC_REV @ ref=$SPEC_REF"
```

`SPEC_REV` defaults to what the runtime claims (the `DefaultProtocolVersion`
constant). `SPEC_REF` is the upstream git ref to fetch docs/schemas from —
default `main` for the latest published, override to a tag like
`v2025.06.18` if upstream publishes one.

Record both in the report header. A compliance finding is meaningless without
"compliance with **which** revision."

## Step 2 — Fetch the upstream spec artifacts

Cache locally so the skill can re-run without re-fetching.

```bash
MCP_SPEC_TMP="${MCP_SPEC_TMP:-$(mktemp -d)}"
trap 'rm -rf "$MCP_SPEC_TMP"' EXIT
SPEC_CACHE="${SPEC_CACHE:-/tmp/mcp-spec/$SPEC_REF}"
mkdir -p "$SPEC_CACHE"
fetch() {
  local path="$1" url="$SPEC_REPO/raw/$SPEC_REF/$1"
  local out="$SPEC_CACHE/$path"
  mkdir -p "$(dirname "$out")"
  [ -s "$out" ] || curl -fsSL "$url" -o "$out"
  echo "$out"
}

SCHEMA="$(fetch "schema/$SPEC_REV/schema.json")"
CHANGELOG_DRAFT="$(fetch "docs/specification/draft/changelog.mdx")"
SEP_INDEX="$(fetch "docs/seps/index.mdx")"
ROADMAP="$(fetch "docs/development/roadmap.mdx")"
# Per-revision spec body (Streamable HTTP, lifecycle, server features):
SPEC_BASE="docs/specification/$SPEC_REV"
fetch "$SPEC_BASE/basic/transports.mdx" >/dev/null || true
fetch "$SPEC_BASE/basic/lifecycle.mdx"  >/dev/null || true
fetch "$SPEC_BASE/server/tools.mdx"     >/dev/null || true
fetch "$SPEC_BASE/server/resources.mdx" >/dev/null || true
fetch "$SPEC_BASE/server/prompts.mdx"   >/dev/null || true
```

If any fetch fails, record it as a blocker and stop — do not silently audit
against a missing schema. If the spec layout has changed upstream (paths
moved), update this skill rather than guess.

## Step 3 — Inventory runtime claims

Find every place the runtime states a protocol version, advertises
capabilities, or shapes a JSON-RPC message. These are the surfaces that must
match the pinned revision.

```bash
# Protocol version constants and literals.
grep -rIn 'DefaultProtocolVersion\|protocolVersion\|Mcp-Protocol-Version\|MCP-Protocol-Version\|"20[0-9]\{2\}-[0-9]\{2\}-[0-9]\{2\}"' \
  --include='*.go' --include='*.md' --include='*.yaml' \
  -- internal/ services/ examples/ docs/ pkg/ cmd/

# Streamable HTTP transport contract in the gateway:
sed -n '1,$p' services/mcp-proxy/rpc.go services/mcp-proxy/proxy.go \
  services/mcp-proxy/types.go 2>/dev/null | grep -nE \
  'jsonrpc|method|Mcp-Session-Id|content-type|text/event-stream|application/json|notifications/initialized|tools/(list|call)|initialize'

# Agent adapter (stdio <-> HTTP shim) protocol surface.
sed -n '1,$p' internal/agentadapter/proxy.go internal/agentadapter/stdio.go \
  internal/agentadapter/config.go internal/agentadapter/rpc_metadata.go \
  2>/dev/null | grep -nE 'Header\.Set\|Mcp-|protocolVersion|jsonrpc|initialize'
```

The output is the "claims" list for the report. Every later check maps a
claim to spec text or schema and reports match / mismatch / extension.

## Step 4 — Static schema conformance

Validate runtime-side fixtures and golden responses against the upstream
schema. Use a JSON Schema validator that supports the draft used by the spec
(typically draft 2020-12).

```bash
command -v jv >/dev/null || go install github.com/santhosh-tekuri/jsonschema/cmd/jv@latest
JV="$(go env GOPATH)/bin/jv"

# Collect captured MCP envelopes from existing tests and fixtures.
mapfile -t FIXTURES < <(grep -rIl --include='*.go' --include='*.json' \
  '"jsonrpc":\s*"2\.0"' \
  -- internal/ services/ examples/ test/ pkg/ 2>/dev/null)
echo "Found ${#FIXTURES[@]} files containing JSON-RPC envelopes"

# Extract message-shaped JSON bodies into a unique temporary directory
# (one tool that does this safely lives in the spec repo's tests; here we
# accept that not every embedded string is round-trippable and only validate
# files where extraction succeeded).
MCP_SPEC_TMP="${MCP_SPEC_TMP:-$(mktemp -d)}"
trap 'rm -rf "$MCP_SPEC_TMP"' EXIT
FIXTURE_TMP="$MCP_SPEC_TMP/fixtures"
mkdir -p "$FIXTURE_TMP"
python3 - "$SPEC_CACHE" "$FIXTURE_TMP" "${FIXTURES[@]}" <<'PY'
import json, re, sys, os
cache, out, files = sys.argv[1], sys.argv[2], sys.argv[3:]
RE = re.compile(r'(\{[^{}]*"jsonrpc"\s*:\s*"2\.0"[^{}]*\})', re.S)
n = 0
for f in files:
    try:
        body = open(f).read()
    except Exception:
        continue
    for m in RE.finditer(body):
        try:
            obj = json.loads(m.group(1))
        except Exception:
            continue
        n += 1
        with open(f"{out}/{os.path.basename(f)}.{n}.json","w") as g:
            json.dump(obj, g)
print(f"Extracted {n} envelopes -> {out}")
PY

# Validate every extracted envelope against the pinned schema.
fail=0
for f in "$FIXTURE_TMP"/*.json; do
  test -e "$f" || continue
  "$JV" "$SCHEMA" "$f" >/dev/null 2>&1 || { fail=$((fail+1)); echo "INVALID: $f"; "$JV" "$SCHEMA" "$f" 2>&1 | head -5; }
done
echo "Schema validation: $fail invalid envelopes"
```

Any **invalid** envelope in `services/mcp-proxy/`, `examples/`, or
`internal/agentadapter/` is a finding (the runtime is emitting / asserting
non-spec shapes). Invalid envelopes in `test/golden/`, `test/integration/`,
or e2e fixtures are findings on the test, not the runtime, but track them
either way.

## Step 5 — Live transport conformance (against the Kind cluster)

Precondition: `qa-cluster-bringup` has run, port-forward is up, demo server
deployed with a valid grant/session.

```bash
kubectl config current-context | grep -qx kind-mcp-runtime \
  || { echo "Run qa-cluster-bringup first"; exit 1; }
BASE=http://localhost:18080/go-example-mcp/mcp
PROTO="$SPEC_REV"
H=(-H "content-type: application/json"
   -H "accept: application/json, text/event-stream"
   -H "Mcp-Protocol-Version: $PROTO"
   -H "X-MCP-Human-ID: local-user"
   -H "X-MCP-Agent-ID: local-agent"
   -H "X-MCP-Agent-Session: local-session")
```

### 5a. `initialize` response conformance

```bash
MCP_SPEC_TMP="${MCP_SPEC_TMP:-$(mktemp -d)}"
trap 'rm -rf "$MCP_SPEC_TMP"' EXIT
INIT="$(curl -sS "${H[@]}" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
        "protocolVersion":"'"$PROTO"'",
        "capabilities":{},
        "clientInfo":{"name":"qa-spec-compliance","version":"0"}}}' "$BASE")"
echo "$INIT" | jq .
echo "$INIT" > "$MCP_SPEC_TMP/mcp-init.json"
"$JV" "$SCHEMA" "$MCP_SPEC_TMP/mcp-init.json" 2>&1 | head -10

# Required fields per spec lifecycle: protocolVersion, capabilities, serverInfo.
echo "$INIT" | jq -e '.result.protocolVersion == "'"$PROTO"'"' >/dev/null \
  || echo "FAIL: server did not echo or downgrade protocolVersion correctly"
echo "$INIT" | jq -e '.result.capabilities | type == "object"' >/dev/null \
  || echo "FAIL: capabilities missing or wrong type"
echo "$INIT" | jq -e '.result.serverInfo.name and .result.serverInfo.version' >/dev/null \
  || echo "FAIL: serverInfo missing name/version"
```

### 5b. Streamable HTTP transport headers

```bash
# Mcp-Session-Id MUST be issued on initialize when the server is stateful
# (per Streamable HTTP). Capture and re-use; verify it survives one round-trip.
SESSION="$(curl -si "${H[@]}" \
  -d '{"jsonrpc":"2.0","id":2,"method":"initialize","params":{
        "protocolVersion":"'"$PROTO"'","capabilities":{},
        "clientInfo":{"name":"qa","version":"0"}}}' "$BASE" \
  | awk -F': ' 'tolower($1)=="mcp-session-id"{print $2}' | tr -d '\r')"
[ -n "$SESSION" ] || echo "FAIL: server did not issue Mcp-Session-Id"

# Content-Type negotiation: JSON request should get JSON response by default;
# event-stream only when server elects to stream.
curl -sSI "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$BASE" \
  | tr -d '\r' | grep -iE '^Content-Type:' | head -1

# DELETE on the MCP endpoint terminates the session (per Streamable HTTP).
# A subsequent request with the same session MUST fail.
curl -sS -X DELETE "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -o "$MCP_SPEC_TMP/del.out" -w "delete=%{http_code}\n" "$BASE"
curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":99,"method":"tools/list"}' "$BASE" \
  | grep -qiE 'error|session|invalid' \
  || echo "FAIL: session reuse after DELETE was accepted"
```

If `DELETE` returns 405 or the server silently accepts post-DELETE calls,
that is a transport-spec violation. If the runtime intentionally treats
`Mcp-Session-Id` as platform-governance only (decoupled from MCP protocol
session), record that as an **intentional deviation** in the report and link
to the design rationale in code/docs — do not flag it as a bug without
context.

### 5c. `tools/list` shape

```bash
TOOLS="$(curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' "$BASE")"
echo "$TOOLS" | jq -e '.result.tools | type == "array" and length > 0' >/dev/null \
  || echo "FAIL: tools/list missing or empty"
echo "$TOOLS" | jq -e '.result.tools | all(.name and (.inputSchema | type == "object"))' >/dev/null \
  || echo "FAIL: tool entry missing required name or inputSchema"
echo "$TOOLS" > "$MCP_SPEC_TMP/mcp-tools.json"
"$JV" "$SCHEMA" "$MCP_SPEC_TMP/mcp-tools.json" 2>&1 | head -10
```

### 5d. `tools/call` result envelope

```bash
CALL="$(curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call",
       "params":{"name":"add","arguments":{"a":2,"b":3}}}' "$BASE")"
echo "$CALL" | jq -e '.result.content | type == "array"' >/dev/null \
  || echo "FAIL: tools/call result.content must be array"
echo "$CALL" | jq -e '.result.content[0].type and .result.content[0].text != null' >/dev/null \
  || echo "FAIL: content[0] missing required type/text"
echo "$CALL" > "$MCP_SPEC_TMP/mcp-call.json"
"$JV" "$SCHEMA" "$MCP_SPEC_TMP/mcp-call.json" 2>&1 | head -10
```

### 5e. JSON-RPC error mapping

The spec inherits JSON-RPC 2.0 error codes (`-32700`/`-32600`/`-32601`/`-32602`/`-32603`)
and adds MCP-specific application errors. Probe each:

```bash
# Parse error: invalid JSON.
curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  --data-binary '{not json' "$BASE" \
  | jq -e '.error.code == -32700' >/dev/null \
  || echo "FAIL: parse error should map to -32700"

# Method not found.
curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":5,"method":"definitely/not/a/method"}' "$BASE" \
  | jq -e '.error.code == -32601' >/dev/null \
  || echo "FAIL: unknown method should map to -32601"

# Invalid params (tool exists, wrong args).
curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call",
       "params":{"name":"add","arguments":{"a":"not-a-number"}}}' "$BASE" \
  | jq -e '.error.code == -32602 or (.result.isError == true)' >/dev/null \
  || echo "FAIL: invalid params should map to -32602 or isError result"
```

MCP allows tool-execution errors to surface either as a top-level
`error.code` or as `result.isError=true` with content; both shapes are
spec-legal but should be **consistent** for a given runtime. Flag any tool
that mixes both for similar failure modes.

### 5f. Protocol-version negotiation

```bash
# Unsupported version: the server SHOULD respond with a version it supports,
# not echo the unsupported one.
RESP="$(curl -sS "${H[@]/Mcp-Protocol-Version: $PROTO/Mcp-Protocol-Version: 1999-01-01}" \
  -d '{"jsonrpc":"2.0","id":7,"method":"initialize","params":{
        "protocolVersion":"1999-01-01","capabilities":{},
        "clientInfo":{"name":"qa","version":"0"}}}' "$BASE")"
echo "$RESP" | jq -e '.result.protocolVersion != "1999-01-01"' >/dev/null \
  || echo "FAIL: server echoed unsupported protocolVersion"

# Missing header: behavior is implementation-defined; record what the runtime
# does so docs and clients stay aligned.
curl -sS -H "content-type: application/json" -H "accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":8,"method":"initialize","params":{
        "protocolVersion":"'"$PROTO"'","capabilities":{},
        "clientInfo":{"name":"qa","version":"0"}}}' "$BASE" | jq -e '.result // .error' >/dev/null
```

## Step 6 — Capability advertisement vs implementation

For each capability advertised in `initialize`, confirm the corresponding
method actually works. Missing capability + working method, or advertised
capability + missing method, are both findings.

```bash
caps="$(echo "$INIT" | jq -r '.result.capabilities | keys[]')"
for cap in $caps; do
  case "$cap" in
    tools)     M='tools/list'     ;;
    resources) M='resources/list' ;;
    prompts)   M='prompts/list'   ;;
    logging)   M='logging/setLevel' ;;
    *)         M='' ;;
  esac
  [ -n "$M" ] || continue
  code="$(curl -sS "${H[@]}" -H "Mcp-Session-Id: $SESSION" \
    -d '{"jsonrpc":"2.0","id":50,"method":"'"$M"'"}' "$BASE" \
    | jq -r '.error.code // "ok"')"
  echo "capability $cap -> $M : $code"
done
```

Cross-check: any method the gateway forwards (`services/mcp-proxy/rpc.go`
method allow-list) that is **not** present in `initialize.capabilities`
should be flagged — the gateway should not encourage clients to call a
capability the server did not advertise.

## Step 7 — SEP and roadmap cross-reference

Read the merged SEP index and roadmap fetched in Step 2; produce a short
table of relevant items and how the runtime stands.

```bash
echo "=== Merged SEPs in index ==="
grep -E '^\s*-\s+SEP-[0-9]+' "$SEP_INDEX" | head -60

echo "=== Draft spec changelog (deltas vs $SPEC_REV) ==="
sed -n '1,80p' "$CHANGELOG_DRAFT"

echo "=== Open SEP PRs (snapshot at audit time) ==="
gh -R modelcontextprotocol/modelcontextprotocol pr list \
  --search 'SEP in:title is:open' --limit 30 \
  --json number,title,labels,updatedAt 2>/dev/null \
  || curl -fsSL "https://api.github.com/repos/modelcontextprotocol/modelcontextprotocol/pulls?state=open&per_page=30" \
       | jq -r '.[] | select(.title|test("SEP";"i")) | "#\(.number) \(.title)"'
```

For each SEP relevant to gateways/transport/auth/sessions/discovery/tasks,
classify the runtime's stance in the report:

- **Implemented** — link to file:line where it lives.
- **Tracked** — design note in docs; no code yet.
- **Not applicable** — explain why (client-only SEP, etc.).
- **Should track** — affects runtime but neither implemented nor documented.

The skill must not invent a SEP list — read the upstream index every run.

## Step 8 — Cross-component coherence

Several places encode protocol assumptions; they must agree.

```bash
# Default protocol version must match across the agent adapter, the CLI doctor
# probe, and the docs.
PIN_AGENT="$(grep -oE '"20[0-9]{2}-[0-9]{2}-[0-9]{2}"' internal/agentadapter/config.go | head -1)"
PIN_DOCTOR="$(grep -oE '"20[0-9]{2}-[0-9]{2}-[0-9]{2}"' internal/cli/cluster/doctor_impl.go | head -1)"
PIN_DOCS="$(grep -oE '20[0-9]{2}-[0-9]{2}-[0-9]{2}' docs/getting-started.md | head -1)"
echo "agent=$PIN_AGENT doctor=$PIN_DOCTOR docs=$PIN_DOCS"
[ "$PIN_AGENT" = "\"$PIN_DOCS\"" ] && [ "$PIN_AGENT" = "$PIN_DOCTOR" ] \
  || echo "FAIL: protocol version drift across agent adapter / doctor / docs"
```

Also check the example servers and SDK pin:

```bash
grep -nE 'modelcontextprotocol/(go-sdk|python-sdk|typescript-sdk)\b' \
  examples/*/go.mod examples/*/requirements.txt examples/*/package.json 2>/dev/null
```

A runtime that pins `2025-06-18` but ships an example using an older SDK
that does not yet emit that version is a documentation finding (the
contributor flow will fail before the audit even starts).

## Step 9 — Forward-compatibility probe (optional)

Validate the same fixtures against `schema/draft/schema.json` to see what
breaks under the upcoming revision. Findings here are **Info** unless the
runtime explicitly claims draft support.

```bash
DRAFT_SCHEMA="$(fetch 'schema/draft/schema.json')"
fail=0
shopt -s nullglob
draft_inputs=()
test -n "${FIXTURE_TMP:-}" && draft_inputs+=("$FIXTURE_TMP"/*.json)
test -n "${MCP_SPEC_TMP:-}" && draft_inputs+=("$MCP_SPEC_TMP"/mcp-init.json "$MCP_SPEC_TMP"/mcp-tools.json "$MCP_SPEC_TMP"/mcp-call.json)
for f in "${draft_inputs[@]}"; do
  [ -s "$f" ] || continue
  "$JV" "$DRAFT_SCHEMA" "$f" >/dev/null 2>&1 || fail=$((fail+1))
done
echo "Draft-schema validation failures: $fail (informational)"
```

## Step 10 — Severity rubric (compliance-specific)

Map findings to `_shared/FINDINGS-TEMPLATE.md` severities using this
domain-specific rubric. Pick the highest that applies:

- **Critical** — Runtime claims protocol version X, but a conforming X
  client cannot complete `initialize` + `tools/call` against it; or the
  gateway corrupts/strips spec-required headers; or the runtime emits
  responses that fail schema validation against the version it advertised.
- **High** — Required behavior missing (no `Mcp-Session-Id`, no
  `DELETE` session handling, wrong JSON-RPC error codes, capability
  advertised but corresponding method returns -32601). Or capability NOT
  advertised but gateway accepts/forwards the method anyway.
- **Medium** — Optional spec feature documented as supported but
  observably broken; cross-component protocol-version drift between agent
  adapter, doctor probe, docs, and example SDKs.
- **Low** — Hardening for upcoming SEPs the runtime says it tracks;
  inconsistent error-shape choice (mixing `error.code` and
  `result.isError` for similar failures).
- **Info** — Draft-schema failures against fixtures that conform to the
  pinned revision; SEPs worth tracking that the runtime is silent on.

## Step 11 — Report

Use the structure in `_shared/FINDINGS-TEMPLATE.md`. The report header must
state the pinned spec revision, the upstream `SPEC_REF` and commit SHA used
for the fetch, and the runtime commit SHA under audit — none of those
findings are interpretable without that triple.

Required report sections, in order:

1. **Spec triple** — `SPEC_REV`, `SPEC_REF + upstream SHA`, runtime commit
   SHA.
2. **Claims inventory** — every place the runtime advertises a version or
   capability, with file:line.
3. **Static conformance** — schema validation pass/fail per envelope.
4. **Live conformance** — per sub-step (5a–5f), command + expected vs
   observed.
5. **Capability vs implementation matrix** — Step 6 output.
6. **SEP / roadmap cross-reference** — Step 7 table.
7. **Cross-component coherence** — Step 8.
8. **Forward-compatibility** — Step 9 (informational).
9. **Findings** — ordered by severity per the rubric in Step 10.
10. **Checks skipped** — what and why (no cluster, fetch blocked,
    upstream schema layout changed).

Cross-link from each finding to the runtime file:line **and** to a stable
upstream URL (anchor in `docs/specification/$SPEC_REV/...` or a line in the
schema) so a reviewer can audit the citation themselves without re-running
the skill.

## When NOT to use this skill

- Auth/policy/audit regression hunting → `qa-e2e-security`.
- Operator/CLI/setup regression hunting → `qa-e2e-operations`.
- UI/dashboard regression hunting → `qa-e2e-ui`.
- Performance regression → `qa-e2e-perf`.
- Cluster RBAC/PSS/NetworkPolicy hygiene → `k8s-hardening-audit`.
- Static repo security review → `security-audit` / `security-audit-platform`.

Use this skill specifically when the question is **"does what we ship match
what the MCP spec says we ship?"**
