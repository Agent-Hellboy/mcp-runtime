# Cluster Readiness

`./bin/mcp-runtime setup` installs the platform (registry, operator, ingress, sentinel) into an *already-running* Kubernetes cluster. It does **not** configure the node's container runtime or host DNS stack. Those are prerequisites that differ per distribution.

If you skip this, you'll typically see:

- `./bin/mcp-runtime setup` fails at "Publish runtime images" with `dial tcp: lookup registry.local: no such host`.
- The operator pod goes to `ImagePullBackOff` with `10.43.x.x:5000: connection refused` or `no such host`.
- MCPServer pods get stuck in `ImagePullBackOff` pulling `registry.local/<server-name>`.

This document lists what each distribution needs before you run `setup`.

---

## Dev vs production readiness

Most examples in this page describe the **dev/local** path: the bundled Docker
registry exposed by `NodePort`, HTTP registry access, `registry.local`, and
node-level insecure-registry or mirror configuration. That path is appropriate
for kind, minikube, Docker Desktop, single-node k3s, CI, and disposable test
clusters where the goal is quick image push/pull validation.

Production clusters use the same readiness model, but should make different
platform choices:

| Area | Dev / local default | Production expectation |
|---|---|---|
| Registry | Bundled Docker registry exposed through `NodePort` and `registry.local` | Managed or hardened registry such as ECR, Artifact Registry, ACR, Harbor, or an internal registry behind stable DNS |
| Transport | HTTP or an insecure registry mirror for fast iteration | HTTPS with a trusted certificate; use `setup --with-tls` when retaining the bundled registry |
| Node trust | `/etc/hosts`, containerd mirror entries, and Docker Desktop insecure-registry settings | Platform-managed DNS, trusted CAs, image pull policy, and audited runtime config across every node pool |
| Image credentials | Local helper flows and default service account image pull secrets | Dedicated pull secrets, workload identity, or cloud-native node registry auth |
| Ingress | Bundled Traefik overlay is acceptable | Existing platform ingress or gateway may be preferred; validate ingress class, TLS, DNS, and policy ownership |
| Persistence | Default registry storage is acceptable for throwaway clusters | Storage class, backup/restore, quota, retention, and registry HA need explicit owner decisions |
| Sentinel stack | Bundled stack is useful for development and demos | Size, retention, security, and observability integration should be reviewed before production use |

Do **not** treat `insecure_skip_verify`, HTTP registries, wildcard insecure CIDR
ranges, or manual `/etc/hosts` edits as production guidance. They are local
workarounds for kubelet image pulls. In production, prefer a registry name that
resolves through normal DNS and is trusted by every node without bypassing TLS
verification.

`./bin/mcp-runtime cluster doctor` is useful in both modes, but some checks are
dev-registry oriented today: it expects the bundled `registry/registry` Service
and a NodePort. If you run with a provisioned external registry, interpret those
registry-specific failures against your registry architecture instead of copying
the local workaround literally.

Production readiness checklist:

- Choose the registry architecture: bundled registry with TLS, or a managed /
  hardened external registry.
- Ensure every node pool that can schedule MCP Runtime workloads can pull the
  operator, gateway proxy, Sentinel, and MCP server images.
- Configure image credentials with pull secrets, workload identity, or
  cloud-native node registry auth.
- Confirm the ingress class, public DNS, and TLS issuer are owned by the
  platform team and match the cluster's ingress controller.
- Confirm a default `StorageClass` exists, or set explicit storage choices for
  the registry and Sentinel data paths.
- Decide certificate management up front: Let's Encrypt with public DNS,
  `--tls-cluster-issuer` for an enterprise CA, or a preinstalled issuer.
- Review Sentinel sizing, retention, auth, and observability integration before
  enabling it on production traffic.
- Run `./bin/mcp-runtime setup --with-tls --strict-prod` for production-style
  validation, or document why a non-strict setup is intentional.

---

## Why these prerequisites exist

