import os
import re
from datetime import datetime, timezone

from mcp.server.fastmcp import FastMCP
from mcp.server.transport_security import TransportSecuritySettings


mcp = FastMCP(
    "python-example-mcp",
    instructions=(
        "Python MCP example server with smoke, text, math, prompt, "
        "resource, and task examples."
    ),
    transport_security=TransportSecuritySettings(enable_dns_rebinding_protection=False),
)


_NON_SLUG = re.compile(r"[^a-z0-9]+")


def _normalize_priority(priority: str) -> str:
    value = (priority or "").strip().lower()
    if value in {"low", "medium", "high"}:
        return value
    return "medium"


@mcp.tool()
def ping(note: str = "") -> str:
    """Return a simple pong response."""
    return f"pong:{note}" if note else "pong"


@mcp.tool()
def echo(message: str) -> str:
    """Echo the provided message."""
    return message


@mcp.tool()
def reverse(message: str) -> str:
    """Reverse the provided message."""
    return message[::-1]


@mcp.tool()
def add(a: float, b: float) -> str:
    """Add two numbers."""
    result = a + b
    return f"{result:g}"


@mcp.tool()
def multiply(a: float, b: float) -> str:
    """Multiply two numbers."""
    result = a * b
    return f"{result:g}"


@mcp.tool()
def upper(message: str) -> str:
    """Uppercase the provided message."""
    return message.upper()


@mcp.tool()
def lower(message: str) -> str:
    """Lowercase the provided message."""
    return message.lower()


@mcp.tool()
def slugify(message: str) -> str:
    """Convert the provided message into a URL slug."""
    slug = _NON_SLUG.sub("-", (message or "").strip().lower()).strip("-")
    return slug


@mcp.tool()
def word_count(message: str) -> str:
    """Count words in the provided message."""
    return str(len((message or "").split()))


@mcp.tool()
def now() -> str:
    """Return the current UTC timestamp in RFC 3339 form."""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


@mcp.tool()
def create_task(title: str, priority: str = "medium", owner: str = "") -> str:
    """Create a deterministic task summary for IDE and adapter smoke tests."""
    title = (title or "").strip() or "Untitled task"
    owner = (owner or "").strip() or "unassigned"
    return (
        f"task: {title}\n"
        f"priority: {_normalize_priority(priority)}\n"
        f"owner: {owner}\n"
        "status: open"
    )


@mcp.resource("embedded:readme")
def readme_resource() -> str:
    """Sample resource served by the Python MCP example server."""
    return "This is a sample resource payload from the Python MCP example server."


@mcp.resource("embedded:task-guide")
def task_guide_resource() -> str:
    """Task workflow guidance for the Python MCP example server."""
    return (
        "Use create_task with title, priority (low|medium|high), and owner to "
        "produce a deterministic task record for adapter smoke tests."
    )


@mcp.prompt()
def hello() -> str:
    """Return a simple greeting prompt."""
    return "Hello from the Python MCP example server."


@mcp.prompt()
def summarize(text: str) -> str:
    """Summarize a short text input."""
    text = (text or "").strip() or "No text provided."
    return f"Summarize this briefly: {text}"


@mcp.prompt()
def task_brief(goal: str) -> str:
    """Draft a concise task brief from a goal."""
    goal = (goal or "").strip() or "No goal provided."
    return f"Turn this goal into a concise task brief with acceptance criteria: {goal}"


if __name__ == "__main__":
    mcp.settings.host = "0.0.0.0"
    mcp.settings.port = int(os.environ.get("PORT", "8088"))
    mcp.settings.streamable_http_path = os.environ.get("MCP_PATH", "/mcp")
    mcp.run(transport="streamable-http")
