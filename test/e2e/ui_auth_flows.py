import json
import os
import urllib.error
import urllib.request

from _helpers_loader import load_e2e_helpers

_helpers = load_e2e_helpers()
check = _helpers.check

ui_base = os.environ["UI_BASE"]
gateway_base = os.environ["GATEWAY_BASE"]
api_key = os.environ["API_KEY"]
platform_mode = os.environ["E2E_PLATFORM_MODE"]


def request(url, *, method="GET", headers=None, body=None):
    headers = dict(headers or {})
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers.setdefault("content-type", "application/json")
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, dict(resp.headers.items()), resp.read().decode()
    except urllib.error.HTTPError as exc:
        return exc.code, dict(exc.headers.items()), exc.read().decode()


def expect_status(url, status, *, method="GET", headers=None, body=None, contains=None):
    got_status, _, got_body = request(url, method=method, headers=headers, body=body)
    check(got_status == status, f"{method} {url} returned {status}", f"{method} {url} returned {got_status}: {got_body}")
    if contains:
        check(contains in got_body, f"{method} {url} contained {contains!r}", f"{method} {url} missing {contains!r}: {got_body}")
    return got_body


def expect_json(url, status=200, *, method="GET", headers=None, body=None):
    return json.loads(expect_status(url, status, method=method, headers=headers, body=body))


def check_ui_auth(base, label, *, include_observability=False):
    expect_status(f"{base}/auth/status", 200, contains='"authenticated":false')
    expect_status(f"{base}/auth/login", 401, method="POST", body={"api_key": "wrong-api-key"})
    login_status, login_headers, login_body = request(f"{base}/auth/login", method="POST", body={"api_key": api_key})
    check(login_status == 200, f"{label} POST /auth/login accepted UI API key", f"{label} login failed: {login_status} {login_body}")
    set_cookie = login_headers.get("Set-Cookie") or login_headers.get("set-cookie") or ""
    cookie = set_cookie.split(";", 1)[0]
    check(cookie.startswith("mcp_ui_session="), f"{label} POST /auth/login returned session cookie", f"missing cookie: {login_headers}")
    cookie_headers = {"Cookie": cookie}
    status = expect_json(f"{base}/auth/status", headers=cookie_headers)
    check(status.get("authenticated") is True, f"{label} GET /auth/status returned authenticated session", f"{label} status response: {status}")
    admin_status, _, admin_body = request(f"{base}/auth/admin-check", headers=cookie_headers)
    check(admin_status == 204, f"{label} GET /auth/admin-check allowed admin session", f"{label} admin-check failed: {admin_status} {admin_body}")
    if include_observability:
        expect_status(f"{base}/grafana/api/health", 200, headers=cookie_headers, contains="database")
        expect_status(f"{base}/prometheus/-/healthy", 404, headers=cookie_headers)
    logout = expect_json(f"{base}/auth/logout", method="POST", headers=cookie_headers)
    check(logout.get("authenticated") is False, f"{label} POST /auth/logout cleared session", f"{label} logout response: {logout}")
    expect_status(f"{base}/auth/status", 200, headers=cookie_headers, contains='"authenticated":false')


expect_status(f"{ui_base}/health", 200, contains='"ok":true')
expect_status(f"{ui_base}/", 200, contains="MCP Sentinel Control Plane")
ui_config = expect_status(f"{ui_base}/config.js", 200, contains="window.MCP_API_BASE")
check(f'window.MCP_PLATFORM_MODE = "{platform_mode}"' in ui_config, "ui config.js exposes platform mode", f"ui config missing platform mode {platform_mode}: {ui_config}")
expect_status(f"{ui_base}/app.js", 200, contains="const apiBase")
expect_status(f"{ui_base}/styles.css", 200, contains=".canvas")
expect_status(f"{gateway_base}/", 200, contains="MCP Sentinel Control Plane")
expect_status(f"{gateway_base}/config.js", 200, contains="window.MCP_API_BASE")
expect_status(f"{gateway_base}/app.js", 200, contains="const apiBase")
expect_status(f"{gateway_base}/styles.css", 200, contains=".canvas")
expect_status(f"{gateway_base}/grafana/api/health", 401)
expect_status(f"{gateway_base}/prometheus/-/healthy", 404)

check_ui_auth(ui_base, "ui")
check_ui_auth(gateway_base, "gateway", include_observability=True)

print("ui-auth request routes:")
for route in (
    "ui:/health",
    "ui:/",
    "ui:/config.js",
    "ui:/app.js",
    "ui:/styles.css",
    "ui:/auth/login",
    "ui:/auth/status",
    "ui:/auth/admin-check",
    "ui:/auth/logout",
    "gateway:/",
    "gateway:/config.js",
    "gateway:/app.js",
    "gateway:/styles.css",
    "gateway:/auth/login",
    "gateway:/auth/status",
    "gateway:/auth/admin-check",
    "gateway:/auth/logout",
    "gateway:/grafana/api/health",
    "gateway:/prometheus/-/healthy (hidden)",
):
    print(f"  {route}")
