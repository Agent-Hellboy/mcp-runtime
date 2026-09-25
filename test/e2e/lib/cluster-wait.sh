#!/usr/bin/env bash
# Shared cluster-convergence helpers for the Staging E2E runners.
#
# A freshly installed k3s reports a Ready node well before it is usable: the
# kubeconfig lands before the kubelet registers, and the bundled Traefik is
# applied by helm-controller afterwards. Every helper here exists because a
# real run raced one of those steps.
#
# The caller must define `log` and `fail`, and must already have KUBECONFIG
# pointing at the target cluster.

# `kubectl wait --all` does not wait for a resource to appear: it exits non-zero
# with "no matching resources found" the moment the selector matches nothing.
# Poll for registration before asking for readiness.
wait_for_node_registration() {
  local timeout="${1:-180}"
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if kubectl get nodes -o name 2>/dev/null | grep -q .; then
      return 0
    fi
    sleep 3
  done
  fail "no Kubernetes node registered within ${timeout}s"
  return 1
}

traefik_namespace() {
  local candidate
  for candidate in kube-system traefik; do
    if kubectl -n "${candidate}" get deploy traefik >/dev/null 2>&1; then
      printf '%s' "${candidate}"
      return 0
    fi
  done
  return 1
}

# Mirrors the doctor "traefik service exposure" check, which accepts either a
# LoadBalancer address or a NodePort for the web entrypoint.
traefik_is_exposed() {
  local namespace="$1" address ports
  address="$(kubectl -n "${namespace}" get svc traefik \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)"
  [[ -n "${address}" ]] && return 0
  ports="$(kubectl -n "${namespace}" get svc traefik -o jsonpath='{.spec.ports[*].nodePort}' 2>/dev/null)"
  [[ -n "${ports// /}" ]]
}

wait_for_traefik() {
  local timeout="${1:-300}"
  local deadline=$((SECONDS + timeout))
  local namespace=""
  log "waiting for the bundled Traefik ingress to become ready"
  while ((SECONDS < deadline)); do
    if namespace="$(traefik_namespace)" &&
      kubectl -n "${namespace}" rollout status deploy/traefik --timeout=30s >/dev/null 2>&1; then
      break
    fi
    namespace=""
    sleep 5
  done
  if [[ -z "${namespace}" ]]; then
    fail "bundled Traefik did not become ready within ${timeout}s"
    return 1
  fi
  log "Traefik deployment is ready in namespace ${namespace}"

  # Exposure is what ACME HTTP-01 needs. Warn rather than abort: setup still
  # gets a chance to wire ingress, and post-setup diagnostics is the real gate.
  while ((SECONDS < deadline)); do
    if traefik_is_exposed "${namespace}"; then
      log "Traefik web entrypoint is externally exposed"
      return 0
    fi
    sleep 5
  done
  log "WARNING: Traefik has no LoadBalancer address or NodePort yet; continuing into setup"
}
