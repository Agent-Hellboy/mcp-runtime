# Website

Basic website project.

## Files

- `templates/index.html` - main page template
- `templates/base.html` - shared layout
- `static/style.css` - landing page styles
- `static/docs.css` - documentation styles
- `app.py` - Flask server entry point
- `Dockerfile` - container setup

## Run

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
