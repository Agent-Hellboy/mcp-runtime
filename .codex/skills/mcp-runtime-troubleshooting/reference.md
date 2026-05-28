# MCP Runtime troubleshooting reference

Symptom-oriented remedies extracted from the contributor runbook. Prefer `cluster doctor` and targeted fixes before full reinstall.

## Ingress and MCPServer

- **"ingressHost is required" (operator):** set `spec.ingressHost` on the `MCPServer`, or operator env `MCP_DEFAULT_INGRESS_HOST`, or `MCP_PLATFORM_DOMAIN` for `mcp.<domain>` defaults.
- **MCPServer stuck `PartiallyReady` with working ingress traffic:** default ingress readiness is strict and waits for `Ingress.status.loadBalancer.ingress[]`. For dev / NodePort-style controllers, set operator env `MCP_INGRESS_READINESS_MODE=permissive`. Keep `strict` for production LB-published ingress.
- **Port mismatch:** bundled workspace assistant listens on `8088` by default; align `MCPServer` `port` / `servicePort` and container `PORT` if overridden.
- **Ingress / routes:** `kubectl get ingress -A` — confirm paths match gateway and demo servers.
- **Custom ingress namespaces:** bundled Traefik watches `registry`, `mcp-sentinel`, `mcp-servers`, `mcp-servers-org`, `mcp-servers-public` only. Per-team namespaces (e.g. `mcp-team-acme`) need Traefik namespace watch, `traefik-watch` Role, and NetworkPolicy ingress allowance. Platform `team create` patches repo-managed `traefik/traefik`; k3s external Traefik sets `PLATFORM_TEAM_TRAEFIK_WATCH=disabled` by default. See `docs/multi-team.md`.
- **IngressClass:** default is `traefik` (`IngressClass` name `traefik`, controller `traefik.io/ingress-controller`). `cluster doctor` checks it exists.

## Auth and UI

- **Analytics 401:** use gateway/ingest URL and `INGEST_API_KEYS` from `mcp-sentinel-secrets`, not app env. Example: `ANALYTICS_INGEST_URL=http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events`.
- **Secret not found in workload namespace:** copy `mcp-sentinel-secrets` or use shared secret reference.
- **Dashboard / API 401:** admin `x-api-key` curl needs a key in both `API_KEYS` and `ADMIN_API_KEYS`; browser login uses `UI_API_KEY`. Roll API/UI after secret changes.
- **Dashboard 308 redirect loop in dev:** UI redirects HTTP→HTTPS when `X-Forwarded-Proto: http`. Set `UI_REQUIRE_HTTPS` on `mcp-sentinel-ui`: `false`/`off` when fronted by a non-TLS terminator on a real hostname.
- **Platform UI 404 / wrong host:** with `MCP_PLATFORM_DOMAIN`, verify `mcp-sentinel-platform-ui` and `mcp-sentinel-platform-observability` ingresses in `mcp-sentinel`, DNS to ingress IP, and `kubectl logs -n traefik deploy/traefik --tail=120`. Path-based `mcp-sentinel-gateway` works when platform domain is unset.

## Registry and images

- **Tenant `registry push` 500 / copy timeout:** CLI uses `POST /api/runtime/registry/push` with internal transfer URL + skopeo helper pod (not `pods/exec` stdin). Ensure `k8s/08-api-rbac.yaml` registry-push Role and `/internal/registry-push/tar/{token}`.
- **Private / HTTP in-cluster registry / k3s:** set `MCP_REGISTRY_*` before `server generate`; raise `MCP_DEPLOYMENT_TIMEOUT` for slow first pulls. See `k3s-public-ops` skill.
- **Sentinel ImagePullBackOff (registry host drift):** `MCP_REGISTRY_HOST` is public alias; for HTTPS public installs set `MCP_REGISTRY_ENDPOINT=registry.<domain>`. Node-pullable host must match cert/trust config.
- **Tenant MCPServer ImagePullBackOff:** `server generate` rewrites `spec.image` to `MCP_REGISTRY_PULL_HOST` / `MCP_REGISTRY_ENDPOINT`. Pull secrets attach to `mcp-workload` SA, not `default`.
- **registry-allow-ingress NetworkPolicy vs k3s Traefik:** apply `config/registry/overlays/compatibility/k3s` when Traefik runs in `kube-system`.
- **k3s ≥1.35 NetworkPolicy ipset stale:** new pods may not reach registry; compatibility overlay adds `ipBlock: 10.0.0.0/8` — patch if pod CIDR differs.
- **Prod registry 404 / pulls "not found":** `curl -k -i -H "x-api-key: $ADMIN_API_KEY" https://registry.<domain>/v2/` — expect 200 + `docker-distribution-api-version: registry/2.0`. Prod registry ingress must not use dev `pii-redactor@file`.
- **Prod MCP URLs:** prefer `https://mcp.<domain>/<server-name>/mcp` with `spec.publicPathPrefix` and matching `MCP_PATH`.

## TLS, DNS, cert-manager

- **Prod DNS / ACME:** `MCP_PLATFORM_DOMAIN=example.com` derives `registry.`, `mcp.`, `platform.` — all three DNS records must point at ingress IP; port 80 for HTTP-01. Verify with `getent hosts` and in-cluster `nslookup`.
- **cert-manager CRDs exist but TLS times out:** reinstall cert-manager pods if CRDs survived but workloads did not (`kubectl get pods -n cert-manager`).
- **Multiple kubeconfigs:** pass `--kubeconfig <path>` explicitly to `setup` — `KUBECONFIG` alone is insufficient for TLS/cert-manager client paths in setup.
- **Duplicate Traefik:** setup refuses second stack when k3s Traefik is active; use `--ingress none` for external ingress or remove duplicate.

## Platform domain (summary)

Full public TLS/DNS/ACME detail: `.codex/skills/mcp-runtime-platform-public/SKILL.md`.
