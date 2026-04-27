FROM python:3.12-slim AS build

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
RUN mkdocs build -f mkdocs.yml

FROM nginx:1.27-alpine

LABEL org.opencontainers.image.source="https://github.com/Agent-Hellboy/mcp-runtime" \
      org.opencontainers.image.title="mcp-runtime-docs" \
      org.opencontainers.image.description="MkDocs-built documentation site for mcp-runtime"

COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /app/site /usr/share/nginx/html

EXPOSE 80
