"""Load shared E2E assertion helpers from the path in E2E_HELPERS."""

from __future__ import annotations

import importlib.util
import os
from types import ModuleType


def load_e2e_helpers(path: str | None = None) -> ModuleType:
    helper_path = path or os.environ["E2E_HELPERS"]
    spec = importlib.util.spec_from_file_location("mcp_runtime_e2e_helpers", helper_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load e2e helpers from {helper_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
