#!/usr/bin/env python3
"""Small HTTP proxy that injects headers before forwarding MCP traffic."""

from __future__ import annotations

import argparse
import http.client
import http.server
import socketserver
import sys
import urllib.parse


HOP_BY_HOP_HEADERS = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
}


def parse_header(raw: str) -> tuple[str, str]:
    name, sep, value = raw.partition("=")
    if not sep or not name.strip():
        raise argparse.ArgumentTypeError("headers must use NAME=VALUE form")
    return name.strip(), value


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--listen-host", default="127.0.0.1")
    parser.add_argument("--listen-port", type=int, required=True)
    parser.add_argument("--upstream-origin", required=True)
    parser.add_argument("--host-header", default="")
    parser.add_argument("--header", action="append", default=[], type=parse_header)
    return parser.parse_args()


def build_upstream_path(base_path: str, request_path: str) -> str:
    parsed = urllib.parse.urlsplit(request_path)
    path = parsed.path or "/"
    if base_path and base_path != "/":
        target_path = base_path.rstrip("/") + path
    else:
        target_path = path
    if parsed.query:
        target_path += "?" + parsed.query
    return target_path


class InjectingProxyHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def forward(self) -> None:
        config = self.server.proxy_config  # type: ignore[attr-defined]

        content_length = int(self.headers.get("Content-Length", "0") or "0")
        body = self.rfile.read(content_length) if content_length > 0 else None

        headers: dict[str, str] = {}
        for name, value in self.headers.items():
            if name.lower() in HOP_BY_HOP_HEADERS or name.lower() == "host":
                continue
            headers[name] = value
        for name, value in config["headers"].items():
            headers[name] = value
        if config["host_header"]:
            headers["Host"] = config["host_header"]
        if body is not None:
            headers["Content-Length"] = str(len(body))

        conn_class = http.client.HTTPSConnection if config["scheme"] == "https" else http.client.HTTPConnection
        conn = conn_class(config["host"], config["port"], timeout=30)
        try:
            conn.request(
                self.command,
                build_upstream_path(config["base_path"], self.path),
                body=body,
                headers=headers,
            )
            resp = conn.getresponse()
            payload = resp.read()
        except Exception as exc:  # pragma: no cover - best effort diagnostic path
            self.send_error(502, f"upstream request failed: {exc}")
            return
        finally:
            conn.close()

        self.send_response(resp.status, resp.reason)
        for name, value in resp.getheaders():
            lower = name.lower()
            if lower in HOP_BY_HOP_HEADERS or lower in {"content-length", "server", "date"}:
                continue
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if payload:
            self.wfile.write(payload)
        self.wfile.flush()

    def do_DELETE(self) -> None:  # noqa: N802
        self.forward()

    def do_GET(self) -> None:  # noqa: N802
        self.forward()

    def do_HEAD(self) -> None:  # noqa: N802
        self.forward()

    def do_OPTIONS(self) -> None:  # noqa: N802
        self.forward()

    def do_POST(self) -> None:  # noqa: N802
        self.forward()

    def do_PUT(self) -> None:  # noqa: N802
        self.forward()

    def log_message(self, fmt: str, *args: object) -> None:
        sys.stderr.write(fmt % args + "\n")


class ThreadingHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True


def main() -> int:
    args = parse_args()
    parsed = urllib.parse.urlparse(args.upstream_origin)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise SystemExit("--upstream-origin must be an http(s) URL")

    server = ThreadingHTTPServer((args.listen_host, args.listen_port), InjectingProxyHandler)
    server.proxy_config = {  # type: ignore[attr-defined]
        "scheme": parsed.scheme,
        "host": parsed.hostname,
        "port": parsed.port or (443 if parsed.scheme == "https" else 80),
        "base_path": parsed.path or "",
        "headers": dict(args.header),
        "host_header": args.host_header,
    }

    print(
        f"proxy listening on http://{args.listen_host}:{args.listen_port} -> {args.upstream_origin}",
        file=sys.stderr,
        flush=True,
    )
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
