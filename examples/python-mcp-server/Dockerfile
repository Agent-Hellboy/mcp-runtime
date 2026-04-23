FROM python:3.12-alpine

WORKDIR /app
COPY app.py /app/app.py
RUN pip install --no-cache-dir "mcp[cli]"

EXPOSE 8088

ENTRYPOINT ["python", "/app/app.py"]
