# Production-mode E2E on the disposable VM

The repository contains an on-demand GitHub Actions workflow, `Production E2E
(Disposable VM)`, for exercising the same CLI and HTTP flows used against a
real installation. It is intentionally separate from the Kind suite: the
workflow installs k3s on the dedicated VM, runs pre-setup `cluster doctor`,
runs production-style `setup`, runs post-setup `cluster diagnostics`, checks
the CLI/API/UI surfaces, and optionally runs the destructive multi-team suite.

The workflow requires these GitHub Actions secrets:

| Secret | Meaning |
| --- | --- |
| `E2E_VM_HOST` | Dedicated disposable VM hostname or address |
| `E2E_VM_USER` | SSH user with permission to install k3s |
| `E2E_VM_PASSWORD` | SSH password for that VM |
| `E2E_VM_KNOWN_HOSTS` | Pinned `known_hosts` line(s) for the VM SSH host key |

The VM must have the wildcard DNS record configured for the four setup hosts:

```text
*.e2e.mcpruntime.org
```

Populate `E2E_VM_KNOWN_HOSTS` from a host-key fingerprint verified against the
VM provider console, for example `ssh-keyscan -H <verified-vm-host>`. The
workflow rejects unknown or changed host keys and never uses host-key bypasses.

Keep E2E-only values in `/var/lib/mcp-runtime-e2e-backup/e2e.env`, outside the
checkout and outside every cleanup path:

```bash
E2E_ACME_EMAIL=operations@example.com
E2E_PLATFORM_API_TOKEN=replace-with-an-e2e-only-admin-key
E2E_WITH_MCP_AUTH=1
E2E_MCP_AUTH_ISSUER_URL=https://auth.e2e.mcpruntime.org/realms/mcp
E2E_MCP_AUTH_CONNECTOR=keycloak
```

The connector name is only an E2E configuration choice. MCP Runtime's auth
server is provider-agnostic and can load any configured identity-provider
connector; Keycloak is useful here because it gives the test an isolated OIDC
issuer and test user.

If certificates, registry credentials, or an identity-provider installation
must survive a VM reset, place encrypted or otherwise access-controlled backup
material in the same directory. The runner supports either an executable
`restore.sh` hook or declarative files below `manifests/`. Cleanup removes the
disposable k3s state and working directory only; it never removes the backup
directory. Failed runs leave cluster snapshots and logs in
`/var/lib/mcp-runtime-e2e-backup/runs/<run-id>` and upload them as workflow
artifacts.

Run the workflow manually after changing setup, registry, auth, ingress,
doctor/diagnostics, or CLI behavior. Use the input to disable the destructive
multi-team scenario while iterating on a narrower failure.

The local Kind suite remains the fast test-mode path:

```bash
E2E_VALIDATE_SCENARIOS_ONLY=1 E2E_SCENARIOS=all bash test/e2e/kind.sh
```

That selector validation is safe on a developer machine. A full Kind run is
still required for changes that affect reconciliation, image pulls, ingress,
or persistent storage.
