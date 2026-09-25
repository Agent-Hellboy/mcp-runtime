FROM python:3.12-slim AS build

WORKDIR /workspace/docs

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
RUN mkdocs build -f mkdocs.yml

FROM nginx:1.27-alpine

LABEL org.opencontainers.image.source="https://github.com/mcp-runtime/mcp-runtime" \
      org.opencontainers.image.title="mcp-runtime-docs" \
      org.opencontainers.image.description="MkDocs-built documentation site for mcp-runtime"

COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /workspace/site /usr/share/nginx/html

# Run as the image's unprivileged nginx user (uid 101). nginx.conf listens on
# 8080 and writes its pid to /tmp; temp dirs live under /var/cache/nginx.
RUN chown -R 101:101 /var/cache/nginx
USER 101

EXPOSE 8080
