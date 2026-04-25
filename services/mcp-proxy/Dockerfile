FROM golang:1.25.9 AS build
WORKDIR /src

# Copy root module first to satisfy local replace directives used by the proxy module.
COPY go.mod go.sum ./
COPY pkg/ ./pkg/

# Copy service module and resolve dependencies from the service directory.
COPY services/mcp-proxy/go.mod services/mcp-proxy/go.sum ./services/mcp-proxy/
RUN cd services/mcp-proxy && go mod download

# Copy service source and build from within the service module.
COPY services/mcp-proxy/*.go ./services/mcp-proxy/
RUN cd services/mcp-proxy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mcp-proxy .

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/mcp-proxy /app/mcp-proxy
EXPOSE 8091
USER 65532:65532
ENTRYPOINT ["/app/mcp-proxy"]
