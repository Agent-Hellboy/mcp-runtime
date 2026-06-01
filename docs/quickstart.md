# Quickstart

Deploy a governed MCP server and connect your first MCP client in under 10 minutes —
no Kubernetes cluster required. This quickstart uses the live
[platform.mcpruntime.org](https://platform.mcpruntime.org) instance.

To self-host MCP Runtime on your own cluster, see [Getting Started](getting-started.md).

---

## 1. Install the CLI

=== "macOS (Apple Silicon)"

    ```bash
    curl -Lo mcp-runtime https://github.com/Agent-Hellboy/mcp-runtime/releases/latest/download/mcp-runtime-darwin-arm64
    chmod +x mcp-runtime
    sudo mv mcp-runtime /usr/local/bin/
    ```

=== "macOS (Intel)"

    ```bash
    curl -Lo mcp-runtime https://github.com/Agent-Hellboy/mcp-runtime/releases/latest/download/mcp-runtime-darwin-amd64
    chmod +x mcp-runtime
    sudo mv mcp-runtime /usr/local/bin/
    ```

=== "Linux (amd64)"

    ```bash
    curl -Lo mcp-runtime https://github.com/Agent-Hellboy/mcp-runtime/releases/latest/download/mcp-runtime-linux-amd64
    chmod +x mcp-runtime
    sudo mv mcp-runtime /usr/local/bin/
    ```

=== "Linux (arm64)"

    ```bash
    curl -Lo mcp-runtime https://github.com/Agent-Hellboy/mcp-runtime/releases/latest/download/mcp-runtime-linux-arm64
    chmod +x mcp-runtime
    sudo mv mcp-runtime /usr/local/bin/
    ```

=== "Windows"

    Download [mcp-runtime-windows-amd64.exe](https://github.com/Agent-Hellboy/mcp-runtime/releases/latest/download/mcp-runtime-windows-amd64.exe) and add it to your `PATH`.

Verify:

```bash
mcp-runtime --version
```

---

## 2. Log in

Get credentials from the [live platform](https://platform.mcpruntime.org) or use
an existing account. You will need a team and a user account — ask your platform
admin, or [self-host MCP Runtime](getting-started.md) to create your own.

```bash
mcp-runtime auth login \
  --api-url https://platform.mcpruntime.org \
  --email you@example.com --password '...' \
  --profile me

mcp-runtime auth status    # confirm the profile is active
```

---

## 3. Deploy an example server

Clone the repo to get the example server source:

```bash
git clone https://github.com/Agent-Hellboy/mcp-runtime
cd mcp-runtime/examples/workspace-assistant-mcp
```

Run it locally to discover its tool names, then scaffold the metadata:

```bash
go run . &
SERVER_PID=$!

mcp-runtime server init workspace-demo \
  --from-server http://localhost:8088
# Discovered: aaa-ping, add, create_task, echo, lower, slugify, upper

kill $SERVER_PID
```

Validate the metadata, build the image, push it, and deploy:

```bash
mcp-runtime server validate --metadata-dir .mcp

mcp-runtime server build image workspace-demo --tag v1
# Prints the exact image ref, e.g.: registry.mcpruntime.org/myteam/workspace-demo:v1

mcp-runtime registry push \
  --image registry.mcpruntime.org/myteam/workspace-demo:v1 \
  --scope tenant

mcp-runtime server deploy workspace-demo --scope tenant --metadata-dir .mcp
```

Confirm it is running:

```bash
mcp-runtime server list
```

---

## 4. Grant access and connect

Create a grant that allows an agent to call `echo` and `add`:

```bash
mcp-runtime access grant init workspace-cursor \
  --server workspace-demo \
  --namespace mcp-team-myteam \    # replace myteam with your actual team slug
  --agent-id cursor \
  --tool echo \
  --tool add \
  --output grant.yaml

mcp-runtime server validate --metadata-dir .mcp --grant-file grant.yaml
mcp-runtime access grant apply --file grant.yaml
```

Start the adapter proxy — it creates and refreshes the agent session automatically:

```bash
mcp-runtime adapter proxy \
  --runtime-url https://mcp.mcpruntime.org/workspace-demo/mcp \
  --server workspace-demo \
  --agent cursor \
  --agent-id cursor \
  --auto-refresh \
  --listen 127.0.0.1:8099
```

Point **Claude Desktop**, **Cursor**, or any MCP client at `http://127.0.0.1:8099`.
Call the `echo` or `add` tool — the gateway enforces the grant on every call.

---

## 5. See it in the analytics

Open [platform.mcpruntime.org](https://platform.mcpruntime.org), go to
**Analytics → Tools** tab. You will see your tool calls broken down by
user, team, agent, call count, and allow/deny.

---

## What's next

- [Concepts](concepts.md) — understand Grants, Sessions, Trust levels, and Side effects
- [Publish an MCP Server](publish-mcp-server.md) — full build, push, deploy guide
- [Getting Started](getting-started.md) — self-host MCP Runtime on your own Kubernetes cluster
- [CLI reference](cli.md) — every command with flags and examples
