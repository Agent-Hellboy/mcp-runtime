"""Minimal Flask app for the MCP Runtime landing page.

Documentation lives at docs.mcpruntime.org (deployed separately from /docs in
the repo). This site is intentionally a single page with links out to docs and
GitHub.
"""

import os

from flask import Flask, Response, redirect, render_template, request, url_for

app = Flask(__name__)


def _canonical_site_base() -> str:
    """Trusted site origin; prefer MCP_WEBSITE_BASE_URL in production."""
    u = (os.environ.get("MCP_WEBSITE_BASE_URL") or "").strip().rstrip("/")
    if u:
        return u
    return url_for("home", _external=True).rstrip("/")


DOCS_URL = (os.environ.get("MCP_DOCS_URL") or "https://docs.mcpruntime.org/").rstrip("/") + "/"
GITHUB_URL = "https://github.com/Agent-Hellboy/mcp-runtime"


@app.context_processor
def _inject_globals():
    def canonical_og_url():
        path = request.path or "/"
        if path != "/" and not path.startswith("/"):
            path = "/" + path
        return _canonical_site_base() + (path if path != "/" else "/")

    return {
        "canonical_og_url": canonical_og_url,
        "docs_url": DOCS_URL,
        "github_url": GITHUB_URL,
    }


CONTENT_SECURITY_POLICY = (
    "default-src 'self'; "
    "img-src 'self' data:; "
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "
    "script-src 'self' 'unsafe-inline'; "
    "connect-src 'self'; "
    "font-src 'self' https://fonts.gstatic.com; "
    "object-src 'none'; "
    "base-uri 'self'; "
    "frame-ancestors 'none'"
)

STATIC_CACHE_CONTROL = "public, max-age=3600, must-revalidate"


@app.route("/")
def home():
    return render_template("index.html")


@app.route("/docs")
@app.route("/docs/")
@app.route("/docs/<path:_subpath>")
def docs_redirect(_subpath: str = ""):
    """Redirect any /docs* path to the external docs site."""
    suffix = _subpath.lstrip("/") if _subpath else ""
    target = DOCS_URL + suffix
    return redirect(target, code=302)


@app.route("/robots.txt")
def robots_txt():
    body = (
        "User-agent: *\n"
        "Allow: /\n"
        f"Sitemap: {url_for('sitemap_xml', _external=True)}\n"
    )
    return Response(body, mimetype="text/plain")


@app.route("/sitemap.xml")
def sitemap_xml():
    base = _canonical_site_base()
    body = (
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
        f"  <url><loc>{base}/</loc></url>\n"
        "</urlset>\n"
    )
    return Response(body, mimetype="application/xml")


@app.after_request
def apply_response_headers(response):
    response.headers.setdefault("Content-Security-Policy", CONTENT_SECURITY_POLICY)
    response.headers.setdefault("X-Content-Type-Options", "nosniff")
    response.headers.setdefault("X-Frame-Options", "DENY")
    response.headers.setdefault("Referrer-Policy", "strict-origin-when-cross-origin")
    response.headers.setdefault("Permissions-Policy", "interest-cohort=()")

    if response.status_code < 400 and (request.path or "").startswith("/static/"):
        response.headers["Cache-Control"] = STATIC_CACHE_CONTROL
    return response


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
