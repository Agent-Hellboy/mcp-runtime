---
title: "Understanding the MCP server-client request flow"
description: "A trace-driven walkthrough of Cursor's Streamable HTTP startup sequence using PyMCP Kit, from OAuth metadata probes through discovery to the tools Cursor exposes and calls in chat."
category: "MCP"
published: "2026-06-04"
reading_time: "7 min"
---

The lifecycle flow shown here is being removed in a near-future release, but it is still important because it explains the current client and server flow in the `2025-11-25` specification. When a new MCP server is added in Cursor, the first interesting thing is not a tool call. Cursor starts by probing the server shape, negotiating the MCP session, and discovering what the server exposes. The screenshots below capture both sides of that sequence for a simple local server built with [PyMCP Kit](https://github.com/Agent-Hellboy/py-mcp), the framework I am using while implementing the MCP spec and tracing real MCP request flows. The terminal screenshots show what the server receives over Streamable HTTP. The Cursor screenshots show what those protocol messages turn into inside the client.

PyMCP Kit is my capability-first MCP server toolkit for FastAPI. It supports Streamable HTTP and stdio, tool/prompt/resource registries, roots, resource subscriptions, task-aware execution, optional auth hooks, and capability advertising. If you want to inspect or build against the same framework used for this trace, check out [github.com/Agent-Hellboy/py-mcp](https://github.com/Agent-Hellboy/py-mcp).

This trace uses the published MCP lifecycle specification version `2025-11-25` as the reference point. That lifecycle still defines three phases: initialization, operation, and shutdown. Initialization is where the client and server agree on protocol version, exchange capabilities, and share implementation details. After the server answers `initialize`, the client sends `notifications/initialized` before normal operation begins.

[![Cursor probes OAuth metadata, then sends initialize and notifications/initialized.](/static/articles/mcp/request-flow/01-oauth-initialize.png)](/static/articles/mcp/request-flow/01-oauth-initialize.png)

The first two requests are probes against standard OAuth protected-resource metadata endpoints:

```text
/.well-known/oauth-protected-resource/mcp
/.well-known/oauth-protected-resource
```

Both return `404 Not Found` in this run. For this local unauthenticated server, that is expected: the server is not exposing OAuth metadata. Cursor then opens the MCP session with `initialize`.

In the observed `initialize` request, Cursor identifies itself as:

```json
{
  "name": "cursor-vscode",
  "version": "1.0.0"
}
```

It also advertises client capabilities, including `elicitation`, `roots`, and an extension capability for MCP UI/App-style content:

```text
io.modelcontextprotocol/ui
text/html;profile=mcp-app
```

That is the important signal in this trace: Cursor is not just asking for tools. It is announcing capability-level support for richer MCP UI/App content negotiation.

After the server responds to `initialize`, Cursor sends:

```text
notifications/initialized
```

That marks the end of the handshake and the start of normal MCP operation.

[![After initialization, Cursor lists resources, tools, prompts, and subscribes to a resource.](/static/articles/mcp/request-flow/02-discovery-subscribe.png)](/static/articles/mcp/request-flow/02-discovery-subscribe.png)

Once initialized, Cursor immediately performs discovery:

```text
resources/list
tools/list
resources/list
prompts/list
resources/subscribe
```

The duplicate `resources/list` is visible in the trace. Cursor first reads available resources, then tools, then resources again, then prompts. Finally, it subscribes to the resource:

```text
memo://welcome
```

The server responds with a resource named `welcome_memo`, several tools, and a prompt template. At this point Cursor has enough inventory to decide what it can show or call in the client.

[![Cursor shows the discovered PyMCP server, its tools, prompt, and resource in the MCP settings page.](/static/articles/mcp/request-flow/05-cursor-mcp-tools.png)](/static/articles/mcp/request-flow/05-cursor-mcp-tools.png)

This is the client-side result of discovery. Cursor now shows the server as `json-schema`, and the inventory from `tools/list`, `prompts/list`, and `resources/list` is visible in the MCP settings UI:

```text
addNumbersTool
multiplyNumbersTool
greetTool
calculateAreaTool
promptEchoTool
releaseNotesPrompt
welcome_memo
```

That matters because this is where the abstract JSON-RPC discovery calls become actual client affordances. The server did not just respond with data; it gave Cursor enough structured metadata to expose callable tools, a prompt, and a resource to the user.

[![Cursor later calls a tool and receives a normal JSON-RPC result.](/static/articles/mcp/request-flow/03-tool-call-followup.png)](/static/articles/mcp/request-flow/03-tool-call-followup.png)

After discovery, user or client activity can produce normal operation requests. In the server trace below, Cursor calls:

```text
tools/call
```

The tool is `addNumbersTool`, with arguments `a = 5` and `b = 6`. The server returns a JSON-RPC result containing:

```text
Sum of 5 + 6 = 11
```

That shows the request has moved out of startup discovery and into ordinary MCP operation. The client has selected a discovered tool, sent a JSON-RPC `tools/call`, and received tool output as normal MCP content.

[![Cursor sends ping and calls another tool after the initial session is established.](/static/articles/mcp/request-flow/04-ping-and-tools.png)](/static/articles/mcp/request-flow/04-ping-and-tools.png)

The last screenshot shows two useful follow-up behaviors. First, Cursor sends:

```text
ping
```

The server returns an empty result, confirming the session is still alive. Then Cursor calls another tool:

```text
multiplyNumbersTool
```

The response contains:

```text
Product of 4 x 7 = 28
```

[![Cursor chat runs the discovered add and multiply tools and displays their results.](/static/articles/mcp/request-flow/06-cursor-tool-results.png)](/static/articles/mcp/request-flow/06-cursor-tool-results.png)

This is the same behavior from the Cursor chat view. Cursor first checks available MCP tools for the request, then runs the matching tool from the `json-schema` server:

```text
Ran Add Numbers Tool in json-schema
Using the addNumbersTool MCP tool with a = 5 and b = 6
5 + 6 = 11
```

The second prompt does the same thing for multiplication:

```text
Ran Multiply Numbers Tool in json-schema
Using the multiplyNumbersTool MCP tool with a = 4 and b = 7
4 x 7 = 28
```

So the full flow is visible in both places: the server logs prove the Streamable HTTP JSON-RPC request path, while Cursor proves that the discovered tool schema was usable enough for the client to select and execute the right MCP tool.

So the complete observed startup and early operation flow is:

```text
OAuth metadata probe
initialize
notifications/initialized
resources/list
tools/list
resources/list
prompts/list
resources/subscribe
ping
tools/call
```

The final `client connection complete` log means Cursor stopped sending startup discovery requests after the initial MCP session setup. From a server author's point of view, this is a useful baseline: do not treat the OAuth metadata probes as failures, do not assume `tools/list` is the first MCP method you will see, and expect clients to discover resources and prompts even when the feature you care about is tool calling.

The lifecycle flow is getting removed in a near-future release, but this article is still important because it explains the current client and server flow in the 2025-11-25 specification. [SEP-2575: Make MCP Stateless](https://modelcontextprotocol.io/seps/2575-stateless-mcp) is final, and it removes the stateful initialization handshake in the next protocol direction:

```text
initialize / notifications/initialized
```

Under that SEP, protocol version moves to per-request metadata, capability discovery moves to `server/discover`, `resources/subscribe` is replaced by the `subscriptions/listen` model, and `ping` is removed because normal RPC calls and transport-level keepalive mechanisms already prove liveness.

So treat this trace as a compatibility snapshot of the current 2025-11-25 lifecycle: it documents how Cursor starts an MCP session over Streamable HTTP today, while also showing the exact machinery that the stateless MCP direction is deprecating and removing in the future release line.
