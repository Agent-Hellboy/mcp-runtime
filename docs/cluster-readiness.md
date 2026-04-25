# Cluster Readiness

`./bin/mcp-runtime setup` installs the platform (registry, operator, ingress, sentinel) into an *already-running* Kubernetes cluster. It does **not** configure the node's container runtime or host DNS stack. Those are prerequisites that differ per distribution.

If you skip this, you'll typically see:

- `./bin/mcp-runtime setup` fails at "Publish runtime images" with `dial tcp: lookup registry.local: no such host`.
- The operator pod goes to `ImagePullBackOff` with `10.43.x.x:5000: connection refused` or `no such host`.
- MCPServer pods get stuck in `ImagePullBackOff` pulling `registry.local/<server-name>`.

This document lists what each distribution needs before you run `setup`.

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

For production, swap HTTP for HTTPS with a real cert via cert-manager (setup supports `--with-tls`) and drop the `insecure_skip_verify`.

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
