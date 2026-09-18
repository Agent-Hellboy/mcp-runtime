"""MCP resource server built on the mcp-auth Python client SDK.

This is the Python counterpart to examples/mcp-auth-sdk-ping. It deliberately
does *not* use FastMCP's RemoteAuthProvider: that path already builds the
protected resource metadata for you, so it cannot exercise the plain SDK
surface a server author reaches for when they are not on FastMCP. Everything
here — the 401 and 403 challenges, the metadata document, audience and scope
validation — comes from mcp_auth_client.
"""

from __future__ import annotations

import json
import os

from mcp_auth_client import (
    protected_resource_metadata,
    unauthorized_headers,
    unauthorized_headers_for_error,
)
from mcp_auth_client.verifier import JWTVerifier, TokenVerificationError
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import JSONResponse, Response
from starlette.routing import Route

PROTOCOL_VERSION = "2025-06-18"
SERVER_NAME = "mcp-auth-sdk-ping-py"
REQUIRED_SCOPES = {"tools:read"}

TOOLS = [
    {
        "name": "sdk-ping-py",
        "description": "Return pong after mcp-auth Python SDK verification",
        "inputSchema": {"type": "object"},
    }
]


def _required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise SystemExit(f"{name} is required")
    return value


ISSUER = _required_env("MCP_AUTH_ISSUER").rstrip("/")
RESOURCE = _required_env("MCP_AUTH_RESOURCE")
METADATA_URL = _required_env("MCP_AUTH_RESOURCE_METADATA_URL")
MCP_PATH = os.getenv("MCP_PATH", "/mcp")

# The public issuer is not reachable from inside a pod and the authorization
# server sits behind an ingress that strips its path prefix, so discovery would
# look in the wrong place. Pointing at the in-cluster JWKS endpoint skips
# discovery; issuer and audience are still validated in full against the public
# values above.
verifier = JWTVerifier(
    jwks_uri=os.getenv("MCP_AUTH_JWKS_URL", "").strip() or f"{ISSUER}/.well-known/jwks.json",
    issuer=ISSUER,
    audience=RESOURCE,
    required_scopes=REQUIRED_SCOPES,
    # The in-cluster JWKS URL is plain HTTP over the Service network. Never
    # relax this for a URL that leaves the cluster.
    ssrf_safe=os.getenv("MCP_AUTH_JWKS_SSRF_SAFE", "true").strip().lower()
    not in {"0", "false", "no", "off"},
)

# Derived from the verifier, so scopes_supported always names the scope that is
# actually enforced. A client that reads this document asks for the right scope
# on its first attempt instead of being refused with 403 and no way to recover.
METADATA = protected_resource_metadata(verifier, RESOURCE, ISSUER)


async def metadata_endpoint(_: Request) -> Response:
    return JSONResponse(METADATA, headers={"Cache-Control": "max-age=3600"})


async def health(_: Request) -> Response:
    return JSONResponse({"ok": True})


def _result(request_id: object, payload: dict[str, object]) -> Response:
    return JSONResponse({"jsonrpc": "2.0", "id": request_id, "result": payload})


async def mcp_endpoint(request: Request) -> Response:
    header = request.headers.get("authorization", "")
    parts = header.split()
    if len(parts) != 2 or parts[0].lower() != "bearer" or not parts[1]:
        return Response(status_code=401, headers=unauthorized_headers(METADATA_URL))

    try:
        await verifier.verify(parts[1])
    except TokenVerificationError as error:
        # RFC 6750 section 3: a 403 has to say a scope is missing, or the client
        # cannot tell a recoverable scope problem from a rejected token.
        if "scope" in str(error).lower():
            return Response(
                status_code=403,
                headers=unauthorized_headers_for_error(
                    METADATA_URL, REQUIRED_SCOPES, "insufficient_scope", "required scope is missing"
                ),
            )
        return Response(
            status_code=401,
            headers=unauthorized_headers_for_error(
                METADATA_URL, None, "invalid_token", "token is invalid"
            ),
        )

    try:
        message = await request.json()
    except (json.JSONDecodeError, ValueError):
        return JSONResponse(
            {"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}},
            status_code=400,
        )

    method = message.get("method")
    request_id = message.get("id")

    if method == "initialize":
        return _result(
            request_id,
            {
                "protocolVersion": PROTOCOL_VERSION,
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": SERVER_NAME, "version": "1.0.0"},
            },
        )
    if method in {"notifications/initialized", "notifications/cancelled"}:
        return Response(status_code=202)
    if method == "tools/list":
        return _result(request_id, {"tools": TOOLS})
    if method == "tools/call":
        name = (message.get("params") or {}).get("name")
        if name != TOOLS[0]["name"]:
            return JSONResponse(
                {
                    "jsonrpc": "2.0",
                    "id": request_id,
                    "error": {"code": -32602, "message": f"unknown tool: {name}"},
                }
            )
        return _result(request_id, {"content": [{"type": "text", "text": "pong"}]})

    return JSONResponse(
        {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32601, "message": f"method not found: {method}"},
        }
    )


app = Starlette(
    routes=[
        Route(MCP_PATH, mcp_endpoint, methods=["POST"]),
        # Clients derive the metadata URL from the resource URI, so the document
        # has to answer on the path-suffixed form as well as the bare one.
        Route(f"/.well-known/oauth-protected-resource{MCP_PATH}", metadata_endpoint),
        Route("/.well-known/oauth-protected-resource", metadata_endpoint),
        Route("/health", health),
    ]
)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("PORT", "8088")))
