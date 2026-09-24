---
title: "Understanding the MCP server-client request flow"
description: "Tracking the MCP request flow from startup to the first tool call."
category: "MCP"
published: "2026-06-04"
reading_time: "7 min"
---

This article traces the client and server flow in the `2025-11-25` MCP specification, which starts with an `initialize` handshake. The current revision, `2026-07-28`, removes that handshake; see the note at the end. When you add a new MCP server in Cursor, the first thing that happens is discovery, before any tool call. Cursor starts by probing the server shape, negotiating the MCP session, and discovering what the server exposes. The screenshots below capture both sides of that sequence for a simple local server built with [PyMCP Kit](https://github.com/Agent-Hellboy/py-mcp), the framework I am using while implementing the MCP spec and tracing real MCP request flows. The terminal screenshots show what the server receives over Streamable HTTP. The Cursor screenshots show what those protocol messages turn into inside the client.

PyMCP Kit is my capability-first MCP server toolkit for FastAPI. It supports Streamable HTTP and stdio, tool/prompt/resource registries, roots, resource subscriptions, task-aware execution, optional auth hooks, and capability advertising. If you want to inspect or build against the same framework used for this trace, check out [github.com/Agent-Hellboy/py-mcp](https://github.com/Agent-Hellboy/py-mcp).

This trace uses the published MCP lifecycle specification version `2025-11-25` as the reference point. That lifecycle still defines three phases: initialization, operation, and shutdown. Initialization is where the client and server agree on protocol version, exchange capabilities, and share implementation details. After the server answers `initialize`, the client sends `notifications/initialized` before normal operation begins.

[![Cursor probes OAuth metadata, then sends initialize and notifications/initialized.](/static/articles/mcp/request-flow/01-oauth-initialize.png)](/static/articles/mcp/request-flow/01-oauth-initialize.png)

The first two requests are probes against standard OAuth protected-resource metadata endpoints: `/.well-known/oauth-protected-resource/mcp` and `/.well-known/oauth-protected-resource`. Both return `404 Not Found` in this run. For this local unauthenticated server, that is expected: the server is not exposing OAuth metadata. Cursor then opens the MCP session with `initialize`.

In the observed `initialize` request, Cursor identifies itself as:

```json
{
  "name": "cursor-vscode",
  "version": "1.0.0"
}
```

It also advertises client capabilities, including `elicitation`, `roots`, and an extension capability for MCP UI/App-style content: `io.modelcontextprotocol/ui` and `text/html;profile=mcp-app`


Here Cursor also announces capability-level support for richer MCP UI/App content negotiation.

After the server responds to `initialize`, Cursor sends: `notifications/initialized`

That marks the end of the handshake and the start of normal MCP operation.

[![After initialization, Cursor lists resources, tools, prompts, and subscribes to a resource.](/static/articles/mcp/request-flow/02-discovery-subscribe.png)](/static/articles/mcp/request-flow/02-discovery-subscribe.png)

Once initialized, Cursor immediately performs discovery: `resources/list`, `tools/list`, `resources/list`, `prompts/list`, and `resources/subscribe`.

The duplicate `resources/list` is visible in the trace. Cursor first reads available resources, then tools, then resources again, then prompts. Finally, it subscribes to the resource `memo://welcome`.

The server responds with a resource named `welcome_memo`, several tools, and a prompt template. At this point Cursor has enough inventory to decide what it can show or call in the client.

[![Cursor shows the discovered PyMCP server, its tools, prompt, and resource in the MCP settings page.](/static/articles/mcp/request-flow/05-cursor-mcp-tools.png)](/static/articles/mcp/request-flow/05-cursor-mcp-tools.png)

This is the client-side result of discovery. Cursor now shows the server as `json-schema`, and the inventory from `tools/list`, `prompts/list`, and `resources/list` is visible in the MCP settings UI: `addNumbersTool`, `multiplyNumbersTool`, `greetTool`, `calculateAreaTool`, `promptEchoTool`, `releaseNotesPrompt`, and `welcome_memo`.

This is where the JSON-RPC discovery calls turn into things the user can click. The server's responses give Cursor enough structured metadata to expose callable tools, a prompt, and a resource.

[![Cursor later calls a tool and receives a normal JSON-RPC result.](/static/articles/mcp/request-flow/03-tool-call-followup.png)](/static/articles/mcp/request-flow/03-tool-call-followup.png)

After discovery, user or client activity can produce normal operation requests. In the server trace below, Cursor calls `tools/call`.

The tool is `addNumbersTool`, with arguments `a = 5` and `b = 6`. The server returns a JSON-RPC result containing `Sum of 5 + 6 = 11`.

That shows the request has moved out of startup discovery and into ordinary MCP operation. The client has selected a discovered tool, sent a JSON-RPC `tools/call`, and received tool output as normal MCP content.

[![Cursor sends ping and calls another tool after the initial session is established.](/static/articles/mcp/request-flow/04-ping-and-tools.png)](/static/articles/mcp/request-flow/04-ping-and-tools.png)

The last screenshot shows two useful follow-up behaviors. First, Cursor sends `ping`.

The server returns an empty result, confirming the session is still alive. Then Cursor calls another tool: `multiplyNumbersTool`.

The response contains `Product of 4 x 7 = 28`.

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

So the complete observed startup and early operation flow is: `OAuth metadata probe`, `initialize`, `notifications/initialized`, `resources/list`, `tools/list`, `resources/list`, `prompts/list`, `resources/subscribe`, `ping`, and `tools/call`.

The final `client connection complete` log means Cursor stopped sending startup discovery requests after the initial MCP session setup. The `2025-11-25` spec requires clients to follow this sequence, and servers such as [PyMCP Kit](https://github.com/Agent-Hellboy/py-mcp) must handle it correctly. An MCP server implementation has to handle the OAuth metadata probes (returning `404` is valid for unauthenticated servers), respond to `initialize` and `notifications/initialized` in order, and serve `resources/list`, `tools/list`, and `prompts/list` before any tool call arrives. Clients follow this flow because the spec mandates it, so the implementation must support it end to end.

The current protocol revision, [`2026-07-28`](https://modelcontextprotocol.io/specification/2026-07-28/changelog), removed this lifecycle. [SEP-2575: Make MCP Stateless](https://modelcontextprotocol.io/seps/2575-stateless-mcp) replaced the `initialize` / `notifications/initialized` handshake: every request now carries its protocol version and client capabilities in `_meta`, and servers must implement `server/discover` to advertise their supported versions and capabilities. The same revision replaced `resources/subscribe` with `subscriptions/listen` and removed `ping`.

Treat this trace as a record of the `2025-11-25` lifecycle. Clients and servers that implement `2025-11-25` or earlier still use this handshake, and the current specification describes how the two generations [interoperate](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions).
