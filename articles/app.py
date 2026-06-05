"""Standalone article publication for articles.mcpruntime.org."""

from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
from functools import lru_cache
import os
from pathlib import Path
import secrets
import sqlite3
import warnings
from urllib.parse import urlparse

import markdown
from authlib.integrations.flask_client import OAuth
from flask import Flask, Response, abort, flash, g, redirect, render_template, request, session, url_for

app = Flask(__name__)
app.config["SEND_FILE_MAX_AGE_DEFAULT"] = 0

CONTENT_DIR = Path(__file__).resolve().parent / "content"
STYLE_PATH = Path(__file__).resolve().parent / "static" / "style.css"
BASE_URL = (os.environ.get("MCP_ARTICLES_BASE_URL") or "https://articles.mcpruntime.org").rstrip("/")
WEBSITE_URL = (os.environ.get("MCP_WEBSITE_URL") or "https://mcpruntime.org").rstrip("/") + "/"
DOCS_URL = (os.environ.get("MCP_DOCS_URL") or "https://docs.mcpruntime.org").rstrip("/") + "/"
GITHUB_URL = "https://github.com/Agent-Hellboy/mcp-runtime"
STATIC_VERSION = int(STYLE_PATH.stat().st_mtime) if STYLE_PATH.exists() else 0
DB_PATH = Path(os.environ.get("MCP_ARTICLES_DB_PATH") or Path(__file__).resolve().parent / "articles.db")


def _first_env(*names: str) -> str | None:
    for name in names:
        value = os.environ.get(name)
        if value:
            return value
    return None


GOOGLE_CLIENT_ID = _first_env("MCP_ARTICLES_GOOGLE_CLIENT_ID", "ARTICLES_GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_ID")
GOOGLE_CLIENT_SECRET = _first_env(
    "MCP_ARTICLES_GOOGLE_CLIENT_SECRET",
    "ARTICLES_GOOGLE_CLIENT_SECRET",
    "GOOGLE_CLIENT_SECRET",
)
GOOGLE_OAUTH_CONFIGURED = bool(GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET)


def _is_production_oauth() -> bool:
    hostname = urlparse(BASE_URL).hostname or ""
    return GOOGLE_OAUTH_CONFIGURED and BASE_URL.startswith("https://") and hostname not in {"localhost", "127.0.0.1", "::1"}


def _secret_key() -> str:
    configured = os.environ.get("MCP_ARTICLES_SECRET_KEY")
    if configured:
        return configured
    message = (
        "MCP_ARTICLES_SECRET_KEY is not set. Using a random key breaks sessions "
        "and CSRF validation across multiple worker processes."
    )
    if _is_production_oauth():
        raise RuntimeError(message)
    warnings.warn(message, RuntimeWarning, stacklevel=2)
    return secrets.token_hex(32)


app.config["SECRET_KEY"] = _secret_key()
app.config["SESSION_COOKIE_HTTPONLY"] = True
app.config["SESSION_COOKIE_SAMESITE"] = "Lax"
app.config["SESSION_COOKIE_SECURE"] = BASE_URL.startswith("https://")

oauth = OAuth(app)
if GOOGLE_OAUTH_CONFIGURED:
    oauth.register(
        name="google",
        client_id=GOOGLE_CLIENT_ID,
        client_secret=GOOGLE_CLIENT_SECRET,
        server_metadata_url="https://accounts.google.com/.well-known/openid-configuration",
        client_kwargs={"scope": "openid email profile"},
    )


@dataclass(frozen=True)
class Category:
    slug: str
    name: str
    deck: str

    @property
    def url(self) -> str:
        return f"/{self.slug}/"


CATEGORIES: tuple[Category, ...] = (
    Category("mcp", "MCP", "Protocol traces, client behavior, gateways, UI content, and runtime design."),
    Category("kubernetes", "Kubernetes", "Clusters, controllers, ingress, scheduling, registry flows, and platform operations."),
    Category("networking", "Networking", "HTTP, DNS, TLS, proxies, service discovery, traffic routing, and failure modes."),
    Category("infrastructure", "Infrastructure", "Deployment systems, observability, reliability, release discipline, and operations."),
    Category("identity-policy", "Identity & Policy", "Auth, authorization, grants, sessions, trust levels, governance, and audit."),
)

CATEGORY_BY_SLUG = {category.slug: category for category in CATEGORIES}


@dataclass(frozen=True)
class Article:
    slug: str
    title: str
    description: str
    category_slug: str
    category_name: str
    published: str
    reading_time: str
    body_html: str

    @property
    def url(self) -> str:
        return f"/{self.slug}/"

    @property
    def canonical_url(self) -> str:
        return f"{BASE_URL}/{self.slug}/"


