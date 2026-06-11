# mcpruntime.org k3s deployment scripts

Env file: `config/deployments/mcpruntime-org.env` (see `.example`).

| Script | Purpose |
|--------|---------|
| `setup.sh` | Build CLI, run `mcp-runtime setup`, auto-restore platform backup when present |
| `clean.sh --yes` | Backup TLS/OIDC/bootstrap secrets, delete MCP Runtime namespaces |
| `restore.sh` | Re-apply platform-runtime backup (TLS, certs, config) |
| `rollout.sh` | Build/push Sentinel API+UI images and roll deployments |
| `multitenancy-test.sh` | End-to-end multi-tenant demo via platform API only |

Shared helpers live in `lib/`:

| Library | Responsibility |
|---------|----------------|
| `lib/env.sh` | Load deployment env, kubeconfig, registry host |
| `lib/backup.sh` | Snapshot and restore platform-runtime state |
| `lib/clean.sh` | Namespace selection and cluster-scoped CR cleanup |
| `lib/registry.sh` | Registry pull secret and local skopeo push via port-forward |

Runbook: `docs/k3s-deployment-runbook.md`
