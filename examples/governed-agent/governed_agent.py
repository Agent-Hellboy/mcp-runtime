#!/usr/bin/env python3
"""Example agent/probe for MCP Runtime governed tool calls.

The Agents SDK mode uses MCPServerStreamableHttp with MCP Runtime's governance
headers. The probe mode uses direct MCP JSON-RPC calls so policy behavior can be
checked without an OpenAI API key.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any


DEFAULT_MCP_URL = "http://localhost:18080/governed-agent-demo-mcp/mcp"
DEFAULT_HUMAN_ID = "support-lead"
DEFAULT_AGENT_ID = "ticket-triage-agent"
DEFAULT_AGENT_SESSION = "sess-ticket-triage-agent"
DEFAULT_PROTOCOL_VERSION = "2025-06-18"


@dataclass(frozen=True)
class Config:
    mcp_url: str
    human_id: str
    agent_id: str
    agent_session: str
    host_header: str
    protocol_version: str
    timeout_seconds: float
    model: str
    prompt: str


@dataclass(frozen=True)
class HTTPResult:
    status: int
    headers: dict[str, str]
    body: str
    json_body: Any | None


def env_or(name: str, default: str) -> str:
    value = os.environ.get(name)
    if value is None or value.strip() == "":
        return default
    return value.strip()


def governance_headers(config: Config) -> dict[str, str]:
    headers = {
        "X-MCP-Human-ID": config.human_id,
        "X-MCP-Agent-ID": config.agent_id,
        "X-MCP-Agent-Session": config.agent_session,
    }
    if config.host_header:
        headers["Host"] = config.host_header
    return headers


def parse_json_body(body: str) -> Any | None:
    body = body.strip()
    if not body:
        return None
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        pass

    # Some MCP transports may return event-stream data. Keep the parser small
    # and only extract JSON data frames when present.
    for line in body.splitlines():
        line = line.strip()
        if not line.startswith("data:"):
            continue
        payload = line[len("data:") :].strip()
        if not payload or payload == "[DONE]":
            continue
        try:
            return json.loads(payload)
        except json.JSONDecodeError:
            continue
    return None


def post_json(config: Config, payload: dict[str, Any], mcp_session_id: str | None = None) -> HTTPResult:
    headers = {
        "content-type": "application/json",
        "accept": "application/json, text/event-stream",
        "Mcp-Protocol-Version": config.protocol_version,
        **governance_headers(config),
    }
    if mcp_session_id:
        headers["Mcp-Session-Id"] = mcp_session_id

    request = urllib.request.Request(
        config.mcp_url,
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=config.timeout_seconds) as response:
            body = response.read().decode("utf-8")
            response_headers = dict(response.headers.items())
            return HTTPResult(response.status, response_headers, body, parse_json_body(body))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8")
        response_headers = dict(exc.headers.items())
        return HTTPResult(exc.code, response_headers, body, parse_json_body(body))


def response_header(headers: dict[str, str], wanted: str) -> str:
    wanted_lower = wanted.lower()
    for name, value in headers.items():
        if name.lower() == wanted_lower:
            return value
    return ""


def make_initialize_payload(config: Config) -> dict[str, Any]:
    return {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": config.protocol_version,
            "capabilities": {},
            "clientInfo": {
                "name": "mcp-runtime-governed-agent",
                "version": "0.1.0",
            },
        },
    }


def tool_text(json_body: Any | None) -> str:
    if not isinstance(json_body, dict):
        return ""
    result = json_body.get("result")
    if not isinstance(result, dict):
        return ""
    content = result.get("content")
    if not isinstance(content, list):
        return ""
    texts = []
    for item in content:
        if isinstance(item, dict) and item.get("type") == "text":
            texts.append(str(item.get("text", "")))
    return "\n".join(texts)


def print_http_result(label: str, result: HTTPResult) -> None:
    print(f"{label}: HTTP {result.status}")
    if result.json_body is not None:
        print(json.dumps(result.json_body, indent=2, sort_keys=True))
    elif result.body:
        print(result.body)


def run_probe(config: Config) -> int:
    initialize = post_json(config, make_initialize_payload(config))
    print_http_result("initialize", initialize)
    mcp_session_id = response_header(initialize.headers, "Mcp-Session-Id")
    if initialize.status != 200 or not mcp_session_id:
        print("initialize failed or did not return Mcp-Session-Id", file=sys.stderr)
        return 1

    initialized = post_json(
        config,
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        mcp_session_id=mcp_session_id,
    )
    print_http_result("notifications/initialized", initialized)
    if initialized.status not in (200, 202):
        return 1

    allowed = post_json(
        config,
        {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/call",
            "params": {
                "name": "slugify",
                "arguments": {"message": "Ticket: Reset Payroll Password"},
            },
        },
        mcp_session_id=mcp_session_id,
    )
    print_http_result("slugify allow check", allowed)
    allowed_text = tool_text(allowed.json_body)
    if allowed.status != 200 or "ticket-reset-payroll-password" not in allowed_text:
        print("expected slugify to be allowed and return ticket-reset-payroll-password", file=sys.stderr)
        return 1

    denied = post_json(
        config,
        {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {
                "name": "upper",
                "arguments": {"message": "escalate payroll password reset"},
            },
        },
        mcp_session_id=mcp_session_id,
    )
    print_http_result("upper deny check", denied)
    if denied.status != 403 or not isinstance(denied.json_body, dict):
        print("expected upper to be denied with HTTP 403", file=sys.stderr)
        return 1
    if denied.json_body.get("error") != "trust_too_low":
        print(f"expected trust_too_low denial, got {denied.json_body}", file=sys.stderr)
        return 1

    print("governance result: low-trust tool allowed, medium-trust tool denied")
    return 0


async def run_agents_sdk(config: Config) -> int:
    try:
        from agents import Agent, Runner
        from agents.mcp import MCPServerStreamableHttp
        from agents.model_settings import ModelSettings
    except ImportError as exc:
        print(
            "Agents SDK is not installed. Run `pip install -r examples/governed-agent/requirements.txt`.",
            file=sys.stderr,
        )
        print(f"import error: {exc}", file=sys.stderr)
        return 1

    server_params: dict[str, Any] = {
        "url": config.mcp_url,
        "headers": governance_headers(config),
        "timeout": config.timeout_seconds,
    }
    agent_args: dict[str, Any] = {}
    if config.model:
        agent_args["model"] = config.model

    async with MCPServerStreamableHttp(
        name="MCP Runtime governed Go example",
        params=server_params,
        cache_tools_list=False,
        max_retry_attempts=1,
    ) as server:
        tool_names = [tool.name for tool in await server.list_tools()]
        print(f"MCP tools visible to the agent: {', '.join(tool_names)}")
        agent = Agent(
            name="Ticket Triage Governance Agent",
            instructions=(
                "You triage internal support tickets. Use the MCP tools. "
                "First call slugify for the ticket title. Then try upper for the "
                "same ticket title. If a tool call is denied, report that MCP "
                "Runtime governance blocked it and include the denial reason."
            ),
            mcp_servers=[server],
            model_settings=ModelSettings(tool_choice="required"),
            **agent_args,
        )
        result = await Runner.run(agent, config.prompt)
        print(result.final_output)
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run a governed MCP Runtime agent example.")
    parser.add_argument(
        "--mode",
        choices=("probe", "agents-sdk"),
        default=env_or("MCP_AGENT_MODE", "probe"),
        help="probe uses direct MCP JSON-RPC; agents-sdk runs the OpenAI Agents SDK example",
    )
    parser.add_argument("--mcp-url", default=env_or("MCP_AGENT_MCP_URL", DEFAULT_MCP_URL))
    parser.add_argument("--human-id", default=env_or("MCP_AGENT_HUMAN_ID", DEFAULT_HUMAN_ID))
    parser.add_argument("--agent-id", default=env_or("MCP_AGENT_ID", DEFAULT_AGENT_ID))
    parser.add_argument(
        "--agent-session",
        default=env_or("MCP_AGENT_SESSION", DEFAULT_AGENT_SESSION),
    )
    parser.add_argument(
        "--host-header",
        default=env_or("MCP_AGENT_HOST_HEADER", ""),
        help="optional Host header for host-based ingress routes",
    )
    parser.add_argument(
        "--protocol-version",
        default=env_or("MCP_PROTOCOL_VERSION", DEFAULT_PROTOCOL_VERSION),
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=float(env_or("MCP_AGENT_TIMEOUT", "20")),
        help="HTTP timeout in seconds",
    )
    parser.add_argument(
        "--model",
        default=env_or("OPENAI_MODEL", ""),
        help="optional Agents SDK model override",
    )
    parser.add_argument(
        "--prompt",
        default=env_or(
            "MCP_AGENT_PROMPT",
            "Triage ticket `Ticket: Reset Payroll Password` and show which MCP Runtime governance rule blocks escalation.",
        ),
    )
    return parser


def config_from_args(args: argparse.Namespace) -> Config:
    return Config(
        mcp_url=args.mcp_url,
        human_id=args.human_id,
        agent_id=args.agent_id,
        agent_session=args.agent_session,
        host_header=args.host_header,
        protocol_version=args.protocol_version,
        timeout_seconds=args.timeout,
        model=args.model,
        prompt=args.prompt,
    )


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    config = config_from_args(args)
    if args.mode == "probe":
        return run_probe(config)
    return asyncio.run(run_agents_sdk(config))


if __name__ == "__main__":
    raise SystemExit(main())
