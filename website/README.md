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

## Production deploy (GitHub Actions)

The `deploy-website` job in [`.github/workflows/ci.yaml`](../.github/workflows/ci.yaml)
syncs `website/` to your remote host and, by default, builds/runs a Docker
container there:

```sh
docker build -t mcp-runtime-website:latest .
docker rm -f mcp-runtime-website || true
docker run -d --name mcp-runtime-website \
  --restart unless-stopped \
  -p 8080:8080 \
  -e MCP_DOCS_URL=https://docs.mcpruntime.org/ \
  -e MCP_WEBSITE_BASE_URL=https://mcpruntime.org \
  mcp-runtime-website:latest
```

Required GitHub secrets:

- `WEBSITE_DEPLOY_HOST`
- `WEBSITE_DEPLOY_USER`
- `WEBSITE_DEPLOY_PATH`
- `WEBSITE_DEPLOY_SSH_KEY`
- `WEBSITE_DEPLOY_HOST_KEY` — pinned SSH host key line, for example `host ssh-ed25519 AAAA...`
- `WEBSITE_BASE_URL=https://mcpruntime.org`

Optional GitHub secrets:

- `WEBSITE_DOCS_URL` (default `https://docs.mcpruntime.org/`)
- `WEBSITE_HOST_PORT=8080`
- `WEBSITE_CONTAINER_PORT=8080`
- `WEBSITE_CONTAINER_NAME=mcp-runtime-website`
- `WEBSITE_IMAGE_NAME=mcp-runtime-website:latest`
- `WEBSITE_DEPLOY_COMMAND` (if set, CI runs this instead of the default
  Docker build/run sequence)

## Files

- `app.py` — Flask app (home, robots, sitemap, `/docs*` redirect).
- `templates/base.html` — shared shell (header, footer, security headers).
- `templates/index.html` — single landing page.
- `static/style.css` — page styles.
- `static/favicon.svg` — brand mark.
- `Dockerfile` — container build.
