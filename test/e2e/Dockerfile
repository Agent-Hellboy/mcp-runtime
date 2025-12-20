FROM golang:1.24.11-bullseye

ENV DEBIAN_FRONTEND=noninteractive
ARG DOCKERCLI_VERSION=26.1.4

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg lsb-release iptables && \
    rm -rf /var/lib/apt/lists/*

# Install docker CLI (newer API than distro docker.io), kubectl, kind
RUN ARCH="$(dpkg --print-architecture)" && \
    if [ "${ARCH}" = "arm64" ] || [ "${ARCH}" = "aarch64" ]; then PLATFORM="aarch64"; else PLATFORM="x86_64"; fi && \
    curl -fsSLo /tmp/docker.tgz "https://download.docker.com/linux/static/stable/${PLATFORM}/docker-${DOCKERCLI_VERSION}.tgz" && \
    tar -C /usr/local/bin -xzf /tmp/docker.tgz --strip-components=1 docker/docker && \
    rm -f /tmp/docker.tgz && \
    curl -fsSLo /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.27.3/bin/linux/amd64/kubectl && \
    chmod +x /usr/local/bin/kubectl && \
    curl -fsSLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64 && \
    chmod +x /usr/local/bin/kind

WORKDIR /workspace

# Copy the repo so tests still run if bind mounts aren't available.
COPY . /workspace

# Use bash as default entrypoint; optionally mount repo at /workspace when running.
ENTRYPOINT ["/bin/bash"]
