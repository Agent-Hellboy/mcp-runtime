"""Shared assertion helpers for e2e test Python blocks.

Load in E2E scripts with:

    from _helpers_loader import load_e2e_helpers

    _helpers = load_e2e_helpers()
    check = _helpers.check
"""
from __future__ import annotations

import os
import sys


def _color_enabled() -> bool:
    if os.environ.get("NO_COLOR"):
        return False

    setting = os.environ.get("E2E_COLOR", "auto").strip().lower()
    if setting in {"always", "1", "true", "yes", "on"}:
        return True
    if setting in {"never", "0", "false", "no", "off"}:
        return False
    return sys.stdout.isatty()


_USE_COLOR = _color_enabled()
_RESET = "\033[0m" if _USE_COLOR else ""
_PASS = "\033[32m" if _USE_COLOR else ""
_FAIL = "\033[31m" if _USE_COLOR else ""


def _tag(text: str, color: str) -> str:
    if not color:
        return text
    return f"{color}{text}{_RESET}"


def fail(message: str) -> None:
    print(f"{_tag('[assert][fail]', _FAIL)} {message}")
    raise AssertionError(message)


def ok(message: str) -> None:
    print(f"{_tag('[assert][pass]', _PASS)} {message}")


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