The registry runs as a Kubernetes `Service` of type `NodePort` (default `5000:32000/TCP`) with an `Ingress` at `registry.local`.

Three *different* actors fetch images, and they resolve hostnames differently:

| Actor | What it pulls | DNS source |
|---|---|---|
| `./bin/mcp-runtime registry push` (in-cluster mode) | Pushes from a helper pod using the registry Service DNS | Cluster CoreDNS (always works) |
| `kubelet` on the node | Pulls operator / MCPServer images for pod creation | **Host DNS** (not CoreDNS) + containerd registry mirrors |
| Developer `docker push` / `docker pull` | Ad-hoc pushes or pulls from your laptop | Your local `/etc/hosts` / corporate DNS |

The in-cluster push path is handled by the CLI (`PushInCluster` rewrites the destination to the service DNS). The developer path is your local concern. **The node/kubelet path is what the distribution-specific config below is for.**

---

## External registry path

Use an external registry when your platform already provides ECR, Artifact
Registry, ACR, Harbor, or another hardened registry with DNS, TLS, retention,
and auth controls. In that mode, `setup` skips installing the bundled registry
and configures the operator to use the provisioned registry.

Configure it either with the CLI:

```bash
./bin/mcp-runtime registry provision --url registry.example.com
```

or with environment variables before `setup`:

```bash
export PROVISIONED_REGISTRY_URL=registry.example.com
export PROVISIONED_REGISTRY_USERNAME=<user>      # optional
export PROVISIONED_REGISTRY_PASSWORD=<password>  # optional
```

Then run setup with production validation:

```bash
./bin/mcp-runtime setup --with-tls --strict-prod
```

Before generating MCP server manifests, set the image host that cluster nodes
should pull from:

```bash
export MCP_REGISTRY_INGRESS_HOST=registry.example.com
./bin/mcp-runtime pipeline generate --dir .mcp --output manifests
```

Use `MCP_REGISTRY_ENDPOINT` only when the operator needs a different internal
push/pull endpoint than the public image host. For example, a private registry
may expose `registry.example.com` to developers but an internal `host:port` to
cluster nodes. Keep image references, pull secrets, and node trust aligned with
the endpoint that kubelet actually uses.

External registry readiness checks:

- `docker login registry.example.com` succeeds from the build machine, if
  username/password auth is required.
- `kubectl get secret -A | grep -i pull` shows the expected image pull secret,
  or workload identity / node registry auth is configured by the platform.
- A test pod using an image from the registry reaches `Running` on every node
  pool that may run MCP Runtime workloads.
- `./bin/mcp-runtime status` reports the provisioned registry URL after setup.

## Strict production setup

`./bin/mcp-runtime setup --strict-prod` adds guardrails for production-style
installs. It is ignored in `--test-mode`; otherwise it requires:

- `--with-tls`.
- An external registry URL that is not dev-only, or a stable
  `MCP_REGISTRY_ENDPOINT` for the bundled registry.
- No default local registry endpoint such as `registry.local` for production
  registry validation.

Normal setup still allows local HTTP and internal registry flows so kind, k3s,
Docker Desktop, and CI remain easy to use. Use `--strict-prod` when the cluster
is intended to represent production or a production-like staging environment.

## DNS and TLS readiness

When `MCP_PLATFORM_DOMAIN=example.com` is set, setup derives these public names:

- `registry.example.com` for registry ingress.
- `mcp.example.com` for MCP server traffic.
- `platform.example.com` for the dashboard, API, Grafana, and Prometheus paths.

All configured public names must resolve to the cluster ingress address before
certificate issuance. For Let's Encrypt HTTP-01, port 80 must reach the ingress
controller from the public internet. For enterprise PKI, install the
`ClusterIssuer` first and pass it with:

```bash
./bin/mcp-runtime setup --with-tls --tls-cluster-issuer <issuer-name>
```

If you retain the bundled registry with TLS, make sure the registry certificate
covers the registry hostname nodes and build machines use. If you use an
external registry, the registry's own TLS and auth configuration are outside
the bundled cert-manager flow.

