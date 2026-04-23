"""Shared assertion helpers for e2e test Python blocks.

Load in a heredoc with:
    import os
    exec(open(os.environ["E2E_HELPERS"]).read())
"""
from __future__ import annotations


def fail(message: str) -> None:
    print(f"[assert][fail] {message}")
    raise AssertionError(message)


def ok(message: str) -> None:
    print(f"[assert][pass] {message}")


def check(condition: bool, success_message: str, failure_message: str) -> None:
    if condition:
        ok(success_message)
        return
    fail(failure_message)


def make_initialize_payload(protocol: str, id: int = 1) -> dict:
    return {
        "jsonrpc": "2.0",
        "id": id,
        "method": "initialize",
        "params": {
            "protocolVersion": protocol,
            "capabilities": {},
            "clientInfo": {"name": "mcp-runtime-e2e", "version": "1.0.0"},
        },
    }
