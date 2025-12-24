# Website

Basic website project.

## Files

- `index.html` - main page
- `style.css` - styles
- `app.py` - simple server entry point
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
docker run --rm -p 8000:8000 website
```