## Node pool consistency

Registry fixes must apply to every node pool that can schedule MCP Runtime
pods. A setup can look healthy on one node and fail later when a workload lands
on a different pool with missing DNS, missing CA trust, missing registry mirror
config, or missing cloud registry permissions.

For production, either standardize registry trust and auth across all eligible
node pools, or use taints, tolerations, labels, and node selectors so MCP
Runtime workloads only land on pools that have been prepared and audited.

---

## k3s

k3s uses embedded containerd. Point it at the registry NodePort on loopback (same node).

1. **Registry mirror.** Create `/etc/rancher/k3s/registries.yaml`:

   ```yaml
   mirrors:
     registry.local:
       endpoint:
         - "http://127.0.0.1:32000"
   configs:
     "127.0.0.1:32000":
       tls:
         insecure_skip_verify: true
   ```

   If the registry's ClusterIP (e.g. `10.43.39.164:5000`) or service DNS
   (`registry.registry.svc.cluster.local:5000`) ever appears as an image ref,
   add a mirror entry for it too — containerd does exact-host matching.

2. **Host DNS.** Add to `/etc/hosts`:

   ```text
   127.0.0.1 registry.local
   ```

3. **Reload.** `systemctl restart k3s`. k3s regenerates containerd's config from `registries.yaml` at startup.

Multi-node k3s: apply the same `/etc/rancher/k3s/registries.yaml` and `/etc/hosts` to every node — `127.0.0.1:32000` reaches the local kube-proxy which forwards to the registry pod regardless of where the pod is scheduled.

## kind

kind's nodes are containers, so the registry NodePort needs an `extraPortMappings` entry to be reachable, and containerd inside the node container needs the same mirror.

1. **Cluster config.** Pass this to `kind create cluster --config`:

   ```yaml
   kind: Cluster
   apiVersion: kind.x-k8s.io/v1alpha4
   containerdConfigPatches:
     - |-
       [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.local"]
         endpoint = ["http://127.0.0.1:32000"]
       [plugins."io.containerd.grpc.v1.cri".registry.configs."127.0.0.1:32000".tls]
         insecure_skip_verify = true
   nodes:
     - role: control-plane
       extraPortMappings:
         - containerPort: 32000
           hostPort: 32000
           protocol: TCP
   ```

2. **Host /etc/hosts** (on your laptop, so `docker push` / `curl` work):

   ```text
   127.0.0.1 registry.local
   ```

Alternative: `kind load docker-image <image>` sideloads without a registry at all — useful for throwaway tests, but bypasses the registry-push flow the CLI is built around.

## minikube

Two options.

**Option A — insecure registry flag at start time:**

```bash
minikube start --insecure-registry=registry.local --insecure-registry=10.43.39.164:5000
minikube addons enable ingress
echo "$(minikube ip) registry.local" | sudo tee -a /etc/hosts
```

The `--insecure-registry` flag is read-only on initial `start`. Re-creating the VM is required to change it.

**Option B — `minikube image load`:**

Skip the registry entirely and push images directly into the node's image store:

```bash
docker build -t registry.local/my-server:latest .
minikube image load registry.local/my-server:latest
```

Fine for quick iteration, but `./bin/mcp-runtime registry push` won't help — images bypass the registry.

## Docker Desktop (Kubernetes)

1. Docker Desktop → Settings → Docker Engine → add:

   ```json
   {
     "insecure-registries": ["registry.local", "10.96.0.0/12"]
   }
   ```

   Apply & Restart. Docker Desktop's embedded k8s shares the Docker daemon's registry config.

2. `/etc/hosts`:

   ```text
   127.0.0.1 registry.local
   ```

Reachability from the k8s nodes (which are VMs managed by Docker Desktop) is automatic because they share the host loopback for the NodePort via `127.0.0.1`.

## kubeadm / vanilla Kubernetes

For each node running kubelet:

