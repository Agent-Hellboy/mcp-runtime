---
name: mcp-runtime-troubleshooting
description: Debug MCP Runtime cluster, ingress, registry, Sentinel auth, MCPServer pods, and platform UI failures on Kind or k3s. Use when setup, doctor, e2e, or live traffic fails; when investigating 401/404/ImagePullBackOff/ACME/TLS/registry push errors; or when AGENTS.md points here for the full failure-mode checklist.
---

# MCP Runtime — cluster troubleshooting

## When to use

- Live cluster misbehaves after `setup`, upgrade, or config change
- Operator, gateway, registry, or Sentinel symptoms (not unit-test failures)
- Before re-running full `setup`, scan the checklist for a targeted fix

## First steps

```bash
kubectl config current-context
./bin/mcp-runtime status
./bin/mcp-runtime cluster doctor
kubectl get ingress -A
kubectl get pods -A | grep -E 'mcp-|registry|traefik|cert-manager'
```

For Kind contributor clusters, prefer reusing `kind-mcp-runtime` — see `.codex/skills/qa-cluster-bringup/SKILL.md`.

For public k3s / `mcpruntime.org` deploys, also read `.codex/skills/k3s-public-ops/SKILL.md` and `docs/cluster-readiness.md`.

## Full checklist

Read **[reference.md](reference.md) end-to-end** before diagnosing (ingress, registry, cert-manager, ImagePullBackOff, UI redirect loops, registry push timeouts, k3s NetworkPolicy, duplicate Traefik, and more). Public TLS/DNS detail: `mcp-runtime-platform-public` skill.

## MCPServer pod / gateway sidecar

When a server is deployed but grants, policy, or analytics look wrong:

```bash
SERVER=workspace-assistant-mcp
CONTAINER=workspace-assistant-mcp
NS=mcp-servers

kubectl get mcpservers -n "$NS"
POD="$(kubectl get pods -n "$NS" -l app="$SERVER" -o jsonpath='{.items[0].metadata.name}')"
kubectl describe pod -n "$NS" "$POD"
kubectl logs -n "$NS" "$POD" -c "$CONTAINER"
kubectl logs -n "$NS" "$POD" -c mcp-gateway
./bin/mcp-runtime server policy inspect "$SERVER" --namespace "$NS"
kubectl get mcpaccessgrant,mcpagentsession -n "$NS" -o wide
```

Sidecar container name is `mcp-gateway`. Many images are distroless — use logs/describe or:

```bash
kubectl debug -it -n "$NS" "pod/$POD" --target="$CONTAINER" --image=busybox:1.36 -- sh
```

Policy reload: the gateway sidecar polls its policy file; wait a few seconds after applying grants/sessions before concluding `session_not_found`.

## Clean start (keep cluster, wipe workloads)

**Destructive** to application namespaces. From repo root:

```bash
to_delete="$(kubectl api-resources --verbs=delete --namespaced -o name | paste -sd, -)"
[ -n "$to_delete" ] && kubectl delete "$to_delete" --all -A --ignore-not-found --grace-period=0 --force
for r in $(kubectl api-resources --verbs=delete --namespaced=false -o name); do
  kubectl delete "$r" --all --ignore-not-found --grace-period=0 --force || true
done
ns_to_delete="$(kubectl get ns --no-headers | awk '{print $1}' | grep -E -v '^(kube-system|kube-public|kube-node-lease|default)$')"
[ -n "$ns_to_delete" ] && printf '%s\n' "$ns_to_delete" | xargs kubectl delete ns
```

Then rerun `setup` or contributor bring-up per `qa-cluster-bringup`.
