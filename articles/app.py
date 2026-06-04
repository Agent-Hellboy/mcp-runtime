"""Standalone article publication for articles.mcpruntime.org."""

from dataclasses import dataclass
from functools import lru_cache
import os
from pathlib import Path

import markdown
from flask import Flask, Response, abort, render_template, request, url_for

app = Flask(__name__)
app.config["SEND_FILE_MAX_AGE_DEFAULT"] = 0

CONTENT_DIR = Path(__file__).resolve().parent / "content"
STYLE_PATH = Path(__file__).resolve().parent / "static" / "style.css"
BASE_URL = (os.environ.get("MCP_ARTICLES_BASE_URL") or "https://articles.mcpruntime.org").rstrip("/")
WEBSITE_URL = (os.environ.get("MCP_WEBSITE_URL") or "https://mcpruntime.org").rstrip("/") + "/"
DOCS_URL = (os.environ.get("MCP_DOCS_URL") or "https://docs.mcpruntime.org").rstrip("/") + "/"
GITHUB_URL = "https://github.com/Agent-Hellboy/mcp-runtime"
STATIC_VERSION = int(STYLE_PATH.stat().st_mtime) if STYLE_PATH.exists() else 0


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


def _canonical_url() -> str:
    path = request.path or "/"
    if not path.startswith("/"):
        path = "/" + path
    return BASE_URL + path


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
        "docs_url": DOCS_URL,
        "github_url": GITHUB_URL,
        "static_version": STATIC_VERSION,
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
    )


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


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