1. Edit `/etc/containerd/config.toml`:

   ```toml
   [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.local"]
     endpoint = ["http://<registry-reachable-ip>:32000"]
   [plugins."io.containerd.grpc.v1.cri".registry.configs."<registry-reachable-ip>:32000".tls]
     insecure_skip_verify = true
   ```

   Pick `<registry-reachable-ip>` as whichever IP the node can reach the registry's NodePort on — typically the node's own IP or a load-balancer VIP.

2. `/etc/hosts` on each node: map `registry.local` to the same IP.

3. `systemctl restart containerd` on each node.

For production, use a stable registry endpoint with HTTPS and trusted node
configuration as described in [Dev vs production readiness](#dev-vs-production-readiness).

## Generic checks you can run

```bash
# Is the registry Service up?
kubectl get svc -n registry registry

# NodePort?
kubectl get svc -n registry registry -o jsonpath='{.spec.ports[0].nodePort}'

# From inside the cluster — should return a JSON repository list:
kubectl run -n registry --rm -it registry-check --restart=Never \
  --image=curlimages/curl --command -- \
  curl -s http://registry.registry.svc.cluster.local:5000/v2/_catalog

# From the node (SSH to a node first):
curl -s http://127.0.0.1:32000/v2/_catalog
getent hosts registry.local

# Preflight check via the CLI (see below)
./bin/mcp-runtime cluster doctor
```

## Failure-to-cause map

| Symptom | Likely cause | What to check |
|---|---|---|
| `lookup registry.local: no such host` during publish or image pull | Host or node DNS does not know `registry.local` | `/etc/hosts`, corporate DNS, `MCP_REGISTRY_INGRESS_HOST`, and the image host emitted by generated manifests |
| `server gave HTTP response to HTTPS client` | The registry is HTTP, but Docker/containerd is trying HTTPS | Insecure registry / mirror settings for dev, or switch to HTTPS with trusted certs for production |
| `ImagePullBackOff` with `401` or `403` | Registry auth is missing or invalid | Image pull secrets, service account references, workload identity, or cloud node registry permissions |
| `ImagePullBackOff` only on some nodes | Node pool config drift | Registry mirror config, CA trust, DNS, and registry permissions on every eligible node pool |
| `curl https://registry.<domain>/v2/` returns Traefik `404 page not found` | Ingress/router is not routing to the registry service | Registry `Ingress`, ingress class, Traefik logs, host rules, and TLS secret names |
| cert-manager reports `NXDOMAIN` | Public DNS is missing or misspelled | `getent hosts registry.<domain>`, `mcp.<domain>`, `platform.<domain>`, and DNS records from outside the cluster |
| `cluster doctor` reports missing bundled registry while using an external registry | Doctor is checking the local bundled-registry path | Validate your external registry path manually and treat that specific check as advisory |

## `bootstrap`

`./bin/mcp-runtime bootstrap` validates a smaller, mostly orthogonal set of prerequisites before `setup`:

- kubectl connectivity to the cluster.
- A CoreDNS deployment in `kube-system`.
- A default `StorageClass` (one annotated `storageclass.kubernetes.io/is-default-class=true`).
- The `traefik` `IngressClass`.
- The `metallb-system` namespace, if you plan to use MetalLB for `LoadBalancer` services.

Missing pieces are warnings, not errors — the command surfaces them so you can decide what to install with your standard platform tooling.

`bootstrap --apply --provider k3s` is the only automated apply path today: run it on the k3s server node and it applies the bundled CoreDNS and local-path manifests under `/var/lib/rancher/k3s/server/manifests`, then waits for both rollouts. Other providers (`rke2`, `kubeadm`, `generic`) print guidance instead.

## `cluster doctor`

`./bin/mcp-runtime cluster doctor` runs a preflight:

- Detects your distribution (k3s / kind / minikube / docker-desktop / generic).
- Checks the registry Service is present and has a NodePort.
- Verifies `registry.local` resolves from inside the cluster (cluster DNS).
- Prints the distribution-specific remediation checklist from this document.

Run it before `setup` on a fresh cluster, or when debugging `ImagePullBackOff`.
