# Website

Documentation website for MCP Runtime.

## Structure

- `templates/index.html` - main page template
- `templates/base.html` - shared layout
- `static/style.css` - landing page styles
- `static/docs.css` - documentation styles
- `docs/` - Documentation pages
  - `index.html` - Docs home
  - `runtime.html` - Runtime architecture
  - `cli.html` - CLI reference
  - `sentinel.html` - Services stack (services/)
  - `api.html` - API reference
- `app.py` - Flask server entry point
- `Dockerfile` - container setup

## Run locally

```sh
python app.py
```

## Python (virtualenv)

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
