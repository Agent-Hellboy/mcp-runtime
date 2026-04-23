import os

from mcp.server.fastmcp import FastMCP
from mcp.server.transport_security import TransportSecuritySettings


mcp = FastMCP(
    "python-example-mcp",
    transport_security=TransportSecuritySettings(enable_dns_rebinding_protection=False),
)


@mcp.tool()
def echo(message: str) -> str:
    """Echo the provided message."""
    return message


@mcp.tool()
def reverse(message: str) -> str:
    """Reverse the provided message."""
    return message[::-1]


if __name__ == "__main__":
    mcp.settings.host = "0.0.0.0"
    mcp.settings.port = int(os.environ.get("PORT", "8088"))
    mcp.settings.streamable_http_path = os.environ.get("MCP_PATH", "/mcp")
    mcp.run(transport="streamable-http")
