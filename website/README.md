# Website

Minimal Flask landing page for MCP Runtime. Serves a single home page with
links to GitHub and the documentation site.

Documentation lives at `docs.mcpruntime.org` (deployed separately) and is
authored as Markdown under [`../docs/`](../docs/) at the repo root. Any
`/docs*` request to this site 302-redirects to `MCP_DOCS_URL`.

## Run locally

```sh
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
python3 app.py
```

Then open <http://localhost:8080>.

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `MCP_DOCS_URL` | `https://docs.mcpruntime.org/` | Target for the Docs link and `/docs*` redirects. |
| `MCP_WEBSITE_BASE_URL` | derived from request | Trusted canonical origin for OG/sitemap URLs. |

## Docker

```sh
docker build -t mcp-runtime-website .
docker run --rm -p 8080:8080 mcp-runtime-website
```

## Files

- `app.py` — Flask app (home, robots, sitemap, `/docs*` redirect).
- `templates/base.html` — shared shell (header, footer, security headers).
- `templates/index.html` — single landing page.
- `static/style.css` — page styles.
- `static/favicon.svg` — brand mark.
- `Dockerfile` — container build.
