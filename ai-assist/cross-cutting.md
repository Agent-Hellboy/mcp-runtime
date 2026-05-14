# Cross-cutting checks

"When you touch X, also check Y." Invariants that span multiple
components and are easy to miss because the connection is not obvious
from the diff alone. New entries follow `TEMPLATE.md`.

---

### Protocol version pin must stay aligned across the runtime

The MCP protocol version is pinned in several places. Bumping one
without the others creates the worst kind of bug — silent mismatch
between what the agent adapter advertises, what the CLI doctor probes
with, and what the docs tell contributors to use. When changing any of
these, change all of them in the same PR.

Cross-check on every bump:

- `internal/agentadapter/config.go: DefaultProtocolVersion`
- `internal/cli/cluster/doctor_impl.go` (the curl `-H "Mcp-Protocol-Version: ..."`)
- `docs/getting-started.md` (the `PROTO=...` example)
- `AGENTS.md` (the documented test request)
- example servers' SDK pins (`examples/*/go.mod`, `requirements.txt`,
  `package.json`) — the SDK must support the version being pinned

The `mcp-spec-compliance` skill in `.codex/skills/` has a check
that flags drift across these.

Added: 2026-05-12

---

### Sentinel service image change → set image + roll, do not rerun `setup`

When iterating on `services/api`, `services/ui`, `services/ingest`, or
`services/processor`, the contributor loop is: build a new tag, push
to the bundled registry, `kubectl set image`, `rollout status`. Do not
rerun `setup --test-mode` per service edit — that rebuilds and
republishes the full stack, which is slow and unnecessary.

`services/mcp-proxy` is the exception: it ships as the `mcp-gateway`
sidecar, injected by the operator from `MCP_GATEWAY_PROXY_IMAGE`. To
test proxy changes, push the new image, update the operator env,
restart the operator, then **recreate** (not just restart) the affected
MCP server pods so the new sidecar image is injected.

References:
- `AGENTS.md` → **Iterate on one Sentinel service** (the canonical recipe)
- `docs/getting-started.md#iterate-on-one-sentinel-service`

Added: 2026-05-12

---

### CLI help text edits → expect golden file updates

Cobra command descriptions, subcommands, flags, and defaults are
snapshotted in `test/golden/cli/testdata/*.golden`. Any change in
`internal/cli/<command>/` that affects help output will fail golden
tests until the snapshots are regenerated. Run the golden test suite
locally before pushing; intentional updates land in the same PR.

References:
- `AGENTS.md` → **Docs sync for CLI help**
- `docs/internals/tests.md` (golden regeneration command)
- `test/golden/cli/testdata/`

Added: 2026-05-12

---

### `MCP_PLATFORM_DOMAIN` change → three DNS names + three TLS secrets

Setting `MCP_PLATFORM_DOMAIN=example.com` derives `registry.example.com`,
`mcp.example.com`, and `platform.example.com`. **All three** public
DNS records (A/AAAA or CNAME) must point to the ingress IP before
cert-manager can complete HTTP-01. Forgetting `platform.<domain>` is
the most common breakage; the dashboard 404s and contributors think
the install is broken.

References:
- `AGENTS.md` → **Platform domain and TLS (short)**
- `AGENTS.md` → **Production registry and TLS (debugging)**
- `config/cert-manager/`

Added: 2026-05-12
