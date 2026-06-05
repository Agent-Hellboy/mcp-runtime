# Articles

Standalone editorial site for `articles.mcpruntime.org`.

This is separate from:

- `website/` — product landing site for `mcpruntime.org`
- `docs/` — product documentation for `docs.mcpruntime.org`

## Content

Articles live under `content/<category>/`.

Current category roots:

- `content/mcp/`
- `content/kubernetes/`
- `content/networking/`
- `content/infrastructure/`
- `content/identity-policy/`

Images live under `static/articles/<category>/<article>/`.

## Run Locally

```sh
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
export MCP_ARTICLES_SECRET_KEY="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"
export MCP_ARTICLES_GOOGLE_CLIENT_ID="<google-client-id>"
export MCP_ARTICLES_GOOGLE_CLIENT_SECRET="<google-client-secret>"
export MCP_ARTICLES_BASE_URL="http://localhost:8080"
python3 app.py
```

Open <http://localhost:8080>.

Register `http://localhost:8080/auth/google/callback` as an authorized
redirect URI in the Google OAuth client for local development. Production uses
`https://articles.mcpruntime.org/auth/google/callback` unless
`MCP_ARTICLES_BASE_URL` is changed.

Comments are stored in SQLite. Set `MCP_ARTICLES_DB_PATH` to choose the
database location; the default local path is `articles.db`.

## Docker

```sh
docker build -t mcp-runtime-articles .
docker run --rm -p 8082:8080 \
  -e MCP_ARTICLES_BASE_URL=https://articles.mcpruntime.org \
  -e MCP_ARTICLES_SECRET_KEY="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')" \
  -e MCP_ARTICLES_GOOGLE_CLIENT_ID="<google-client-id>" \
  -e MCP_ARTICLES_GOOGLE_CLIENT_SECRET="<google-client-secret>" \
  -e MCP_ARTICLES_DB_PATH=/data/articles.db \
  -v mcp-runtime-articles-data:/data \
  mcp-runtime-articles
```

## Production

The CI `deploy-articles` job syncs `articles/`, builds
`mcp-runtime-articles:latest`, and runs it on host port `8082` by default.
The host nginx vhost for `articles.mcpruntime.org` should proxy to
`127.0.0.1:8082`.

Required secret:

- `ARTICLES_DEPLOY_PATH`

Optional secrets:

- `ARTICLES_DEPLOY_HOST` (falls back to `WEBSITE_DEPLOY_HOST`)
- `ARTICLES_DEPLOY_USER` (falls back to `WEBSITE_DEPLOY_USER`)
- `ARTICLES_DEPLOY_SSH_KEY` (falls back to `WEBSITE_DEPLOY_SSH_KEY`)
- `ARTICLES_DEPLOY_HOST_KEY` (falls back to `WEBSITE_DEPLOY_HOST_KEY`)
- `ARTICLES_HOST_PORT` (default: `8082`)
- `ARTICLES_CONTAINER_PORT` (default: `8080`)
- `ARTICLES_CONTAINER_NAME` (default: `mcp-runtime-articles`)
- `ARTICLES_IMAGE_NAME` (default: `mcp-runtime-articles:latest`)
- `ARTICLES_BASE_URL` (default: `https://articles.mcpruntime.org`)
- `ARTICLES_SECRET_KEY` (`MCP_ARTICLES_SECRET_KEY` in the container)
- `MCP_ARTICLES_GOOGLE_CLIENT_ID`
- `MCP_ARTICLES_GOOGLE_CLIENT_SECRET`
- `ARTICLES_DB_PATH` (default: `/data/articles.db`)
- `ARTICLES_DATA_VOLUME` (default: `mcp-runtime-articles-data`)
- `ARTICLES_DOCS_URL` (default: `https://docs.mcpruntime.org/`)
- `ARTICLES_WEBSITE_URL` (default: `https://mcpruntime.org/`)
- `ARTICLES_DEPLOY_COMMAND` (if set, CI runs it instead of the default Docker deploy)
