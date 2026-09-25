#!/usr/bin/env bash
set -Eeuo pipefail

# Runner-side guard for the Staging E2E disposable VM.
#
#   staging-target.sh check
#       Refuse (exit 1) unless the configured target is the disposable VM:
#       every E2E hostname ends in the disposable suffix, no E2E hostname or
#       the VM host shares an address with the production hostnames (resolved
#       by DNS now), the E2E hostnames point at the VM, and the VM carries the
#       disposable marker written by `bootstrap`. The workflows run this before
#       they copy anything to, or run anything on, the VM.
#
#   E2E_CONFIRM_DISPOSABLE_VM=<vm host> staging-target.sh bootstrap
#       One-time, explicit step that writes the marker on a freshly provisioned
#       disposable VM, after the same DNS checks pass. The confirmation value
#       must repeat E2E_VM_HOST exactly. Never run it against production.
#
# Required: E2E_VM_HOST. Optional: E2E_VM_USER (root), SSHPASS or an SSH
# agent/key, E2E_VM_KNOWN_HOSTS, E2E_BACKUP_DIR, E2E_{PLATFORM,MCP,AUTH}_URL,
# E2E_REGISTRY_HOST, E2E_DISPOSABLE_DOMAIN_SUFFIX, E2E_PRODUCTION_DOMAIN.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAGING_LOG_PREFIX=staging-target
# shellcheck source=test/e2e/lib/staging.sh
source "${ROOT_DIR}/test/e2e/lib/staging.sh"

VM_HOST="${E2E_VM_HOST:?set E2E_VM_HOST to the disposable VM address}"
VM_USER="${E2E_VM_USER:-root}"
PLATFORM_URL="${E2E_PLATFORM_URL:-https://platform.e2e.mcpruntime.org}"
MCP_URL="${E2E_MCP_URL:-https://mcp.e2e.mcpruntime.org}"
REGISTRY_HOST="${E2E_REGISTRY_HOST:-registry.e2e.mcpruntime.org}"
AUTH_URL="${E2E_AUTH_URL:-https://auth.e2e.mcpruntime.org}"
E2E_HOSTS=(
  "$(staging_url_host "${PLATFORM_URL}")"
  "$(staging_url_host "${MCP_URL}")"
  "$(staging_url_host "${REGISTRY_HOST}")"
  "$(staging_url_host "${AUTH_URL}")"
)
staging_ssh_init

case "${1:-check}" in
  check)
    staging_verify_remote_target "${E2E_HOSTS[@]}"
    ;;
  bootstrap)
    if [[ "${E2E_CONFIRM_DISPOSABLE_VM:-}" != "${VM_HOST}" ]]; then
      staging_err "bootstrap needs E2E_CONFIRM_DISPOSABLE_VM set to the exact E2E_VM_HOST value"
      exit 1
    fi
    staging_guard_hosts "${VM_HOST}" "${E2E_HOSTS[@]}"
    machine="$(staging_vm_ssh 'hostname' | tr -d '\r' | head -n 1)"
    [[ -n "${machine}" ]] || {
      staging_err "could not read the VM hostname over SSH"
      exit 1
    }
    if staging_production_machine_names | grep -Fxq -- "${machine}"; then
      staging_err "refusing to bootstrap: ${machine} is a production machine name"
      exit 1
    fi
    marker="$(staging_marker_path)"
    staging_marker_content "${machine}" |
      staging_vm_ssh "set -eu; umask 077; install -d -m 700 '$(dirname "${marker}")'; cat >'${marker}.tmp'; mv '${marker}.tmp' '${marker}'"
    staging_log "wrote disposable marker ${marker} on ${machine}"
    staging_verify_remote_target "${E2E_HOSTS[@]}"
    ;;
  *)
    echo "usage: $0 check|bootstrap" >&2
    exit 2
    ;;
esac
