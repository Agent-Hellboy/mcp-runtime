# Gotchas

Non-obvious behavior that has already cost a session. New entries follow
`TEMPLATE.md`. Keep it tight; promote to `AGENTS.md` if it becomes
universal contributor knowledge.

---

### `CLAUDE.md` is a symlink to `AGENTS.md`

The repo root has `CLAUDE.md -> AGENTS.md`. Editing `CLAUDE.md` directly
works (follows the link), but the canonical filename is `AGENTS.md`.
Diffs and commits should reference `AGENTS.md`; PR reviewers will look
there. If a contributor adds `CLAUDE.md` as a separate file, that's a
bug — the symlink should be restored.

References:
- `CLAUDE.md` (symlink target)
- `AGENTS.md` (canonical)

Added: 2026-05-12

---

### Proxy sidecar reloads policy on a polling loop, not on apply

After `kubectl apply` of an `MCPAccessGrant` or `MCPAgentSession`,
`./bin/mcp-runtime server policy inspect` may already show the rendered
policy while the `mcp-gateway` sidecar is still on the prior version.
Allow ~6–10 seconds after the rendered policy reflects the change
before concluding a fresh `tools/call` failed for governance reasons —
the proxy reads the mounted file on a short poll interval.

References:
- `AGENTS.md` → **MCP server pod / sidecar checks**
- `docs/getting-started.md#3-contributor-test-mode-cluster` (the
  documented `sleep 6` after policy materialization)

Added: 2026-05-12

---

### `setup --test-mode` still builds and pushes images

Test mode is **not** a no-build shortcut. `./bin/mcp-runtime setup
--test-mode` builds and pushes the operator, gateway proxy, and Sentinel
images (with `latest` tags) to the configured or bundled registry, then
deploys pods that pull those images. Plan for the disk/CPU cost of a
full build whenever this command runs; don't expect "just deploy from
existing images."

References:
- `AGENTS.md` → **Local dev setup (Kind and CLI)**
- `docs/getting-started.md#3-contributor-test-mode-cluster`

Added: 2026-05-12

---

### Bundled registry TLS does not configure node trust

`setup --registry-mode bundled-https` makes the registry pod serve HTTPS
and creates the `cert-manager/mcp-runtime-ca` Secret when the built-in issuer is used,
but kubelet still pulls through the node's container runtime. Nodes must
be able to resolve or mirror the registry host and trust the issuing CA; the
cluster-side Secret, Certificate, and Service changes alone do not update
containerd, k3s, Docker, or host DNS.

References:
- `docs/cluster-readiness.md` → **Registry setup modes**
- `config/registry/overlays/internal-tls/`

Added: 2026-05-20

---

### Workspace assistant sample image is distroless — no shell

`kubectl exec -it <pod> -c workspace-assistant-mcp -- /bin/sh` fails on the
workspace assistant sample because the image is distroless. Same caveat
applies to several other runtime images. Use `kubectl logs`, `kubectl
describe`, or `kubectl debug --image=busybox:1.36 --target=<container>`
to inspect the pod namespace instead of expecting an interactive shell.

References:
- `examples/go-mcp-server/Dockerfile`
- `AGENTS.md` → **MCP server pod / sidecar checks**

Added: 2026-05-12
