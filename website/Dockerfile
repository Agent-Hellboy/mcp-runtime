FROM python:3.12-slim

LABEL org.opencontainers.image.source="https://github.com/Agent-Hellboy/mcp-runtime" \
      org.opencontainers.image.title="mcp-runtime-website" \
      org.opencontainers.image.description="Marketing site for mcp-runtime (Flask + Gunicorn)"

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8080

CMD ["gunicorn", "-b", "0.0.0.0:8080", "app:app"]
