# Example MCP Server (In-Cluster)

This example MCP server runs inside the cluster and emits analytics events to the ingest service.

## Endpoints

- `GET /mcp/tools` - list tools
- `POST /mcp/call` - call a tool
- `GET /health` - readiness

## Tool call example

```bash
curl -s -X POST http://<gateway>:8083/mcp/call \
  -H 'content-type: application/json' \
  -d '{"tool":"add","input":{"a":2,"b":3}}'
```

## Analytics

Each tool call sends a `tool.call` event to the ingest service.
