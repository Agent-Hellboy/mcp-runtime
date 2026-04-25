# Website

Marketing site and tracked documentation for MCP Runtime. Flask app, served behind gunicorn in production.

## Prerequisites

- Python 3.10+
- (optional) Docker for the container build

## Structure

- `templates/index.html` - main page template
- `templates/base.html` - shared layout (security headers, OG/Twitter, favicon)
- `static/style.css` - landing page styles
- `static/docs.css` - documentation styles
- `static/favicon.svg` - brand mark
- `docs/` - tracked documentation pages
  - `index.html` - Docs home
  - `runtime.html` - Runtime architecture
  - `cli.html` - CLI reference
  - `sentinel.html` - Services stack (services/)
  - `api.html` - API reference
- `app.py` - Flask server entry point (also serves `/robots.txt` and `/sitemap.xml`)
- `Dockerfile` - container setup

## Run locally

```sh
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
python3 app.py
```

## Docker

```sh
docker build -t website .
docker run --rm -p 8080:8080 website
```

## Repository Structure

The main repository follows a flat structure:
- `services/` - Service implementations (api, ui, ingest, processor, etc.)
- `k8s/` - Kubernetes manifests
- `pkg/` - Shared libraries (access, sentinel, clickhouse, k8sclient)
- `internal/` - CLI implementation
- `api/` - CRD types
- `website/` - This documentation website
