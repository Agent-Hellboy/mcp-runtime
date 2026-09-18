# mcp-auth-sdk-ping-py

The Python counterpart to [`../mcp-auth-sdk-ping`](../mcp-auth-sdk-ping), proving
the mcp-auth **Python** client SDK against the same authorization server.

It deliberately avoids FastMCP's `RemoteAuthProvider`. That adapter already
builds the protected resource metadata for you, so it cannot exercise the plain
SDK surface a server author reaches for when they are not on FastMCP — which is
where the metadata is easy to get wrong. Everything here comes from
`mcp_auth_client`:

| Concern | SDK call |
|---|---|
| 401 challenge | `unauthorized_headers(metadata_url)` |
| 403 challenge naming the missing scope | `unauthorized_headers_for_error(..., "insufficient_scope", ...)` |
| RFC 9728 metadata, incl. `scopes_supported` | `protected_resource_metadata(verifier, resource, issuer)` |
| Signature, `iss`, `aud`, `exp`, scope | `JWTVerifier.verify` |

`scopes_supported` is derived from the verifier rather than written by hand, so
the advertised scope cannot drift from the enforced one. A server that gets this
wrong returns `403` with an empty body to every client, and the client has
nothing to retry with — Cursor reports it as
`Server returned 403 after trying upscoping`.

## Run locally

```bash
pip install -r requirements.txt
MCP_AUTH_ISSUER=http://localhost:18080/mcp-auth \
MCP_AUTH_RESOURCE=http://localhost:18080/mcp-auth-sdk-ping-py/mcp \
MCP_AUTH_RESOURCE_METADATA_URL=http://localhost:18080/.well-known/oauth-protected-resource/mcp-auth-sdk-ping-py/mcp \
MCP_PATH=/mcp-auth-sdk-ping-py/mcp \
python server.py
```

## Deploy

```bash
kubectl apply -f ../mcp-auth-sdk-ping-py.yaml
```

The resource URI in `MCP_AUTH_RESOURCE` must also be listed in the authorization
server's `MCP_AUTH_RESOURCES`, or `/authorize` rejects it with
`resource is not recognized`.