@dataclass(frozen=True)
class Comment:
    id: int
    article_slug: str
    author_name: str
    body: str
    created_at: str


def _canonical_url() -> str:
    path = request.path or "/"
    if not path.startswith("/"):
        path = "/" + path
    return BASE_URL + path


def _safe_next_url(next_url: str | None) -> str:
    if not next_url:
        return url_for("index")
    parsed = urlparse(next_url)
    if parsed.scheme or parsed.netloc:
        return url_for("index")
    if not next_url.startswith("/") or next_url.startswith("//") or next_url.startswith("\\"):
        return url_for("index")
    return next_url


def _csrf_token() -> str:
    token = session.get("csrf_token")
    if not token:
        token = secrets.token_urlsafe(32)
        session["csrf_token"] = token
    return token


def _verify_csrf() -> None:
    expected = session.get("csrf_token")
    submitted = request.form.get("csrf_token")
    if not expected or not submitted or not secrets.compare_digest(expected, submitted):
        abort(400)


def init_db() -> None:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    with sqlite3.connect(DB_PATH) as conn:
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS comments (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                article_slug TEXT NOT NULL,
                google_sub TEXT NOT NULL,
                author_name TEXT NOT NULL,
                author_email TEXT NOT NULL,
                body TEXT NOT NULL,
                created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
            )
            """
        )
        conn.execute("CREATE INDEX IF NOT EXISTS idx_comments_article_created ON comments(article_slug, created_at)")


@contextmanager
def _db():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    try:
        with conn:
            yield conn
    finally:
        conn.close()


init_db()


def comments_for_article(article_slug: str) -> tuple[Comment, ...]:
    with _db() as conn:
        rows = conn.execute(
            """
            SELECT id, article_slug, author_name, body, created_at
            FROM comments
            WHERE article_slug = ?
            ORDER BY created_at ASC, id ASC
            """,
            (article_slug,),
        ).fetchall()
    return tuple(
        Comment(
            id=row["id"],
            article_slug=row["article_slug"],
            author_name=row["author_name"],
            body=row["body"],
            created_at=row["created_at"],
        )
        for row in rows
    )


def create_comment(article_slug: str, user: dict[str, str], body: str) -> None:
    normalized = body.strip()
    if not normalized:
        raise ValueError("Comment cannot be empty.")
    if len(normalized) > 2000:
        raise ValueError("Comment must be 2,000 characters or fewer.")
    with _db() as conn:
        conn.execute(
            """
            INSERT INTO comments (article_slug, google_sub, author_name, author_email, body)
            VALUES (?, ?, ?, ?, ?)
            """,
            (
                article_slug,
                user["sub"],
                user.get("name") or "Reader",
                user.get("email") or "",
                normalized,
            ),
        )


def _split_front_matter(source: str) -> tuple[dict[str, str], str]:
    if not source.startswith("---\n"):
        return {}, source
    parts = source.split("---\n", 2)
    if len(parts) < 3:
        return {}, source
    _, metadata_block, body = parts
    metadata: dict[str, str] = {}
    for line in metadata_block.splitlines():
        key, separator, value = line.partition(":")
        if separator:
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
                value = value[1:-1]
            metadata[key.strip()] = value
    return metadata, body.strip()


@lru_cache(maxsize=1)
def load_articles() -> tuple[Article, ...]:
    articles: list[Article] = []
    for article_path in sorted(CONTENT_DIR.glob("**/*.md")):
        metadata, body = _split_front_matter(article_path.read_text(encoding="utf-8"))
        slug = article_path.relative_to(CONTENT_DIR).with_suffix("").as_posix()
        category_slug = slug.split("/", 1)[0]
        category = CATEGORY_BY_SLUG.get(category_slug, Category(category_slug, category_slug.title(), ""))
        body_html = markdown.markdown(
            body,
            extensions=["fenced_code", "tables", "toc"],
            output_format="html5",
        )
        articles.append(
            Article(
                slug=slug,
                title=metadata.get("title", article_path.stem.replace("-", " ").title()),
                description=metadata.get("description", ""),
                category_slug=category.slug,
                category_name=metadata.get("category", category.name),
                published=metadata.get("published", ""),
                reading_time=metadata.get("reading_time", "Essay"),
                body_html=body_html,
            )
        )
    return tuple(sorted(articles, key=lambda article: article.published, reverse=True))


def articles_for_category(category_slug: str) -> tuple[Article, ...]:
    return tuple(article for article in load_articles() if article.category_slug == category_slug)


def get_article(slug: str) -> Article | None:
    return next((article for article in load_articles() if article.slug == slug), None)


@app.context_processor
def inject_globals():
    return {
        "canonical_url": _canonical_url,
        "categories": CATEGORIES,
        "csrf_token": _csrf_token,
        "docs_url": DOCS_URL,
        "github_url": GITHUB_URL,
        "google_oauth_configured": GOOGLE_OAUTH_CONFIGURED,
        "static_version": STATIC_VERSION,
        "user": getattr(g, "user", None),
        "website_url": WEBSITE_URL,
    }


CONTENT_SECURITY_POLICY = (
    "default-src 'self'; "
    "img-src 'self' data:; "
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "
    "script-src 'self'; "
    "connect-src 'self'; "
    "font-src 'self' https://fonts.gstatic.com; "
    "object-src 'none'; "
    "base-uri 'self'; "
    "frame-ancestors 'none'"
)


@app.route("/")
def index():
    articles = load_articles()
    latest = articles[0] if articles else None
    category_counts = {category.slug: len(articles_for_category(category.slug)) for category in CATEGORIES}
    return render_template(
        "index.html",
        title="MCP Runtime Articles",
        description="Technical articles on MCP, Kubernetes, networking, infrastructure, identity, and policy.",
        articles=articles,
        latest=latest,
        category_counts=category_counts,
    )


@app.route("/<category_slug>/")
def category(category_slug: str):
    category_item = CATEGORY_BY_SLUG.get(category_slug)
    if category_item is None:
        abort(404)
    articles = articles_for_category(category_slug)
    return render_template(
        "category.html",
        title=f"{category_item.name} Articles",
        description=category_item.deck,
        category=category_item,
        articles=articles,
    )


@app.route("/<path:slug>/")
def article(slug: str):
    article_item = get_article(slug)
    if article_item is None:
        abort(404)
    return render_template(
        "detail.html",
        title=article_item.title,
        description=article_item.description,
        article=article_item,
        comments=comments_for_article(article_item.slug),
    )


@app.route("/login")
def login():
    if not GOOGLE_OAUTH_CONFIGURED:
        flash("Google login is not configured yet.")
        return redirect(_safe_next_url(request.args.get("next")))
    redirect_uri = f"{BASE_URL}{url_for('auth_google_callback')}"
    session["login_next"] = _safe_next_url(request.args.get("next"))
    return oauth.google.authorize_redirect(redirect_uri)


@app.route("/auth/google/callback")
def auth_google_callback():
    if not GOOGLE_OAUTH_CONFIGURED:
        abort(404)
    token = oauth.google.authorize_access_token()
    profile = token.get("userinfo") or oauth.google.get("userinfo").json()
    email = profile.get("email", "")
    if not profile.get("sub") or not email or profile.get("email_verified") is False:
        abort(401)
    session["user"] = {
        "sub": profile["sub"],
        "name": profile.get("name") or email.split("@", 1)[0],
        "email": email,
    }
    pending_comment = session.pop("pending_comment", None)
    if isinstance(pending_comment, dict):
        article_slug = pending_comment.get("article_slug", "")
        article_item = get_article(article_slug)
        if article_item is not None:
            try:
                create_comment(article_item.slug, session["user"], pending_comment.get("body", ""))
                flash("Comment posted.")
                return redirect(article_item.url + "#comments")
            except ValueError as exc:
                flash(str(exc))
    return redirect(_safe_next_url(session.pop("login_next", None)))


@app.post("/logout")
def logout():
    _verify_csrf()
    session.pop("user", None)
    flash("Signed out.")
    return redirect(_safe_next_url(request.form.get("next")))


@app.post("/<path:slug>/comments")
def post_comment(slug: str):
    article_item = get_article(slug)
    if article_item is None:
        abort(404)
    _verify_csrf()
    user = getattr(g, "user", None)
    if user is None:
        body = request.form.get("body", "")
        if body.strip():
            session["pending_comment"] = {"article_slug": article_item.slug, "body": body}
        if not GOOGLE_OAUTH_CONFIGURED:
            flash("Google login is not configured yet.")
            return redirect(article_item.url + "#comments")
        return redirect(url_for("login", next=article_item.url))
    try:
        create_comment(article_item.slug, user, request.form.get("body", ""))
        flash("Comment posted.")
    except ValueError as exc:
        flash(str(exc))
    return redirect(article_item.url + "#comments")


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
    category_urls = "".join(f"  <url><loc>{BASE_URL}/{category.slug}/</loc></url>\n" for category in CATEGORIES)
    article_urls = "".join(f"  <url><loc>{article.canonical_url}</loc></url>\n" for article in load_articles())
    body = (
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
        f"  <url><loc>{BASE_URL}/</loc></url>\n"
        f"{category_urls}"
        f"{article_urls}"
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
        response.headers["Cache-Control"] = "public, max-age=3600, must-revalidate"
    return response


@app.before_request
def load_current_user():
    g.user = session.get("user")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
